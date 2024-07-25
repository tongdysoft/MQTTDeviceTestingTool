package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
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
	// EventLog 为空时不写 CSV/文本日志；诊断事件不受影响。
	EventLog *EventLog
	// Context 只用于在启动阶段感知取消；服务运行期间由调用方通过 Close 停止。
	Context context.Context
	// Diagnostics 可选。未提供时 StartServer 按 Config 创建，
	// 并把事件终端输出写到 LogWriter（未提供时为 stdout）。
	// 传入的实例所有权交给 StartServer，随服务器一起关闭。
	Diagnostics *Diagnostics
}

// RunningServer 表示已绑定监听器并启动事件循环的服务器。
type RunningServer struct {
	config   *Config
	server   *mqtt.Server
	material *TLSMaterial
	addr     string
	diag     *Diagnostics
	listener *diagnosticListener

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
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	material, err := cfg.BuildTLSMaterial()
	if err != nil {
		return nil, err
	}
	tlsConfig := (*tls.Config)(nil)
	if material != nil {
		tlsConfig = material.Config
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
	diag := opts.Diagnostics
	if diag == nil {
		diag, err = NewDiagnostics(DiagnosticsOptions{
			Config: cfg,
			RunID:  cfg.RunID,
			Metadata: RunMetadata{
				DeviceModel:     cfg.DeviceModel,
				FirmwareVersion: cfg.FirmwareVersion,
				TestCase:        cfg.TestCase,
				Note:            cfg.Note,
			},
			ResultsDir: cfg.ResultsDir,
			Terminal:   writer,
		})
		if err != nil {
			return nil, err
		}
	}
	fail := func(startErr error) (*RunningServer, error) {
		if closeErr := diag.Close(); closeErr != nil {
			return nil, errors.Join(startErr, closeErr)
		}
		return nil, startErr
	}
	level := new(slog.LevelVar)
	level.Set(slog.LevelError)

	server := mqtt.New(nil)
	server.Log = slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: level,
	}))

	if err := server.AddHook(new(auth.Hook), &auth.Options{Data: userData}); err != nil {
		return fail(fmt.Errorf("cannot load MQTT auth configuration: %w", err))
	}

	tcp := newDiagnosticListener(listenerOptions{
		address:          cfg.Listen,
		tlsConfig:        tlsConfig,
		diagnostics:      diag,
		handshakeTimeout: cfg.HandshakeTimeout,
		connectTimeout:   cfg.ConnectTimeout,
		maxHandshakes:    cfg.MaxConcurrentHandshakes,
		shutdownTimeout:  cfg.ShutdownTimeout,
	})
	if err := server.AddListener(tcp); err != nil {
		return fail(fmt.Errorf("cannot listen on %s: %w", cfg.Listen, err))
	}
	// 诊断 hook 必须添加在 auth.Hook 之后：
	// 其 OnACLCheck 只记录“前序 hook 已拒绝”的事实并返回 false，不放宽或绕过认证。
	if err := server.AddHook(new(MQTTHook), &MQTTHookOptions{
		Server:      server,
		Diagnostics: diag,
		EventLog:    opts.EventLog,
		Filter:      cfg.NewRecordFilter(),
	}); err != nil {
		_ = server.Close()
		return fail(fmt.Errorf("cannot initialise MQTT event hook: %w", err))
	}

	// 在开始接受连接之前固定实际生效的监听地址与证书信息，
	// 避免第一个连接的事件落在信息尚未写入的窗口里。
	if material != nil {
		clientAuth := material.Config.ClientAuth
		diag.SetServerInfo(tcp.Address(), &material.Report, material.Server.Chain, &clientAuth)
	} else {
		diag.SetServerInfo(tcp.Address(), nil, nil, nil)
	}

	if err := server.Serve(); err != nil {
		_ = server.Close()
		return fail(fmt.Errorf("cannot start MQTT server: %w", err))
	}

	return &RunningServer{
		config:   cfg,
		server:   server,
		material: material,
		addr:     tcp.Address(),
		diag:     diag,
		listener: tcp,
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
	if r.material == nil {
		return nil
	}
	return r.material.Config
}

// CertificateReport 返回证书链、信任库与放行项的可审计报告。
// plain 模式返回 nil。
func (r *RunningServer) CertificateReport() *CertificateReport {
	if r.material == nil {
		return nil
	}
	report := r.material.Report
	return &report
}

// Diagnostics 返回本次运行的诊断记录器。关闭服务器后其汇总为最终结果。
func (r *RunningServer) Diagnostics() *Diagnostics {
	return r.diag
}

// Close 停止接收新连接、关闭现有连接、等待清理并完成结果导出，可重复调用。
//
// 关闭预算 -shutdown-timeout 只计算一次：监听器用它设置所有连接的通知写期限，
// 之后再用同一个期限等待连接处理与 mochi 的 ClientsWg；另加一个固定的本地
// 收尾余量（不随连接数量累积）。mochi 的 CloseAll 在监听器返回后还有一次
// 无期限的等待，这里在已完成底层连接关闭的前提下做有界等待，超时如实返回错误。
func (r *RunningServer) Close() error {
	r.closeOnce.Do(func() {
		budget := time.Duration(0)
		if r.config != nil {
			budget = r.config.ShutdownTimeout
		}
		var networkDeadline, cleanupDeadline time.Time
		if budget > 0 {
			networkDeadline = time.Now().Add(budget)
			// 本地收尾余量用于连接被强制关闭后的 goroutine 调度与
			// mochi CloseAll 自身的收尾；它是常数，不按连接累积。
			cleanupDeadline = networkDeadline.Add(shutdownCompletionGrace)
		}
		r.listener.SetShutdownDeadline(networkDeadline)
		// 即使 CloseAll 本身迟迟不进入监听器 Close，也要让已被本地关闭
		// 打断的断连事件使用 shutdown 语义。
		r.diag.MarkShuttingDown()

		serverDone := make(chan error, 1)
		go func() { serverDone <- r.server.Close() }()
		var serverErr error
		if err, done := awaitError(serverDone, cleanupDeadline); done {
			serverErr = err
		} else {
			message := fmt.Sprintf("MQTT server cleanup did not finish within %s plus %s local cleanup grace", budget, shutdownCompletionGrace)
			serverErr = errors.New(message)
			r.diag.ShutdownCleanupFailed("server", message, r.listener.ActiveCount())
		}
		listenerErr := r.listener.CloseError()
		diagErr := r.diag.CloseWithin(cleanupDeadline)
		r.closeErr = errors.Join(serverErr, listenerErr, diagErr)
	})
	return r.closeErr
}

// awaitError 等待 done 返回结果；deadline 为零时无限等待。
// 返回 done=false 表示预算耗尽，调用方必须自行报告，而不是假装清理完成。
func awaitError(done <-chan error, deadline time.Time) (error, bool) {
	if deadline.IsZero() {
		return <-done, true
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case err := <-done:
		return err, true
	case <-timer.C:
		return nil, false
	}
}
