package main

// 本文件实现带诊断能力的 TCP/TLS 监听器。
// 它复用项目真实的监听与 MQTT 服务路径，只在外围增加可观察节点：
//   - TCP 接入
//   - TLS 握手成功/失败（显式握手，便于取得 ConnectionState 与精确错误）
//   - 传输层关闭
// 不自定义 VerifyPeerCertificate，不改变客户端证书认证策略。

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mochi-mqtt/server/v2/listeners"
)

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

	log      *slog.Logger
	end      uint32
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
	closed   bool
	closeErr error

	listen net.Listener
}

func newDiagnosticListener(opts listenerOptions) *diagnosticListener {
	maxHandshakes := opts.maxHandshakes
	if maxHandshakes < 1 {
		maxHandshakes = 1
	}
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
		conns:            map[net.Conn]struct{}{},
	}
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
	l.wg.Add(1)
	return true
}

func (l *diagnosticListener) untrack(conn net.Conn) {
	l.mu.Lock()
	delete(l.conns, conn)
	l.mu.Unlock()
	l.wg.Done()
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
	id := l.diag.ConnectionAccepted(remote, l.id)

	var conn *observedConn
	if l.tlsConfig != nil {
		if l.handshakeTimeout > 0 {
			_ = raw.SetReadDeadline(time.Now().Add(l.handshakeTimeout))
		}
		tlsConn := tls.Server(raw, l.tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			certainty := l.diag.TLSHandshakeFailed(id, err)
			l.diag.ConnectionClosedWithReason(id, "tls_handshake_failed", certainty)
			_ = tlsConn.Close()
			_ = raw.Close()
			return
		}
		l.diag.TLSHandshakeSucceeded(id, tlsConn.ConnectionState())
		conn = &observedConn{Conn: tlsConn, id: id}
	} else {
		conn = &observedConn{Conn: raw, id: id}
	}
	release()
	if l.connectTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(l.connectTimeout))
	}
	l.serveConn(conn, establish)
}

func (l *diagnosticListener) serveConn(conn *observedConn, establish listeners.EstablishFn) {
	err := establish(l.id, conn)
	// 在关闭连接之前记录原因：先关闭会先触发通用的 transport close，
	// 使这里更具体的超时/协议错误被去重逻辑隐藏。
	if err != nil {
		kind, reason, certainty := classifyTransportError(err)
		l.diag.ConnectionClosedWithReason(conn.id, kind+": "+reason, certainty)
		if l.log != nil {
			l.log.Warn("connection ended", "error", l.diag.SanitizeError(err), "remote", conn.RemoteAddr().String())
		}
	} else {
		l.diag.ConnectionClosedWithReason(conn.id, "connection_closed", CertaintyObserved)
	}
	_ = conn.Close()
}

func (l *diagnosticListener) Close(closeClients listeners.CloseFn) {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		conns := make([]net.Conn, 0, len(l.conns))
		for conn := range l.conns {
			conns = append(conns, conn)
		}
		l.mu.Unlock()

		atomic.StoreUint32(&l.end, 1)
		close(l.done)
		// 先关闭监听套接字，确保关闭期间不再接受新连接。
		if l.listen != nil {
			_ = l.listen.Close()
		}
		if closeClients != nil {
			closeClients(l.id)
		}
		for _, conn := range conns {
			_ = conn.Close()
		}
		if !waitGroupWithin(&l.wg, l.shutdownTimeout) {
			l.mu.Lock()
			l.closeErr = fmt.Errorf("%d connection(s) did not finish within %s", len(conns), l.shutdownTimeout)
			l.mu.Unlock()
		}
	})
}

// CloseError 返回关闭阶段超出时限的错误；正常关闭时为空。
func (l *diagnosticListener) CloseError() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closeErr
}

// waitGroupWithin 等待 wg 完成；超时返回 false，不阻塞调用方。
func waitGroupWithin(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	if timeout <= 0 {
		<-done
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// observedConn 让 MQTT hook 能从 cl.Net.Conn 找回连接标识，
// 并在读到 CONNECT 后解除启动阶段的读超时。
type observedConn struct {
	net.Conn
	id string

	closeOnce sync.Once
}

func (c *observedConn) DiagnosticsConnID() string { return c.id }

func (c *observedConn) ClearInitialDeadline() {
	_ = c.Conn.SetReadDeadline(time.Time{})
}

func (c *observedConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		err = c.Conn.Close()
	})
	return err
}
