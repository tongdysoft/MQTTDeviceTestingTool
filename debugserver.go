package main

// pprof 调试监听默认关闭。只有显式传入 -pprof-listen 时才启动，
// 并绑定用户指定的地址；非回环地址会输出警告。

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
	"time"
)

type debugServer struct {
	server   *http.Server
	listener net.Listener
	addr     string

	closeOnce sync.Once
	closeErr  error
}

func startDebugServer(address string, eventLog *EventLog) (*debugServer, error) {
	if address == "" {
		return nil, nil
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("cannot listen for pprof on %s: %w", address, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	d := &debugServer{server: server, listener: listener, addr: listener.Addr().String()}
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			eventLog.Print("X", fmt.Sprintf("%s: %v", lang("DEBUGHTTPFAIL"), serveErr))
		}
	}()
	if !isLoopbackAddr(listener.Addr()) {
		eventLog.Print("X", fmt.Sprintf("%s: %s", lang("DEBUGHTTPPUBLIC"), d.addr))
	}
	eventLog.Print("C", fmt.Sprintf("%s: %s", lang("DEBUGHTTP"), d.addr))
	return d, nil
}

// Close 在主服务退出前停止调试监听；nil 接收者表示未启用。
func (d *debugServer) Close(timeout time.Duration) error {
	if d == nil {
		return nil
	}
	d.closeOnce.Do(func() {
		if timeout <= 0 {
			timeout = defaultShutdownTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := d.server.Shutdown(ctx); err != nil {
			// Shutdown 超时后强制关闭底层监听，避免调试端口残留。
			d.closeErr = errors.Join(err, d.listener.Close())
			return
		}
		d.closeErr = nil
	})
	return d.closeErr
}

func isLoopbackAddr(addr net.Addr) bool {
	if addr == nil {
		return false
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
