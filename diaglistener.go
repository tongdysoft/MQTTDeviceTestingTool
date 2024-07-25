package main

// 本文件实现带诊断能力的 TCP/TLS 监听器。
// 它复用项目真实的监听与 MQTT 服务路径，只在外围增加可观察节点：
//   - TCP 接入
//   - TLS 握手成功/失败（显式握手，便于取得 ConnectionState 与精确错误）
//   - 传输层关闭
// 不自定义 VerifyPeerCertificate，不改变客户端证书认证策略。

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

// errServerShutdown 标记由本工具主动关闭而中断的握手或连接。
// 它用于把“本地关闭”与设备证书/协议失败区分开。
var errServerShutdown = errors.New("server is shutting down")

// shutdownCompletionGrace 是关闭预算耗尽后，为本地收尾预留的固定时间：
// 通知写与握手受 -shutdown-timeout 约束，连接被强制关闭后处理 goroutine
// 仍需要短暂调度时间。这个余量是常数，不随连接数量累积。
const shutdownCompletionGrace = 250 * time.Millisecond

// deadlineSetupError 表示本地无法设置或清除 socket 期限。
// 它不代表对端设备或证书失败；无法限时的连接会被直接关闭。
type deadlineSetupError struct {
	operation string
	err       error
}

func (e *deadlineSetupError) Error() string {
	return fmt.Sprintf("cannot %s: %v", e.operation, e.err)
}

func (e *deadlineSetupError) Unwrap() error { return e.err }

// listenerOptions 是诊断监听器的生命周期参数。
type listenerOptions struct {
	address          string
	tlsConfig        *tls.Config
	diagnostics      *Diagnostics
	handshakeTimeout time.Duration
	connectTimeout   time.Duration
	maxHandshakes    int
	shutdownTimeout  time.Duration
}

// diagnosticListener 实现 listeners.Listener，保留与 listeners.TCP 相同的
// 监听、关闭和客户端清理语义，并限制并发握手数量。
type diagnosticListener struct {
	id        string
	address   string
	tlsConfig *tls.Config
	diag      *Diagnostics

	handshakeTimeout time.Duration
	connectTimeout   time.Duration
	shutdownTimeout  time.Duration
	slots            chan struct{}
	done             chan struct{}
	closeOnce        sync.Once
	ctx              context.Context
	cancel           context.CancelFunc

	log   *slog.Logger
	end   uint32
	mu    sync.Mutex
	conns map[net.Conn]struct{}
	// guards 为每个连接保存期限门控，防止 MQTT 保活刷新把关闭期限覆盖掉。
	guards map[net.Conn]*deadlineGuard
	active activeTasks
	closed bool
	// shutdownDeadline 由 RunningServer.Close 提前设置，使监听器与
	// 连接通知、诊断收尾共享同一个绝对期限。
	shutdownDeadline time.Time
	closeErr         error

	listen net.Listener
}

func newDiagnosticListener(opts listenerOptions) *diagnosticListener {
	maxHandshakes := opts.maxHandshakes
	if maxHandshakes < 1 {
		maxHandshakes = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &diagnosticListener{
		id:               "",
		address:          opts.address,
		tlsConfig:        opts.tlsConfig,
		diag:             opts.diagnostics,
		handshakeTimeout: opts.handshakeTimeout,
		connectTimeout:   opts.connectTimeout,
		shutdownTimeout:  opts.shutdownTimeout,
		slots:            make(chan struct{}, maxHandshakes),
		done:             make(chan struct{}),
		ctx:              ctx,
		cancel:           cancel,
		conns:            map[net.Conn]struct{}{},
		guards:           map[net.Conn]*deadlineGuard{},
	}
}

// deadlineGuard 让关闭期限对后续所有 Set*Deadline 调用保持生效。
// mochi-mqtt 在读取循环中会按保活时间刷新期限；如果没有这层门控，
// 关闭时的 DISCONNECT 写会把期限延长到保活上限。
type deadlineGuard struct {
	mu       sync.Mutex
	shutdown time.Time
}

func (g *deadlineGuard) setShutdown(deadline time.Time) {
	if deadline.IsZero() {
		return
	}
	g.mu.Lock()
	if g.shutdown.IsZero() || deadline.Before(g.shutdown) {
		g.shutdown = deadline
	}
	g.mu.Unlock()
}

// apply 在门控锁内直接设置底层连接期限，避免与并发的刷新交错。
func (g *deadlineGuard) apply(conn net.Conn, deadline time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.shutdown.IsZero() && (deadline.IsZero() || deadline.After(g.shutdown)) {
		deadline = g.shutdown
	}
	return conn.SetDeadline(deadline)
}

func (g *deadlineGuard) applyRead(conn net.Conn, deadline time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.shutdown.IsZero() && (deadline.IsZero() || deadline.After(g.shutdown)) {
		deadline = g.shutdown
	}
	return conn.SetReadDeadline(deadline)
}

func (g *deadlineGuard) applyWrite(conn net.Conn, deadline time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.shutdown.IsZero() && (deadline.IsZero() || deadline.After(g.shutdown)) {
		deadline = g.shutdown
	}
	return conn.SetWriteDeadline(deadline)
}

// activeTasks 跟踪本次监听器创建的处理 goroutine。
// 它提供可等待的“归零”信号，而不像 sync.WaitGroup 那样只能阻塞等待。
type activeTasks struct {
	mu   sync.Mutex
	n    int
	idle chan struct{}
}

func (a *activeTasks) add() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.n == 0 {
		a.idle = make(chan struct{})
	}
	a.n++
}

func (a *activeTasks) done() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n--
	if a.n == 0 && a.idle != nil {
		close(a.idle)
	}
}

// wait 在 deadline 前等待任务归零；返回是否完成及剩余数量。
// deadline 为零表示无限等待（仅在未配置关闭期限时使用）。
func (a *activeTasks) wait(deadline time.Time) (bool, int) {
	a.mu.Lock()
	if a.n == 0 {
		a.mu.Unlock()
		return true, 0
	}
	idle := a.idle
	a.mu.Unlock()
	if deadline.IsZero() {
		<-idle
		return true, 0
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-idle:
		return true, 0
	case <-timer.C:
		a.mu.Lock()
		pending := a.n
		a.mu.Unlock()
		return false, pending
	}
}

func (a *activeTasks) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}

func (l *diagnosticListener) ID() string { return l.id }

func (l *diagnosticListener) Protocol() string { return listeners.TypeTCP }

func (l *diagnosticListener) Address() string {
	if l.listen != nil {
		return l.listen.Addr().String()
	}
	return l.address
}

func (l *diagnosticListener) Init(log *slog.Logger) error {
	l.log = log
	listener, err := net.Listen("tcp", l.address)
	if err != nil {
		return err
	}
	l.listen = listener
	return nil
}

func (l *diagnosticListener) Serve(establish listeners.EstablishFn) {
	for {
		select {
		case <-l.done:
			return
		case l.slots <- struct{}{}:
		}

		raw, err := l.listen.Accept()
		if err != nil {
			<-l.slots
			if atomic.LoadUint32(&l.end) == 0 && l.log != nil {
				l.log.Warn("listener accept failed", "error", l.diag.SanitizeError(err))
			}
			return
		}
		if atomic.LoadUint32(&l.end) == 1 {
			<-l.slots
			_ = raw.Close()
			return
		}
		if !l.track(raw) {
			<-l.slots
			_ = raw.Close()
			return
		}
		go l.handle(raw, establish, func() { <-l.slots })
	}
}

func (l *diagnosticListener) track(conn net.Conn) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false
	}
	l.conns[conn] = struct{}{}
	l.guards[conn] = &deadlineGuard{}
	l.active.add()
	return true
}

func (l *diagnosticListener) untrack(conn net.Conn) {
	l.mu.Lock()
	delete(l.conns, conn)
	delete(l.guards, conn)
	l.mu.Unlock()
	l.active.done()
}

// guardFor 返回连接的期限门控；正常路径由 track 创建。
func (l *diagnosticListener) guardFor(conn net.Conn) *deadlineGuard {
	l.mu.Lock()
	defer l.mu.Unlock()
	if guard := l.guards[conn]; guard != nil {
		return guard
	}
	guard := &deadlineGuard{}
	l.guards[conn] = guard
	return guard
}

func (l *diagnosticListener) handle(raw net.Conn, establish listeners.EstablishFn, releaseSlot func()) {
	defer l.untrack(raw)
	// 握手槽只覆盖握手阶段；成功建立 MQTT 会话的连接不再占用。
	// defer 保证任何提前返回都会释放槽位。
	released := false
	release := func() {
		if !released {
			released = true
			releaseSlot()
		}
	}
	defer release()

	remote := raw.RemoteAddr().String()
	guard := l.guardFor(raw)
	id := l.diag.ConnectionAccepted(remote, l.id)

	var conn *observedConn
	if l.tlsConfig != nil {
		tlsConn, err := l.handshake(raw)
		if err != nil {
			if l.isShuttingDown() && isShutdownInterruption(err) {
				l.diag.TLSHandshakeAborted(id, "server_shutdown", err)
				l.diag.ConnectionClosedWithReason(id, "server_shutdown", CertaintyObserved)
			} else {
				certainty := l.diag.TLSHandshakeFailed(id, err)
				l.diag.ConnectionClosedWithReason(id, "tls_handshake_failed", certainty)
			}
			_ = tlsConn.Close()
			_ = raw.Close()
			return
		}
		l.diag.TLSHandshakeSucceeded(id, tlsConn.ConnectionState())
		conn = &observedConn{Conn: tlsConn, id: id, guard: guard}
	} else {
		conn = &observedConn{Conn: raw, id: id, guard: guard}
	}
	release()
	if l.connectTimeout > 0 {
		if err := conn.SetReadDeadline(time.Now().Add(l.connectTimeout)); err != nil {
			// 无法限制 CONNECT 阶段时会破坏“异常客户端不能长期占坑”的约束，
			// 因此直接关闭，并把原因记录为本地期限设置失败。
			l.diag.ConnectionClosedWithReason(id,
				(&deadlineSetupError{operation: "set MQTT CONNECT read deadline", err: err}).Error(),
				CertaintyObserved)
			_ = conn.Close()
			return
		}
	}
	l.serveConn(conn, establish)
}

// handshake 在读写两个方向上都设置绝对期限，并在握手成功后清除。
// 握手期间监听器关闭会取消上下文，避免只能在期限到点后才退出。
func (l *diagnosticListener) handshake(raw net.Conn) (*tls.Conn, error) {
	tlsConn := tls.Server(raw, l.tlsConfig)
	deadlineSet := false
	if l.handshakeTimeout > 0 {
		if err := raw.SetDeadline(time.Now().Add(l.handshakeTimeout)); err != nil {
			return tlsConn, &deadlineSetupError{operation: "set TLS handshake deadline", err: err}
		}
		deadlineSet = true
	}
	if err := tlsConn.HandshakeContext(l.ctx); err != nil {
		return tlsConn, err
	}
	if !deadlineSet {
		return tlsConn, nil
	}
	// 成功后必须同时清除读、写期限，再进入 MQTT CONNECT 与保活阶段。
	// 一旦开始关闭就不能再清除，否则关闭通知的写可能永久阻塞。
	if err := l.clearHandshakeDeadline(tlsConn); err != nil {
		return tlsConn, err
	}
	return tlsConn, nil
}

func (l *diagnosticListener) clearHandshakeDeadline(conn net.Conn) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errServerShutdown
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return &deadlineSetupError{operation: "clear TLS handshake deadline", err: err}
	}
	return nil
}

// isShutdownInterruption 判断错误是否只是本地关闭或关闭期限造成的中断。
// 证书验证、协议错误等不在其中，仍按原有分类记录。
func isShutdownInterruption(err error) bool {
	if err == nil {
		return false
	}
	var code packets.Code
	if errors.As(err, &code) && code.Code == packets.ErrServerShuttingDown.Code {
		return true
	}
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, errServerShutdown) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.ErrClosedPipe) ||
		isPeerClosedError(err) ||
		isTimeoutError(err)
}

func (l *diagnosticListener) isShuttingDown() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

func (l *diagnosticListener) serveConn(conn *observedConn, establish listeners.EstablishFn) {
	err := establish(l.id, conn)
	// 在关闭连接之前记录原因：先关闭会先触发通用的 transport close，
	// 使这里更具体的超时/协议错误被去重逻辑隐藏。
	if err != nil {
		if l.isShuttingDown() && isShutdownInterruption(err) {
			// 主动关闭不是设备侧失败，不能记录成证书或协议结论。
			l.diag.ConnectionClosedWithReason(conn.id, "server_shutdown", CertaintyObserved)
		} else {
			kind, reason, certainty := classifyTransportError(err)
			l.diag.ConnectionClosedWithReason(conn.id, kind+": "+reason, certainty)
		}
		if l.log != nil && !l.isShuttingDown() {
			l.log.Warn("connection ended", "error", l.diag.SanitizeError(err), "remote", conn.RemoteAddr().String())
		}
	} else {
		reason := "connection_closed"
		if l.isShuttingDown() {
			reason = "server_shutdown"
		}
		l.diag.ConnectionClosedWithReason(conn.id, reason, CertaintyObserved)
	}
	_ = conn.Close()
}

func (l *diagnosticListener) Close(closeClients listeners.CloseFn) {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		deadline := l.shutdownDeadline
		if deadline.IsZero() && l.shutdownTimeout > 0 {
			deadline = time.Now().Add(l.shutdownTimeout)
		}
		conns := make([]net.Conn, 0, len(l.conns))
		guards := make([]*deadlineGuard, 0, len(l.conns))
		for conn := range l.conns {
			conns = append(conns, conn)
			if guard := l.guards[conn]; guard != nil {
				guards = append(guards, guard)
			}
		}
		// 在同一个绝对期限下打断握手、客户端通知与残余读写。
		// 期限只计算一次，不会为每个连接重新获得完整预算。
		if !deadline.IsZero() {
			// 先让门控生效，后续 mochi 的保活刷新只能收紧，不能延长。
			for _, guard := range guards {
				guard.setShutdown(deadline)
			}
			for _, conn := range conns {
				_ = conn.SetDeadline(deadline)
			}
		}
		l.mu.Unlock()

		l.diag.MarkShuttingDown()
		l.cancel()
		atomic.StoreUint32(&l.end, 1)
		close(l.done)
		// 先关闭监听套接字，确保关闭期间不再接受新连接。
		if l.listen != nil {
			_ = l.listen.Close()
		}
		// 通知客户端（mochi 的 DISCONNECT 写是同步的），写受上面的期限约束。
		if closeClients != nil {
			closeClients(l.id)
		}
		// 无论通知是否成功都关闭底层连接，打断残余的读写阻塞。
		for _, conn := range conns {
			_ = conn.Close()
		}

		waitUntil := deadline
		if !waitUntil.IsZero() {
			waitUntil = waitUntil.Add(shutdownCompletionGrace)
		}
		if done, pending := l.active.wait(waitUntil); !done {
			message := fmt.Sprintf("%d connection(s) did not finish within the shutdown budget plus %s", pending, shutdownCompletionGrace)
			if l.shutdownTimeout > 0 {
				message = fmt.Sprintf("%d connection(s) did not finish within %s plus %s local cleanup grace",
					pending, l.shutdownTimeout, shutdownCompletionGrace)
			}
			l.mu.Lock()
			l.closeErr = errors.New(message)
			l.mu.Unlock()
			l.diag.ShutdownCleanupFailed("connections", message, pending)
		}
	})
}

// SetShutdownDeadline 让监听器与调用方共享同一个绝对关闭期限。
// 只接受第一次设置，重复或并发调用不会延长期限。
func (l *diagnosticListener) SetShutdownDeadline(deadline time.Time) {
	if deadline.IsZero() {
		return
	}
	l.mu.Lock()
	if l.shutdownDeadline.IsZero() {
		l.shutdownDeadline = deadline
	}
	l.mu.Unlock()
}

// ActiveCount 返回仍在处理的连接数量，用于关闭阶段的错误报告与测试。
func (l *diagnosticListener) ActiveCount() int {
	return l.active.count()
}

// CloseError 返回关闭阶段超出时限的错误；正常关闭时为空。
func (l *diagnosticListener) CloseError() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closeErr
}

// observedConn 让 MQTT hook 能从 cl.Net.Conn 找回连接标识，
// 并在读到 CONNECT 后解除启动阶段的读超时。
type observedConn struct {
	net.Conn
	id    string
	guard *deadlineGuard

	closeOnce sync.Once
}

func (c *observedConn) DiagnosticsConnID() string { return c.id }

func (c *observedConn) ClearInitialDeadline() {
	_ = c.SetReadDeadline(time.Time{})
}

// 以下三个期限方法都经过门控：关闭开始后，任何“清除期限”或
// “延长到保活时间”的调用都不能绕过关闭预算。
func (c *observedConn) SetDeadline(deadline time.Time) error {
	if c.guard == nil {
		return c.Conn.SetDeadline(deadline)
	}
	return c.guard.apply(c.Conn, deadline)
}

func (c *observedConn) SetReadDeadline(deadline time.Time) error {
	if c.guard == nil {
		return c.Conn.SetReadDeadline(deadline)
	}
	return c.guard.applyRead(c.Conn, deadline)
}

func (c *observedConn) SetWriteDeadline(deadline time.Time) error {
	if c.guard == nil {
		return c.Conn.SetWriteDeadline(deadline)
	}
	return c.guard.applyWrite(c.Conn, deadline)
}

func (c *observedConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		err = c.Conn.Close()
	})
	return err
}
