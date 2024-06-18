package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

const defaultAuthData = `
auth:
    - allow: true
acl:
    - filters:
        '#': 3
`

// StartOptions 是供 main 和测试共同使用的服务器启动参数。
// 测试可以指定 UserData 与 LogWriter，无需执行 main 或接管进程信号。
type StartOptions struct {
	Config    *Config
	UserData  []byte
	LogWriter io.Writer
}

// RunningServer 表示已绑定监听器并启动事件循环的服务器。
type RunningServer struct {
	config    *Config
	server    *mqtt.Server
	tlsConfig *tls.Config
	addr      string

	closeOnce sync.Once
	closeErr  error
}

// StartServer 校验配置、加载 TLS 材料并启动 MQTT 服务。
// 所有失败都通过 error 返回，不会调用 os.Exit，也不会处理信号。
func StartServer(opts StartOptions) (*RunningServer, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("server config is required")
	}
	cfg := opts.Config
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	tlsConfig, err := cfg.BuildTLSConfig()
	if err != nil {
		return nil, err
	}

	userData := opts.UserData
	if userData == nil {
		if cfg.UserFile != "" {
			userData, err = os.ReadFile(cfg.UserFile)
			if err != nil {
				return nil, fmt.Errorf("cannot read users file %s: %w", cfg.UserFile, err)
			}
		} else {
			userData = []byte(defaultAuthData)
		}
	}

	writer := opts.LogWriter
	if writer == nil {
		writer = os.Stdout
	}
	level := new(slog.LevelVar)
	level.Set(slog.LevelError)

	server := mqtt.New(nil)
	server.Log = slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: level,
	}))

	if err := server.AddHook(new(auth.Hook), &auth.Options{Data: userData}); err != nil {
		return nil, fmt.Errorf("cannot load MQTT auth configuration: %w", err)
	}

	tcp := listeners.NewTCP(listeners.Config{
		Address:   cfg.Listen,
		TLSConfig: tlsConfig,
	})
	if err := server.AddListener(tcp); err != nil {
		return nil, fmt.Errorf("cannot listen on %s: %w", cfg.Listen, err)
	}
	if err := server.AddHook(new(MQTTHook), &MQTTHookOptions{Server: server}); err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("cannot initialise MQTT event hook: %w", err)
	}

	if err := server.Serve(); err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("cannot start MQTT server: %w", err)
	}

	return &RunningServer{
		config:    cfg,
		server:    server,
		tlsConfig: tlsConfig,
		addr:      tcp.Address(),
	}, nil
}

// Addr 返回监听器实际绑定的地址（端口为 0 时可用于测试取得动态端口）。
func (r *RunningServer) Addr() string {
	return r.addr
}

// Config 返回本次启动实际使用的配置。
func (r *RunningServer) Config() *Config {
	return r.config
}

// TLSConfig 返回实际传给监听器的 TLS 配置；plain 模式下为 nil。
func (r *RunningServer) TLSConfig() *tls.Config {
	return r.tlsConfig
}

// Close 关闭监听器、客户端和服务器事件循环，可重复调用。
func (r *RunningServer) Close() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.server.Close()
	})
	return r.closeErr
}
