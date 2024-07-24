package main

// 本文件实现文本日志、状态 CSV 与数据 CSV 的唯一写入入口。
// 所有写入由 EventLog 的互斥锁串行化；调用方不再依赖包级可变状态。
// 连接事件来自多个 goroutine，因此写入失败会计数并报告，而不是静默丢弃。

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/logrusorgru/aurora"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

// lockedWriter 让多个输出组件共享同一把锁，避免同一终端上的长行互相穿插。
type lockedWriter struct {
	mu     *sync.Mutex
	writer io.Writer
}

func newLockedWriter(writer io.Writer) *lockedWriter {
	return &lockedWriter{mu: new(sync.Mutex), writer: writer}
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

// EventLogOptions 描述终端与文件输出目标。路径为空表示不写入该文件。
type EventLogOptions struct {
	Terminal      io.Writer
	ErrorWriter   io.Writer
	TextPath      string
	StatusPath    string
	DataPath      string
	FileTimestamp bool
	Monochrome    bool
	// Protected 列出不得被日志截断的输入文件（例如证书、私钥、认证配置）。
	// 日志路径与其中任一路径指向同一文件时，OpenEventLog 会在截断前失败。
	Protected []FileRole
}

// EventLog 是并发安全的日志写入器。
type EventLog struct {
	mu          sync.Mutex
	terminal    io.Writer
	errorWriter io.Writer

	text   *os.File
	status *os.File
	data   *os.File

	textBroken   bool
	statusBroken bool
	dataBroken   bool

	fileTimestamp bool
	monochrome    bool
	windows       bool

	closed    bool
	errors    []string
	errorSeen map[string]struct{}
}

// OpenEventLog 创建日志写入器。显式请求的日志文件无法打开时返回错误，
// 由启动流程决定退出，而不是静默改用其他输出。
// 冲突检查在截断任何文件之前完成；打开后还会再比较一次文件身份，
// 缩小静态检查与实际截断之间的竞态窗口。
func OpenEventLog(opts EventLogOptions) (*EventLog, error) {
	l := &EventLog{
		terminal:      opts.Terminal,
		errorWriter:   opts.ErrorWriter,
		fileTimestamp: opts.FileTimestamp,
		monochrome:    opts.Monochrome,
		windows:       runtime.GOOS == "windows",
		errorSeen:     map[string]struct{}{},
	}
	if l.terminal == nil {
		l.terminal = os.Stdout
	}
	if l.errorWriter == nil {
		l.errorWriter = os.Stderr
	}

	targets := []FileRole{
		{Flag: "-log-data", Role: "data CSV log", Path: opts.DataPath, Kind: PathRoleOutput},
		{Flag: "-log-status", Role: "status CSV log", Path: opts.StatusPath, Kind: PathRoleOutput},
		{Flag: "-log", Role: "text log", Path: opts.TextPath, Kind: PathRoleOutput},
	}
	roles := make([]FileRole, 0, len(targets)+len(opts.Protected))
	roles = append(roles, targets...)
	roles = append(roles, opts.Protected...)
	if err := CheckFileRoleConflicts(roles); err != nil {
		return nil, err
	}

	opened := make([]*os.File, len(targets))
	closeOpened := func() {
		for _, file := range opened {
			if file != nil {
				_ = file.Close()
			}
		}
	}
	for i, target := range targets {
		if target.Path == "" {
			continue
		}
		// 先以不截断的方式打开：即使随后发现冲突，已有内容也保持不变。
		file, err := os.OpenFile(target.Path, os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			closeOpened()
			return nil, fmt.Errorf("cannot open %s %s: %w", target.Role, target.Path, err)
		}
		opened[i] = file
	}
	if err := verifyLogFileIdentities(targets, opened, opts.Protected); err != nil {
		closeOpened()
		return nil, err
	}
	for i, file := range opened {
		if file == nil {
			continue
		}
		if err := file.Truncate(0); err != nil {
			closeOpened()
			return nil, fmt.Errorf("cannot truncate %s %s: %w", targets[i].Role, targets[i].Path, err)
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			closeOpened()
			return nil, fmt.Errorf("cannot seek %s %s: %w", targets[i].Role, targets[i].Path, err)
		}
	}
	l.data, l.status, l.text = opened[0], opened[1], opened[2]
	return l, nil
}

// verifyLogFileIdentities 在打开后、截断前复核文件身份，
// 覆盖静态检查之后路径被替换（例如临时创建硬链接或符号链接）的情况。
// 它不承诺防御所有并发文件系统攻击，只是把破坏性操作推迟到复核之后。
func verifyLogFileIdentities(targets []FileRole, opened []*os.File, protected []FileRole) error {
	infos := make([]os.FileInfo, len(opened))
	for i, file := range opened {
		if file == nil {
			continue
		}
		info, err := file.Stat()
		if err != nil {
			return fmt.Errorf("cannot inspect opened %s %s: %w", targets[i].Role, targets[i].Path, err)
		}
		infos[i] = info
	}
	for i, info := range infos {
		if info == nil {
			continue
		}
		for j := 0; j < i; j++ {
			if infos[j] != nil && os.SameFile(info, infos[j]) {
				return conflictError(targets[i], targets[j], targets[i].Path)
			}
		}
		for _, role := range protected {
			if role.Path == "" {
				continue
			}
			other, err := os.Stat(role.Path)
			if err != nil {
				// 输入文件不存在或不可访问由配置校验负责报告。
				continue
			}
			if os.SameFile(info, other) {
				return conflictError(targets[i], role, targets[i].Path)
			}
		}
	}
	return nil
}

// Print 输出一条带图标和时间戳的日志行。nil 接收者表示不记录。
func (l *EventLog) Print(iconChar string, text string, titleMore ...string) {
	if l == nil {
		return
	}
	timeStr := time.Now().Format(timeFormat)
	more := ""
	if len(titleMore) > 0 {
		more = fmt.Sprintf("[%s]", strings.Join(titleMore, "]["))
	}
	head := fmt.Sprintf("[%s][%s]%s", iconChar, timeStr, more)
	body := fmt.Sprintf(" %s\n", text)

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.writeTextLocked(head + body)
	if l.terminal == nil {
		return
	}
	line := l.colorizeLocked(iconChar, head, body)
	if _, err := io.WriteString(l.terminal, line); err != nil {
		l.recordErrorLocked("terminal output", err)
	}
}

func (l *EventLog) colorizeLocked(iconChar, head, body string) string {
	if l.monochrome {
		return head + body
	}
	if l.windows {
		switch iconChar {
		case "M":
			return color.New(color.BgWhite, color.FgBlack).Sprint(head) +
				color.New(color.BgBlack, color.FgWhite).Sprint(body)
		case "C":
			return color.New(color.BgMagenta, color.FgWhite).Sprint(head) +
				color.New(color.BgBlack, color.FgMagenta).Sprint(body)
		case "L":
			return color.New(color.BgGreen, color.FgBlack).Sprint(head) +
				color.New(color.BgBlack, color.FgGreen).Sprint(body)
		case "D", "X":
			return color.New(color.BgRed, color.FgWhite).Sprint(head) +
				color.New(color.BgBlack, color.FgRed).Sprint(body)
		case "S":
			return color.New(color.BgCyan, color.FgBlack).Sprint(head) +
				color.New(color.BgBlack, color.FgCyan).Sprint(body)
		case "U":
			return color.New(color.BgYellow, color.FgBlack).Sprint(head) +
				color.New(color.BgBlack, color.FgYellow).Sprint(body)
		default:
			return color.New(color.Reset).Sprint(head) + body
		}
	}
	switch iconChar {
	case "M":
		return aurora.BgWhite(aurora.Black(head)).String() + aurora.BgBlack(aurora.White(body)).String()
	case "C":
		return aurora.BgMagenta(aurora.White(head)).String() + aurora.BgBlack(aurora.Magenta(body)).String()
	case "L":
		return aurora.BgGreen(aurora.Black(head)).String() + aurora.BgBlack(aurora.Green(body)).String()
	case "D", "X":
		return aurora.BgRed(aurora.White(head)).String() + aurora.BgBlack(aurora.Red(body)).String()
	case "S":
		return aurora.BgCyan(aurora.Black(head)).String() + aurora.BgBlack(aurora.Cyan(body)).String()
	case "U":
		return aurora.BgYellow(aurora.Black(head)).String() + aurora.BgBlack(aurora.Yellow(body)).String()
	default:
		return head + body
	}
}

func (l *EventLog) writeTextLocked(line string) {
	if l.text == nil || l.textBroken {
		return
	}
	if _, err := io.WriteString(l.text, line); err != nil {
		l.textBroken = true
		l.recordErrorLocked("text log", err)
	}
}

// Status 写入状态 CSV（连接、断开、订阅、退订）。nil 接收者表示不记录。
func (l *EventLog) Status(infos ...string) {
	l.writeCSV(true, infos)
}

// Data 写入消息数据 CSV。nil 接收者表示不记录。
func (l *EventLog) Data(infos ...string) {
	l.writeCSV(false, infos)
}

func (l *EventLog) writeCSV(isStatus bool, infos []string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	var file *os.File
	var broken *bool
	if isStatus {
		file, broken = l.status, &l.statusBroken
	} else {
		file, broken = l.data, &l.dataBroken
	}
	if file == nil || *broken {
		return
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	record := make([]string, 0, len(infos)+1)
	if l.fileTimestamp {
		record = append(record, fmt.Sprintf("%d", time.Now().UnixNano()))
	} else {
		record = append(record, time.Now().Format(timeFormat))
	}
	record = append(record, infos...)
	if err := writer.Write(record); err != nil {
		*broken = true
		l.recordErrorLocked("log CSV", err)
		return
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		*broken = true
		l.recordErrorLocked("log CSV", err)
		return
	}
	if _, err := file.Write(buf.Bytes()); err != nil {
		*broken = true
		l.recordErrorLocked("log CSV", err)
	}
}

func (l *EventLog) recordErrorLocked(kind string, err error) {
	if err == nil {
		return
	}
	message := fmt.Sprintf("%s: %v", kind, err)
	if l.errorSeen == nil {
		l.errorSeen = map[string]struct{}{}
	}
	if _, seen := l.errorSeen[message]; seen {
		return
	}
	l.errorSeen[message] = struct{}{}
	l.errors = append(l.errors, message)
	if l.errorWriter != nil {
		fmt.Fprintf(l.errorWriter, "log: %s\n", message)
	}
}

// Errors 返回已记录的写入错误摘要。
func (l *EventLog) Errors() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.errors...)
}

// Close 刷盘并关闭显式请求的日志文件，可重复调用。
func (l *EventLog) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	var errs []error
	for _, file := range []*os.File{l.text, l.status, l.data} {
		if file == nil {
			continue
		}
		if err := file.Sync(); err != nil {
			l.recordErrorLocked("log file", err)
			errs = append(errs, err)
		}
		if err := file.Close(); err != nil {
			l.recordErrorLocked("log file", err)
			errs = append(errs, err)
		}
	}
	l.text, l.status, l.data = nil, nil, nil
	if len(l.errors) > 0 {
		errs = append(errs, errors.New(strings.Join(l.errors, "; ")))
	}
	return errors.Join(errs...)
}

func pkFilters(pk packets.Subscriptions) string {
	var filters []string
	for _, filter := range pk {
		filters = append(filters, string(filter.Filter))
	}
	return strings.Join(filters, ",")
}

func strCL(cl *mqtt.Client) string {
	var userName string = string(cl.Properties.Username)
	if len(userName) > 0 {
		userName += "@"
	}
	return fmt.Sprintf("%s%s(%s)", userName, cl.ID, cl.Net.Remote)
}
