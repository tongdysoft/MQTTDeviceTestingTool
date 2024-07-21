package main

// 本文件实现带诊断能力的 TCP/TLS 监听器。
// 它复用项目真实的监听与 MQTT 服务路径，只在外围增加可观察节点：
//   - TCP 接入
//   - TLS 握手成功/失败（显式握手，便于取得 ConnectionState 与精确错误）
//   - 传输层关闭
// 不自定义 VerifyPeerCertificate，不改变客户端证书认证策略。

import (
	"crypto/tls"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"github.com/mochi-mqtt/server/v2/listeners"
)

// diagnosticListener 实现 listeners.Listener，保留与 listeners.TCP 相同的
// 监听、关闭和客户端清理语义。
type diagnosticListener struct {
	id        string
	address   string
	tlsConfig *tls.Config
	diag      *Diagnostics

	log    *slog.Logger
	end    uint32
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	wg     sync.WaitGroup
	closed bool

	listen net.Listener
}

func newDiagnosticListener(id, address string, tlsConfig *tls.Config, diag *Diagnostics) *diagnosticListener {
	return &diagnosticListener{
		id:        id,
		address:   address,
		tlsConfig: tlsConfig,
		diag:      diag,
		conns:     map[net.Conn]struct{}{},
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
		if atomic.LoadUint32(&l.end) == 1 {
			return
		}
		raw, err := l.listen.Accept()
		if err != nil {
			if atomic.LoadUint32(&l.end) == 0 && l.log != nil {
				l.log.Warn("listener accept failed", "error", l.diag.SanitizeError(err))
			}
			return
		}
		if atomic.LoadUint32(&l.end) == 1 {
			_ = raw.Close()
			return
		}
		if !l.track(raw) {
			_ = raw.Close()
			return
		}
		go l.handle(raw, establish)
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

func (l *diagnosticListener) handle(raw net.Conn, establish listeners.EstablishFn) {
	defer l.untrack(raw)
	remote := raw.RemoteAddr().String()
	id := l.diag.ConnectionAccepted(remote, l.id)

	if l.tlsConfig != nil {
		tlsConn := tls.Server(raw, l.tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			certainty := l.diag.TLSHandshakeFailed(id, err)
			l.diag.ConnectionClosedWithReason(id, "tls_handshake_failed", certainty)
			_ = tlsConn.Close()
			_ = raw.Close()
			return
		}
		l.diag.TLSHandshakeSucceeded(id, tlsConn.ConnectionState())
		conn := &observedConn{Conn: tlsConn, id: id, diag: l.diag}
		l.serveConn(conn, establish)
		return
	}

	conn := &observedConn{Conn: raw, id: id, diag: l.diag}
	l.serveConn(conn, establish)
}

func (l *diagnosticListener) serveConn(conn *observedConn, establish listeners.EstablishFn) {
	err := establish(l.id, conn)
	if err != nil && l.log != nil {
		l.log.Warn("connection ended", "error", l.diag.SanitizeError(err), "remote", conn.RemoteAddr().String())
	}
	_ = conn.Close()
}

func (l *diagnosticListener) Close(closeClients listeners.CloseFn) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	conns := make([]net.Conn, 0, len(l.conns))
	for conn := range l.conns {
		conns = append(conns, conn)
	}
	l.mu.Unlock()

	atomic.StoreUint32(&l.end, 1)
	if closeClients != nil {
		closeClients(l.id)
	}
	if l.listen != nil {
		_ = l.listen.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
	l.wg.Wait()
}

// observedConn 让 MQTT hook 能从 cl.Net.Conn 找回连接标识。
type observedConn struct {
	net.Conn
	id   string
	diag *Diagnostics

	closeOnce sync.Once
}

func (c *observedConn) DiagnosticsConnID() string { return c.id }

func (c *observedConn) Close() error {
	err := c.Conn.Close()
	c.closeOnce.Do(func() {
		c.diag.ConnectionClosed(c.id, nil)
	})
	return err
}
