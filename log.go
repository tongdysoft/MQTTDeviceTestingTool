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

	open := func(path string) (*os.File, error) {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, fmt.Errorf("cannot open log file %s: %w", path, err)
		}
		return file, nil
	}
	var err error
	if opts.DataPath != "" {
		if l.data, err = open(opts.DataPath); err != nil {
			l.closeFilesLocked()
			return nil, err
		}
	}
	if opts.StatusPath != "" {
		if l.status, err = open(opts.StatusPath); err != nil {
			l.closeFilesLocked()
			return nil, err
		}
	}
	if opts.TextPath != "" {
		if l.text, err = open(opts.TextPath); err != nil {
			l.closeFilesLocked()
			return nil, err
		}
	}
	return l, nil
}

func (l *EventLog) closeFilesLocked() {
	for _, file := range []*os.File{l.text, l.status, l.data} {
		if file != nil {
			_ = file.Close()
		}
	}
	l.text, l.status, l.data = nil, nil, nil
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
