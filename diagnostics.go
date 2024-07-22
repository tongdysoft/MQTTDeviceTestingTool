package main

// 本文件实现连接诊断的事件模型、结构化导出与结果判定。
//
// 设计要点：
//   - 事件在真实监听与真实 MQTT 服务路径上产生，不另建“演示”路径。
//   - TCP 接入、TLS 握手、MQTT CONNECT、CONNACK、订阅授权、发布、断开分别记录。
//   - 收到 MQTT CONNECT 只表示“收到连接请求”，认证结果以 CONNACK 返回码为准。
//   - 不序列化 MQTT CONNECT 或 Client 对象，避免把密码写入日志。
//   - 自动判定只使用完整证据；TLS 未完成时不推断设备能力。

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// 事件阶段。阶段与结果使用稳定的小写标识，便于结构化解析。
const (
	StageRun           = "run"
	StageTransport     = "transport"
	StageTLS           = "tls"
	StageMQTTConnect   = "mqtt_connect"
	StageMQTTAuth      = "mqtt_auth"
	StageAuthorization = "authorization"
	StageSubscribe     = "subscribe"
	StageUnsubscribe   = "unsubscribe"
	StagePublish       = "publish"
	StageDisconnect    = "disconnect"
)

// 事件结果。
const (
	ResultInfo     = "info"
	ResultSuccess  = "success"
	ResultFailure  = "failure"
	ResultAccepted = "accepted"
	ResultRejected = "rejected"
	ResultGranted  = "granted"
	ResultDenied   = "denied"
	ResultObserved = "observed"
	ResultClosed   = "closed"
	ResultReceived = "received"
	ResultUnknown  = "unknown"
)

// 证据确定性。自动判定只接受 CertaintyObserved。
const (
	CertaintyObserved  = "observed"
	CertaintyHeuristic = "heuristic"
	CertaintyUncertain = "uncertain"
	CertaintyUnknown   = "unknown"
)

// 单次运行的自动判定结果。
const (
	VerdictPass         = "pass"
	VerdictFail         = "fail"
	VerdictUndetermined = "undetermined"
)

// 结构化输出状态。
const (
	OutputNotRequested = "memory_only"
	OutputComplete     = "complete"
	OutputFailed       = "failed"
)

// 未指定预期时使用的取值。
const expectationAny = "any"

const (
	eventsFileName  = "events.jsonl"
	summaryFileName = "summary.json"
	maxInMemory     = 20000
	maxReasonRunes  = 600
	maxPayloadBytes = 4096
	// terminalQueueSize 限制尚未显示的终端行数量。终端只是展示副本，
	// 队列满时丢弃并计数；JSONL 与内存事件仍然完整写入。
	terminalQueueSize = 1024
)

// Event 是写入 JSONL 的单条诊断事件。
// 时间使用带时区的 RFC3339Nano；conn_id 在尚未取得 MQTT Client ID 时用于关联。
type Event struct {
	Seq          uint64            `json:"seq"`
	Time         string            `json:"time"`
	RunID        string            `json:"run_id"`
	ConnectionID string            `json:"connection_id,omitempty"`
	Stage        string            `json:"stage"`
	Action       string            `json:"action"`
	Result       string            `json:"result"`
	Certainty    string            `json:"certainty,omitempty"`
	Remote       string            `json:"remote,omitempty"`
	ClientID     string            `json:"client_id,omitempty"`
	Username     string            `json:"username,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	ReasonCode   string            `json:"reason_code,omitempty"`
	TLS          *TLSRecord        `json:"tls,omitempty"`
	Context      map[string]string `json:"context,omitempty"`
}

// TLSRecord 是握手后可见的 TLS 摘要；失败时只保留可观察的分类信息。
type TLSRecord struct {
	Version     string `json:"version,omitempty"`
	CipherSuite string `json:"cipher_suite,omitempty"`
	// 以下字段只在握手成功（确实能观察到 ConnectionState）时出现；
	// 握手失败时保持 null/缺失，避免把“未观察到”误读为 false 或 0。
	DidResume                *bool             `json:"did_resume,omitempty"`
	ServerName               string            `json:"server_name,omitempty"`
	NegotiatedProtocol       string            `json:"negotiated_protocol,omitempty"`
	ClientCertificatePresent *bool             `json:"client_certificate_present,omitempty"`
	PeerCertificateCount     *int              `json:"peer_certificate_count,omitempty"`
	VerifiedChainCount       *int              `json:"verified_chain_count,omitempty"`
	PeerCertificates         []CertificateInfo `json:"peer_certificates,omitempty"`
	VerifiedChainSubjects    [][]string        `json:"verified_chain_subjects,omitempty"`
	Alert                    string            `json:"alert,omitempty"`
	AlertCode                int               `json:"alert_code,omitempty"`
	FailureKind              string            `json:"failure_kind,omitempty"`
}

// CertificateInfo 是证书的非敏感摘要。
type CertificateInfo struct {
	Subject     string   `json:"subject"`
	Issuer      string   `json:"issuer"`
	Serial      string   `json:"serial"`
	NotBefore   string   `json:"not_before"`
	NotAfter    string   `json:"not_after"`
	SHA256      string   `json:"sha256_fingerprint"`
	DNSNames    []string `json:"dns_names,omitempty"`
	IPAddresses []string `json:"ip_addresses,omitempty"`
	IsCA        bool     `json:"is_ca"`
}

// RunMetadata 是人工提供的运行上下文。
type RunMetadata struct {
	DeviceModel     string `json:"device_model,omitempty"`
	FirmwareVersion string `json:"firmware_version,omitempty"`
	TestCase        string `json:"test_case,omitempty"`
	Note            string `json:"note,omitempty"`
}

// Expectations 是本轮运行的全部预期。any 表示未指定。
type Expectations struct {
	TLS               string `json:"tls"`
	MQTTConnect       string `json:"mqtt_connect"`
	ClientCertificate string `json:"client_certificate"`
}

func defaultExpectations() Expectations {
	return Expectations{TLS: expectationAny, MQTTConnect: expectationAny, ClientCertificate: expectationAny}
}

func (e Expectations) specified() bool {
	return e.TLS != expectationAny || e.MQTTConnect != expectationAny || e.ClientCertificate != expectationAny
}

// EventCounts 是事件级计数，汇总时与连接级判定分开呈现。
type EventCounts struct {
	Events            uint64 `json:"events"`
	Connections       uint64 `json:"connections"`
	TLSSuccess        uint64 `json:"tls_success"`
	TLSFailure        uint64 `json:"tls_failure"`
	AuthAccepted      uint64 `json:"mqtt_auth_accepted"`
	AuthRejected      uint64 `json:"mqtt_auth_rejected"`
	ACLDenied         uint64 `json:"publish_authorization_denied"`
	Subscriptions     uint64 `json:"subscriptions"`
	SubscribeDenied   uint64 `json:"subscriptions_denied"`
	PublishEvents     uint64 `json:"publishes"`
	DisconnectEvents  uint64 `json:"disconnects"`
	TransportClosed   uint64 `json:"transport_closed"`
	DroppedMemory     uint64 `json:"dropped_in_memory_events"`
	DroppedStructured uint64 `json:"dropped_structured_events"`
	// DroppedTerminal 只统计未显示的终端行；事件文件与内存记录不受影响。
	DroppedTerminal uint64 `json:"dropped_terminal_events"`
}

// EffectiveConfig 是实际生效配置的非敏感摘要。
type EffectiveConfig struct {
	Mode                    string            `json:"mode"`
	ListenRequested         string            `json:"listen_requested"`
	ListenerAddress         string            `json:"listener_address"`
	TLSMin                  string            `json:"tls_min,omitempty"`
	TLSMax                  string            `json:"tls_max,omitempty"`
	ClientAuth              string            `json:"client_auth,omitempty"`
	ClientCertificateNeeded bool              `json:"client_certificate_required"`
	ServerCertificates      []CertificateInfo `json:"server_certificates,omitempty"`
	ClientTrustAnchors      int               `json:"client_trust_anchors,omitempty"`
	ServerKeyFormat         string            `json:"server_key_format,omitempty"`
	ServerKeyPasswordSource string            `json:"server_key_password_source,omitempty"`
	AllowInvalidServerCert  bool              `json:"allow_invalid_server_cert"`
	BypassedChecks          []string          `json:"bypassed_server_certificate_checks,omitempty"`
	Warnings                []string          `json:"warnings,omitempty"`
	LogPayload              bool              `json:"log_payload"`
}

// ObservedFacts 是单个连接的可观察事实。unknown 表示未观察到，不是否定结论。
type ObservedFacts struct {
	TCP                  string     `json:"tcp"`
	TLS                  string     `json:"tls"`
	TLSDetail            *TLSRecord `json:"tls_detail,omitempty"`
	ClientCertificate    string     `json:"client_certificate"`
	CertEvidenceQuality  string     `json:"client_certificate_certainty,omitempty"`
	MQTTConnect          string     `json:"mqtt_connect"`
	MQTTAuth             string     `json:"mqtt_auth"`
	MQTTAuthReasonCode   string     `json:"mqtt_auth_reason_code,omitempty"`
	ClientID             string     `json:"client_id,omitempty"`
	Username             string     `json:"username,omitempty"`
	Subscriptions        uint64     `json:"subscriptions"`
	SubscriptionsDenied  uint64     `json:"subscriptions_denied"`
	Publishes            uint64     `json:"publishes"`
	AuthorizationDenied  uint64     `json:"publish_authorization_denied"`
	Disconnect           string     `json:"disconnect"`
	DisconnectReason     string     `json:"disconnect_reason,omitempty"`
	DisconnectCertainty  string     `json:"disconnect_certainty,omitempty"`
	TransportCloseReason string     `json:"transport_close_reason,omitempty"`
}

// ConnectionSummary 是一个连接的观测、预期、判定与依据。
type ConnectionSummary struct {
	ConnectionID       string        `json:"connection_id"`
	Remote             string        `json:"remote"`
	StartedAt          string        `json:"started_at"`
	EndedAt            string        `json:"ended_at,omitempty"`
	Observed           ObservedFacts `json:"observed"`
	Expected           Expectations  `json:"expected"`
	Verdict            string        `json:"verdict"`
	Evidence           []string      `json:"evidence"`
	Failures           []string      `json:"failures,omitempty"`
	UndeterminedReason []string      `json:"undetermined_reasons,omitempty"`
}

// OutputStatus 明确记录结构化输出是否完整。
type OutputStatus struct {
	Status         string   `json:"status"`
	EventsFile     string   `json:"events_file,omitempty"`
	SummaryFile    string   `json:"summary_file,omitempty"`
	EventsComplete bool     `json:"events_complete"`
	WriteErrors    []string `json:"write_errors,omitempty"`
}

// RunSummary 是单次运行的汇总，写入 summary.json。
type RunSummary struct {
	RunID           string              `json:"run_id"`
	StartedAt       string              `json:"started_at"`
	EndedAt         string              `json:"ended_at,omitempty"`
	DurationMS      int64               `json:"duration_ms"`
	Metadata        RunMetadata         `json:"metadata"`
	EffectiveConfig EffectiveConfig     `json:"effective_config"`
	Expectations    Expectations        `json:"expectations"`
	Counts          EventCounts         `json:"counts"`
	Connections     []ConnectionSummary `json:"connections"`
	Verdict         string              `json:"verdict"`
	VerdictReasons  []string            `json:"verdict_reasons"`
	Output          OutputStatus        `json:"output"`
}

// DiagnosticsOptions 是创建诊断记录器所需参数。
type DiagnosticsOptions struct {
	Config       *Config
	RunID        string
	Metadata     RunMetadata
	ResultsDir   string
	Expectations *Expectations
	Terminal     io.Writer
	ErrorWriter  io.Writer
	Now          func() time.Time
}

// Diagnostics 是并发安全的事件记录与结果导出器。
// 文件写入、内存事件和汇总共用同一把锁，保证 JSONL 顺序与汇总一致。
type Diagnostics struct {
	mu sync.Mutex

	runID        string
	metadata     RunMetadata
	expectations Expectations
	now          func() time.Time
	terminalOut  *terminalWriter
	errorWriter  io.Writer
	logPayload   bool

	startedAt time.Time
	endedAt   time.Time
	seq       uint64
	events    []Event
	order     []*connectionRecord
	byID      map[string]*connectionRecord
	counts    EventCounts
	secrets   []string

	mode               SecurityMode
	listenRequested    string
	listenerAddress    string
	tlsMin             string
	tlsMax             string
	clientAuth         string
	clientCertRequired bool
	allowInvalidCert   bool
	resultsDir         string
	eventsPath         string
	summaryPath        string
	report             *CertificateReport
	serverCertificates []CertificateInfo

	eventsFile   *os.File
	eventsOffset int64
	eventsBroken bool
	droppedFile  uint64
	writeErrors  []string

	closed    bool
	closeOnce sync.Once
	closeErr  error
	final     *RunSummary
}

type connectionRecord struct {
	id      string
	remote  string
	started time.Time
	ended   time.Time

	tcpAccepted bool
	tlsResult   string // success|failure|not_applicable|unknown
	tlsDetail   *TLSRecord

	clientCert          string // present|absent|unknown
	clientCertCertainty string

	connectReceived bool
	authResult      string // accepted|rejected|unknown
	authCode        string
	clientID        string
	username        string

	subscriptions       uint64
	subscriptionsDenied uint64
	publishes           uint64
	aclDenied           uint64

	disconnect        string // observed|not_observed
	disconnectReason  string
	disconnectCertain string
	transportClose    string
	transportCertain  string
	closed            bool
}

// NewDiagnostics 创建诊断记录器；请求结果目录时会以 O_EXCL 创建 events.jsonl，
// 避免静默覆盖上一轮结果。
func NewDiagnostics(opts DiagnosticsOptions) (*Diagnostics, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	terminal := opts.Terminal
	if terminal == nil {
		terminal = os.Stdout
	}
	errorWriter := opts.ErrorWriter
	if errorWriter == nil {
		errorWriter = os.Stderr
	}
	runID := strings.TrimSpace(opts.RunID)
	if runID == "" {
		runID = generateRunID(now())
	}
	if err := validateRunID(runID); err != nil {
		return nil, err
	}
	expectations := defaultExpectations()
	if opts.Expectations != nil {
		expectations = *opts.Expectations
	} else if opts.Config != nil {
		expectations = Expectations{
			TLS:               normalizeExpectation(opts.Config.ExpectTLS, "tls", []string{expectationAny, "success", "failure"}),
			MQTTConnect:       normalizeExpectation(opts.Config.ExpectConnect, "mqtt connect", []string{expectationAny, "accepted", "rejected"}),
			ClientCertificate: normalizeExpectation(opts.Config.ExpectClientCert, "client certificate", []string{expectationAny, "present", "absent"}),
		}
	}
	if expectations.TLS == "" {
		expectations.TLS = expectationAny
	}
	if expectations.MQTTConnect == "" {
		expectations.MQTTConnect = expectationAny
	}
	if expectations.ClientCertificate == "" {
		expectations.ClientCertificate = expectationAny
	}
	if err := validateExpectations(expectations); err != nil {
		return nil, err
	}

	shutdownTimeout := defaultShutdownTimeout
	if opts.Config != nil && opts.Config.ShutdownTimeout > 0 {
		shutdownTimeout = opts.Config.ShutdownTimeout
	}
	d := &Diagnostics{
		runID:        runID,
		metadata:     opts.Metadata,
		expectations: expectations,
		now:          now,
		terminalOut:  newTerminalWriter(terminal, shutdownTimeout),
		errorWriter:  errorWriter,
		startedAt:    now(),
		byID:         map[string]*connectionRecord{},
		resultsDir:   opts.ResultsDir,
		writeErrors:  []string{},
	}
	if opts.Config != nil {
		d.mode = opts.Config.Mode
		d.listenRequested = opts.Config.Listen
		d.logPayload = opts.Config.LogPayload
		d.allowInvalidCert = opts.Config.AllowInvalidServerCert
		d.tlsMin = opts.Config.TLSMinVersion
		d.tlsMax = opts.Config.TLSMaxVersion
		if d.mode == ModeMTLS {
			d.clientAuth = "RequireAndVerifyClientCert"
			d.clientCertRequired = true
		} else if d.mode == ModeTLS {
			d.clientAuth = "NoClientCert"
		}
	}

	if err := d.openResultsLocked(); err != nil {
		return nil, err
	}

	d.emitLocked(Event{
		Stage:   StageRun,
		Action:  "start",
		Result:  ResultInfo,
		Context: d.runStartContext(),
	})
	return d, nil
}

func generateRunID(t time.Time) string {
	suffix := make([]byte, 3)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Sprintf("run-%d", t.UnixNano())
	}
	return fmt.Sprintf("run-%s-%s", t.UTC().Format("20060102T150405Z"), hex.EncodeToString(suffix))
}

func validateRunID(id string) error {
	if id == "" {
		return errors.New("run id must not be empty")
	}
	if utf8.RuneCountInString(id) > 64 {
		return errors.New("run id is longer than 64 characters")
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return errors.New("run id must not contain control characters")
		}
	}
	return nil
}

func validateExpectations(e Expectations) error {
	checks := []struct {
		name   string
		value  string
		values []string
	}{
		{"tls", e.TLS, []string{expectationAny, "success", "failure"}},
		{"mqtt connect", e.MQTTConnect, []string{expectationAny, "accepted", "rejected"}},
		{"client certificate", e.ClientCertificate, []string{expectationAny, "present", "absent"}},
	}
	for _, check := range checks {
		if !containsString(check.values, check.value) {
			return fmt.Errorf("invalid %s expectation %q; supported values: %s", check.name, check.value, strings.Join(check.values, ", "))
		}
	}
	return nil
}

func normalizeExpectation(value, name string, allowed []string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return expectationAny
	}
	if !containsString(allowed, value) {
		// 参数校验层已经拒绝非法值；这里保留任何值，让 validateExpectations 报错。
		return value
	}
	return value
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func (d *Diagnostics) openResultsLocked() error {
	if d.resultsDir == "" {
		return nil
	}
	if err := os.MkdirAll(d.resultsDir, 0o755); err != nil {
		return fmt.Errorf("cannot create results directory %s: %w", d.resultsDir, err)
	}
	d.eventsPath = filepath.Join(d.resultsDir, eventsFileName)
	d.summaryPath = filepath.Join(d.resultsDir, summaryFileName)
	if _, err := os.Stat(d.eventsPath); err == nil {
		return fmt.Errorf("refusing to overwrite existing events file: %s", d.eventsPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect events file %s: %w", d.eventsPath, err)
	}
	if _, err := os.Stat(d.summaryPath); err == nil {
		return fmt.Errorf("refusing to overwrite existing summary file: %s", d.summaryPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect summary file %s: %w", d.summaryPath, err)
	}
	file, err := os.OpenFile(d.eventsPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("cannot create events file %s: %w", d.eventsPath, err)
	}
	d.eventsFile = file
	return nil
}

func (d *Diagnostics) runStartContext() map[string]string {
	ctx := map[string]string{
		"mode": string(d.mode),
	}
	if d.listenRequested != "" {
		ctx["listen_requested"] = d.listenRequested
	}
	if d.mode != ModePlain {
		ctx["tls_min"] = d.tlsMin
		ctx["tls_max"] = d.tlsMax
		ctx["client_auth"] = d.clientAuth
	}
	if d.resultsDir != "" {
		ctx["results_dir"] = d.resultsDir
	}
	return ctx
}

// SetServerInfo 在监听器成功绑定后写入实际地址、证书摘要与 TLS 客户端认证策略。
// clientAuth 为 nil 表示未启用 TLS。
func (d *Diagnostics) SetServerInfo(address string, report *CertificateReport, chain []*x509.Certificate, clientAuth *tls.ClientAuthType) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listenerAddress = address
	if clientAuth != nil {
		d.clientAuth = clientAuth.String()
		d.clientCertRequired = *clientAuth == tls.RequireAnyClientCert || *clientAuth == tls.RequireAndVerifyClientCert
	}
	if report != nil {
		copied := *report
		copied.Warnings = append([]string(nil), report.Warnings...)
		copied.Bypassed = append([]string(nil), report.Bypassed...)
		d.report = &copied
	}
	d.serverCertificates = nil
	for _, cert := range chain {
		d.serverCertificates = append(d.serverCertificates, certificateInfo(cert))
	}
	ctx := map[string]string{"mode": string(d.mode)}
	if address != "" {
		ctx["listener_address"] = address
	}
	if d.mode != ModePlain {
		ctx["tls_min"] = d.tlsMin
		ctx["tls_max"] = d.tlsMax
		ctx["client_auth"] = d.clientAuth
	}
	d.emitLocked(Event{
		Stage:   StageRun,
		Action:  "listening",
		Result:  ResultInfo,
		Context: ctx,
	})
}

// RunID 返回本轮运行标识。
func (d *Diagnostics) RunID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.runID
}

// ResultsPaths 返回结果文件路径；未请求时为空。
func (d *Diagnostics) ResultsPaths() (events, summary string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.eventsPath, d.summaryPath
}

// AddSecret 注册需要在后续文本中脱敏的敏感值（例如 MQTT 密码）。
func (d *Diagnostics) AddSecret(value string) {
	if value == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.addSecretLocked(value)
}

func (d *Diagnostics) logPayloadEnabled() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.logPayload
}

func (d *Diagnostics) addSecretLocked(value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	for _, existing := range d.secrets {
		if existing == value {
			return
		}
	}
	d.secrets = append(d.secrets, value)
}

// SanitizeError 返回可写入日志的错误摘要：单行、受限长度、已脱敏。
func (d *Diagnostics) SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sanitizeLocked(err.Error())
}

func (d *Diagnostics) sanitizeLocked(text string) string {
	text = d.redactSecretsLocked(text)
	// 终端输出不接受控制字符（含 CR/LF、ESC、NUL），避免伪造日志行或终端转义。
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	if utf8.RuneCountInString(text) > maxReasonRunes {
		runes := []rune(text)
		text = string(runes[:maxReasonRunes]) + "…"
	}
	return strings.TrimSpace(text)
}

func (d *Diagnostics) redactSecretsLocked(text string) string {
	for _, secret := range d.secrets {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return redactPEMPrivateKeys(text)
}

func redactPEMPrivateKeys(text string) string {
	const begin = "-----BEGIN "
	const end = "-----END "
	const dashes = "-----"
	for {
		start := strings.Index(text, begin)
		if start < 0 {
			return text
		}
		// 从 "-----BEGIN " 之后查找结束的五连字符，避免立即匹配到起始符本身。
		labelStart := start + len(begin)
		labelEnd := strings.Index(text[labelStart:], dashes)
		if labelEnd < 0 {
			return text
		}
		headerEnd := labelStart + labelEnd
		header := text[start:headerEnd]
		if !strings.Contains(header, "PRIVATE KEY") {
			// 跳过该区块，继续查找后续区块。
			next := strings.Index(text[headerEnd+len(dashes):], begin)
			if next < 0 {
				return text
			}
			prefix := text[:headerEnd+len(dashes)]
			text = prefix + redactPEMPrivateKeys(text[headerEnd+len(dashes):])
			return text
		}
		endIndex := strings.Index(text[headerEnd+len(dashes):], end)
		if endIndex < 0 {
			text = text[:start] + "[REDACTED PRIVATE KEY]"
			return text
		}
		endIndex += headerEnd + len(dashes)
		lineEnd := strings.Index(text[endIndex+len(dashes):], dashes)
		if lineEnd < 0 {
			text = text[:start] + "[REDACTED PRIVATE KEY]"
			return text
		}
		text = text[:start] + "[REDACTED PRIVATE KEY]" + text[endIndex+lineEnd+2*len(dashes):]
	}
}

// ConnectionAccepted 记录 TCP 接入并返回连接标识。
func (d *Diagnostics) ConnectionAccepted(remote, listener string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	id := d.newConnectionIDLocked()
	now := d.now()
	rec := &connectionRecord{
		id:          id,
		remote:      remote,
		started:     now,
		tcpAccepted: true,
		tlsResult:   ResultUnknown,
		clientCert:  ResultUnknown,
		authResult:  ResultUnknown,
		disconnect:  "not_observed",
	}
	d.byID[id] = rec
	d.order = append(d.order, rec)
	d.counts.Connections++
	ctx := map[string]string{"tls": boolString(d.mode != ModePlain)}
	if listener != "" {
		ctx["listener"] = listener
	}
	d.emitLocked(Event{
		ConnectionID: id,
		Stage:        StageTransport,
		Action:       "accept",
		Result:       ResultAccepted,
		Remote:       remote,
		Context:      ctx,
	})
	return id
}

func (d *Diagnostics) newConnectionIDLocked() string {
	for {
		suffix := make([]byte, 4)
		if _, err := rand.Read(suffix); err != nil {
			return fmt.Sprintf("conn-%d", d.counts.Connections+1)
		}
		id := "conn-" + hex.EncodeToString(suffix)
		if _, exists := d.byID[id]; !exists {
			return id
		}
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func boolPointerString(value *bool) string {
	if value == nil {
		return "unknown"
	}
	return boolString(*value)
}

func intPointerString(value *int) string {
	if value == nil {
		return "unknown"
	}
	return strconv.Itoa(*value)
}

// TLSHandshakeSucceeded 记录握手成功及证书摘要。
func (d *Diagnostics) TLSHandshakeSucceeded(id string, state tls.ConnectionState) {
	record := tlsRecordFromState(state)
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.byID[id]
	if rec != nil {
		rec.tlsResult = ResultSuccess
		rec.tlsDetail = &record
		clientCertPresent := record.ClientCertificatePresent != nil && *record.ClientCertificatePresent
		verifiedChains := 0
		if record.VerifiedChainCount != nil {
			verifiedChains = *record.VerifiedChainCount
		}
		switch {
		case !clientCertPresent:
			rec.clientCert = "absent"
			rec.clientCertCertainty = CertaintyObserved
		case verifiedChains == 0:
			// 证书存在但没有经验证的链，单独记录，不能当作“已提供有效证书”。
			rec.clientCert = "present_unverified"
			rec.clientCertCertainty = CertaintyObserved
		default:
			rec.clientCert = "present"
			rec.clientCertCertainty = CertaintyObserved
		}
	}
	d.counts.TLSSuccess++
	ctx := map[string]string{
		"version":        record.Version,
		"cipher_suite":   record.CipherSuite,
		"did_resume":     boolPointerString(record.DidResume),
		"client_cert":    boolPointerString(record.ClientCertificatePresent),
		"verified_chain": intPointerString(record.VerifiedChainCount),
	}
	if record.ServerName != "" {
		ctx["sni"] = record.ServerName
	}
	if record.NegotiatedProtocol != "" {
		ctx["alpn"] = record.NegotiatedProtocol
	}
	d.emitLocked(Event{
		ConnectionID: id,
		Stage:        StageTLS,
		Action:       "handshake",
		Result:       ResultSuccess,
		Certainty:    CertaintyObserved,
		Remote:       remoteOf(rec),
		TLS:          &record,
		Context:      ctx,
	})
}

// TLSHandshakeFailed 记录握手失败，并保留错误分类、确定性与脱敏原因。
// 返回值为本次失败的证据确定性，供传输层关闭事件复用。
func (d *Diagnostics) TLSHandshakeFailed(id string, err error) string {
	failure := classifyTLSFailure(err)
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.byID[id]
	if rec != nil {
		rec.tlsResult = ResultFailure
		rec.tlsDetail = &failure.Record
		if failure.ClientCertMissing {
			rec.clientCert = "absent"
			rec.clientCertCertainty = failure.Certainty
		}
	}
	d.counts.TLSFailure++
	record := failure.Record
	ctx := map[string]string{"failure_kind": failure.Kind}
	if failure.Alert != "" {
		ctx["alert"] = failure.Alert
	}
	if failure.RawReason != "" {
		ctx["error"] = d.sanitizeLocked(failure.RawReason)
	}
	if failure.ClientCertMissing {
		ctx["client_certificate"] = "absent_heuristic"
		ctx["client_certificate_certainty"] = failure.Certainty
	}
	d.emitLocked(Event{
		ConnectionID: id,
		Stage:        StageTLS,
		Action:       "handshake",
		Result:       ResultFailure,
		Certainty:    failure.Certainty,
		Remote:       remoteOf(rec),
		Reason:       d.sanitizeLocked(failure.Reason),
		ReasonCode:   failure.ReasonCode,
		TLS:          &record,
		Context:      ctx,
	})
	return failure.Certainty
}

// MQTTConnectReceived 表示收到 CONNECT 报文，此时尚未完成认证。
func (d *Diagnostics) MQTTConnectReceived(id, clientID, username string, protocolVersion byte, cleanStart bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.byID[id]
	if rec != nil {
		rec.connectReceived = true
		if clientID != "" {
			rec.clientID = clientID
		}
		rec.username = username
	}
	ctx := map[string]string{
		"protocol_version": strconv.Itoa(int(protocolVersion)),
		"clean_start":      boolString(cleanStart),
		"authenticated":    "false",
	}
	d.emitLocked(Event{
		ConnectionID: id,
		Stage:        StageMQTTConnect,
		Action:       "connect",
		Result:       ResultReceived,
		Certainty:    CertaintyObserved,
		Remote:       remoteOf(rec),
		ClientID:     clientID,
		Username:     username,
		Context:      ctx,
	})
}

// MQTTConnack 以 CONNACK 返回码判定 MQTT 认证结果；不把 CONNECT 当作成功。
func (d *Diagnostics) MQTTConnack(id, clientID string, code byte, sessionPresent bool, protocolVersion byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.byID[id]
	if rec != nil {
		if clientID != "" {
			rec.clientID = clientID
		}
		if code == 0 {
			rec.authResult = ResultAccepted
		} else {
			rec.authResult = ResultRejected
			if rec.disconnectReason == "" {
				rec.disconnectReason = "mqtt_connack_rejected"
				rec.disconnectCertain = CertaintyObserved
			}
		}
		rec.authCode = fmt.Sprintf("0x%02X", code)
	}
	result := ResultAccepted
	if code != 0 {
		result = ResultRejected
		d.counts.AuthRejected++
	} else {
		d.counts.AuthAccepted++
	}
	ctx := map[string]string{
		"return_code":      fmt.Sprintf("0x%02X", code),
		"return_code_name": connackCodeName(code),
		"session_present":  boolString(sessionPresent),
		"protocol_version": strconv.Itoa(int(protocolVersion)),
	}
	d.emitLocked(Event{
		ConnectionID: id,
		Stage:        StageMQTTAuth,
		Action:       "connack",
		Result:       result,
		Certainty:    CertaintyObserved,
		Remote:       remoteOf(rec),
		ClientID:     clientID,
		ReasonCode:   fmt.Sprintf("0x%02X", code),
		Context:      ctx,
	})
}

func connackCodeName(code byte) string {
	switch code {
	case 0x00:
		return "accepted"
	case 0x01:
		return "unacceptable_protocol_version"
	case 0x02:
		return "identifier_rejected"
	case 0x03:
		return "server_unavailable"
	case 0x04:
		return "bad_username_or_password"
	case 0x05:
		return "not_authorized"
	case 0x80:
		return "unspecified_error"
	case 0x81:
		return "malformed_packet"
	case 0x82:
		return "protocol_error"
	case 0x83:
		return "implementation_specific_error"
	case 0x84:
		return "unsupported_protocol_version"
	case 0x85:
		return "client_identifier_not_valid"
	case 0x86:
		return "bad_username_or_password"
	case 0x87:
		return "not_authorized"
	case 0x88:
		return "server_unavailable"
	case 0x89:
		return "server_busy"
	case 0x8A:
		return "banned"
	case 0x8C:
		return "bad_authentication_method"
	case 0x90:
		return "topic_name_invalid"
	case 0x95:
		return "packet_too_large"
	case 0x97:
		return "quota_exceeded"
	case 0x99:
		return "payload_format_invalid"
	case 0x9A:
		return "retain_not_supported"
	case 0x9B:
		return "qos_not_supported"
	case 0x9C:
		return "use_another_server"
	case 0x9D:
		return "server_moved"
	case 0x9F:
		return "connection_rate_exceeded"
	default:
		return "unknown"
	}
}

// SubscriptionOutcome 是单个主题过滤器的订阅授权结果。
type SubscriptionOutcome struct {
	Filter       string
	RequestedQoS byte
	GrantedQoS   byte
	Code         byte
}

// Subscribed 记录 SUBACK 中每个过滤器的授权结果。
func (d *Diagnostics) Subscribed(id, clientID string, outcomes []SubscriptionOutcome) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.byID[id]
	if rec != nil && clientID != "" {
		rec.clientID = clientID
	}
	for _, outcome := range outcomes {
		granted := outcome.Code <= 2
		if rec != nil {
			rec.subscriptions++
			if !granted {
				rec.subscriptionsDenied++
			}
		}
		d.counts.Subscriptions++
		ctx := map[string]string{
			"filter":        outcome.Filter,
			"requested_qos": strconv.Itoa(int(outcome.RequestedQoS)),
			"return_code":   fmt.Sprintf("0x%02X", outcome.Code),
		}
		result := ResultGranted
		if !granted {
			d.counts.SubscribeDenied++
			result = ResultDenied
			ctx["granted_qos"] = ""
			ctx["authorization"] = "denied_or_invalid"
		} else {
			ctx["granted_qos"] = strconv.Itoa(int(outcome.GrantedQoS))
		}
		d.emitLocked(Event{
			ConnectionID: id,
			Stage:        StageSubscribe,
			Action:       "subscribe",
			Result:       result,
			Certainty:    CertaintyObserved,
			Remote:       remoteOf(rec),
			ClientID:     clientID,
			ReasonCode:   fmt.Sprintf("0x%02X", outcome.Code),
			Context:      ctx,
		})
	}
}

// Unsubscribed 记录取消订阅。
func (d *Diagnostics) Unsubscribed(id, clientID string, filters []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.byID[id]
	ctx := map[string]string{"filters": strings.Join(filters, ",")}
	d.emitLocked(Event{
		ConnectionID: id,
		Stage:        StageUnsubscribe,
		Action:       "unsubscribe",
		Result:       ResultObserved,
		Certainty:    CertaintyObserved,
		Remote:       remoteOf(rec),
		ClientID:     clientID,
		Context:      ctx,
	})
}

// Publish 记录已被 ACL 接受的发布。payload 仅在显式启用时传入。
func (d *Diagnostics) Publish(id, clientID, topic string, qos byte, retain bool, payload []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.byID[id]
	if rec != nil {
		rec.publishes++
	}
	d.counts.PublishEvents++
	ctx := map[string]string{
		"topic":  topic,
		"qos":    strconv.Itoa(int(qos)),
		"retain": boolString(retain),
	}
	if d.logPayload && payload != nil {
		addPayloadContext(ctx, payload)
	}
	d.emitLocked(Event{
		ConnectionID: id,
		Stage:        StagePublish,
		Action:       "publish",
		Result:       ResultAccepted,
		Certainty:    CertaintyObserved,
		Remote:       remoteOf(rec),
		ClientID:     clientID,
		Context:      ctx,
	})
}

func addPayloadContext(ctx map[string]string, payload []byte) {
	truncated := false
	if len(payload) > maxPayloadBytes {
		payload = payload[:maxPayloadBytes]
		truncated = true
	}
	if utf8.Valid(payload) {
		ctx["payload"] = string(payload)
	} else {
		ctx["payload_base64"] = base64.StdEncoding.EncodeToString(payload)
	}
	if truncated {
		ctx["payload_truncated"] = "true"
	}
}

// ACLDenied 记录发布主题被拒绝。仅对 write=true 的检查记录，
// 订阅授权已由 SUBACK 返回码体现。
func (d *Diagnostics) ACLDenied(id, clientID, topic string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.byID[id]
	if rec != nil {
		rec.aclDenied++
	}
	d.counts.ACLDenied++
	d.emitLocked(Event{
		ConnectionID: id,
		Stage:        StageAuthorization,
		Action:       "publish",
		Result:       ResultDenied,
		Certainty:    CertaintyObserved,
		Remote:       remoteOf(rec),
		ClientID:     clientID,
		Context:      map[string]string{"topic": topic},
	})
}

// MQTTDisconnected 记录 MQTT 层断开；错误原因经过分类与脱敏。
func (d *Diagnostics) MQTTDisconnected(id, clientID string, err error, expire bool) {
	reason, code, certainty := classifyMQTTError(err)
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.byID[id]
	if rec != nil {
		rec.disconnect = "observed"
		rec.disconnectReason = reason
		rec.disconnectCertain = certainty
	}
	d.counts.DisconnectEvents++
	result := ResultClosed
	if err != nil {
		result = ResultFailure
	}
	ctx := map[string]string{"session_expired": boolString(expire)}
	if code != "" {
		ctx["reason_code"] = code
	}
	d.emitLocked(Event{
		ConnectionID: id,
		Stage:        StageDisconnect,
		Action:       "disconnect",
		Result:       result,
		Certainty:    certainty,
		Remote:       remoteOf(rec),
		ClientID:     clientID,
		Reason:       d.sanitizeLocked(reason),
		ReasonCode:   code,
		Context:      ctx,
	})
}

// ConnectionClosed 记录传输层关闭；与 MQTT 断开事件分开，避免丢失原因。
func (d *Diagnostics) ConnectionClosed(id string, err error) {
	reason := "connection_closed"
	certainty := CertaintyObserved
	if err != nil {
		kind, classified, classCertainty := classifyTransportError(err)
		reason = kind + ": " + classified
		certainty = classCertainty
	}
	d.ConnectionClosedWithReason(id, reason, certainty)
}

// ConnectionClosedWithReason 记录已知原因的传输层关闭，例如 TLS 握手失败。
func (d *Diagnostics) ConnectionClosedWithReason(id, reason, certainty string) {
	if certainty == "" {
		certainty = CertaintyObserved
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.byID[id]
	if rec != nil {
		if rec.closed {
			return
		}
		rec.closed = true
		rec.ended = d.now()
		rec.transportClose = reason
		rec.transportCertain = certainty
	}
	d.counts.TransportClosed++
	d.emitLocked(Event{
		ConnectionID: id,
		Stage:        StageTransport,
		Action:       "close",
		Result:       ResultClosed,
		Certainty:    certainty,
		Remote:       remoteOf(rec),
		Reason:       d.sanitizeLocked(reason),
	})
}

func remoteOf(rec *connectionRecord) string {
	if rec == nil {
		return ""
	}
	return rec.remote
}

// emitLocked 追加事件、写 JSONL 并输出终端行。调用方必须持有 d.mu。
func (d *Diagnostics) emitLocked(event Event) {
	if d.closed {
		return
	}
	d.seq++
	event.Seq = d.seq
	event.Time = d.now().Format(time.RFC3339Nano)
	event.RunID = d.runID
	if event.Certainty == "" {
		event.Certainty = CertaintyObserved
	}
	event.Reason = d.sanitizeLocked(event.Reason)
	event.ClientID = d.sanitizeLocked(event.ClientID)
	event.Username = d.sanitizeLocked(event.Username)
	if event.Context != nil {
		sanitized := make(map[string]string, len(event.Context))
		for key, value := range event.Context {
			switch key {
			case "payload", "payload_base64":
				// 负载已经按需截断，保留原始空白，只做秘密替换。
				sanitized[key] = d.redactSecretsLocked(value)
			default:
				sanitized[d.sanitizeLocked(key)] = d.sanitizeLocked(value)
			}
		}
		event.Context = sanitized
	}
	if len(d.events) >= maxInMemory {
		d.events = d.events[1:]
		d.counts.DroppedMemory++
	}
	d.events = append(d.events, event)
	d.counts.Events++
	d.writeEventLocked(event)
	d.printEventLocked(event)
}

func (d *Diagnostics) writeEventLocked(event Event) {
	if d.eventsFile == nil {
		return
	}
	if d.eventsBroken {
		d.droppedFile++
		d.counts.DroppedStructured++
		return
	}
	data, err := json.Marshal(event)
	if err == nil {
		data = append(data, '\n')
		start := d.eventsOffset
		n, writeErr := d.eventsFile.Write(data)
		if writeErr == nil && n != len(data) {
			writeErr = io.ErrShortWrite
		}
		if writeErr == nil {
			d.eventsOffset += int64(n)
			return
		}
		// 避免留下半行 JSON：回退到上一条完整记录。
		if truncateErr := d.eventsFile.Truncate(start); truncateErr == nil {
			_, _ = d.eventsFile.Seek(start, io.SeekStart)
			d.eventsOffset = start
		}
		err = writeErr
	}
	d.eventsBroken = true
	d.droppedFile++
	d.counts.DroppedStructured++
	d.noteWriteErrorLocked(fmt.Errorf("cannot write events file: %w", err))
}

func (d *Diagnostics) noteWriteErrorLocked(err error) {
	message := d.sanitizeLocked(err.Error())
	for _, existing := range d.writeErrors {
		if existing == message {
			return
		}
	}
	d.writeErrors = append(d.writeErrors, message)
	if d.errorWriter != nil {
		fmt.Fprintf(d.errorWriter, "diagnostics: %s\n", message)
	}
}

func (d *Diagnostics) printEventLocked(event Event) {
	if d.terminalOut == nil {
		return
	}
	var b strings.Builder
	b.WriteString(event.Time)
	b.WriteString(" [")
	b.WriteString(event.Stage)
	b.WriteString("] ")
	if event.ConnectionID != "" {
		b.WriteString("conn=")
		b.WriteString(event.ConnectionID)
		b.WriteByte(' ')
	}
	b.WriteString(event.Action)
	b.WriteByte('/')
	b.WriteString(event.Result)
	if event.Remote != "" {
		b.WriteString(" remote=")
		b.WriteString(event.Remote)
	}
	if event.ClientID != "" {
		b.WriteString(" client=")
		b.WriteString(event.ClientID)
	}
	if event.ReasonCode != "" {
		b.WriteString(" code=")
		b.WriteString(event.ReasonCode)
	}
	if event.TLS != nil {
		if event.TLS.Version != "" {
			b.WriteString(" tls=")
			b.WriteString(event.TLS.Version)
		}
		if event.TLS.CipherSuite != "" {
			b.WriteString(" cipher=")
			b.WriteString(event.TLS.CipherSuite)
		}
		if event.Result == ResultSuccess {
			b.WriteString(" resume=")
			b.WriteString(boolPointerString(event.TLS.DidResume))
			b.WriteString(" client_cert=")
			b.WriteString(boolPointerString(event.TLS.ClientCertificatePresent))
			b.WriteString(" verified_chains=")
			b.WriteString(intPointerString(event.TLS.VerifiedChainCount))
		}
	}
	if event.Certainty != "" && event.Certainty != CertaintyObserved {
		b.WriteString(" certainty=")
		b.WriteString(event.Certainty)
	}
	if event.Reason != "" {
		b.WriteString(" reason=")
		b.WriteString(event.Reason)
	}
	if topic := event.Context["topic"]; topic != "" {
		b.WriteString(" topic=")
		b.WriteString(topic)
	}
	if filters := event.Context["filter"]; filters != "" {
		b.WriteString(" filter=")
		b.WriteString(filters)
	}
	if payload, ok := event.Context["payload"]; ok {
		b.WriteString(" payload=")
		b.WriteString(strconv.Quote(payload))
	}
	if payload, ok := event.Context["payload_base64"]; ok {
		b.WriteString(" payload_base64=")
		b.WriteString(payload)
	}
	b.WriteByte('\n')
	if !d.terminalOut.enqueue(b.String()) {
		d.counts.DroppedTerminal++
	}
}

// terminalWriter 串行输出诊断事件行。写入失败只影响终端显示，
// 不会回滚已经写入 JSONL 的事件。
type terminalWriter struct {
	writer  io.Writer
	queue   chan string
	done    chan struct{}
	timeout time.Duration

	mu      sync.Mutex
	errs    []string
	stopped bool
}

func newTerminalWriter(writer io.Writer, timeout time.Duration) *terminalWriter {
	t := &terminalWriter{
		writer:  writer,
		queue:   make(chan string, terminalQueueSize),
		done:    make(chan struct{}),
		timeout: timeout,
	}
	go t.run()
	return t
}

func (t *terminalWriter) run() {
	defer close(t.done)
	for line := range t.queue {
		if _, err := io.WriteString(t.writer, line); err != nil {
			t.mu.Lock()
			if len(t.errs) < 8 {
				t.errs = append(t.errs, err.Error())
			}
			t.mu.Unlock()
		}
	}
}

// enqueue 在队列已满时立即返回 false，不阻塞连接处理。
func (t *terminalWriter) enqueue(line string) bool {
	select {
	case t.queue <- line:
		return true
	default:
		return false
	}
}

// stop 关闭队列并等待排空；超时或写入错误会作为摘要返回。
func (t *terminalWriter) stop() []string {
	t.mu.Lock()
	if !t.stopped {
		t.stopped = true
		close(t.queue)
	}
	t.mu.Unlock()

	var messages []string
	if t.timeout > 0 {
		timer := time.NewTimer(t.timeout)
		defer timer.Stop()
		select {
		case <-t.done:
		case <-timer.C:
			messages = append(messages, fmt.Sprintf("terminal output did not flush within %s", t.timeout))
		}
	} else {
		<-t.done
	}
	t.mu.Lock()
	for _, errText := range t.errs {
		messages = append(messages, "terminal output: "+errText)
	}
	t.mu.Unlock()
	return messages
}

// Events 返回内存中的事件副本（可能因上限丢弃最旧事件）。
func (d *Diagnostics) Events() []Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Event(nil), d.events...)
}

// Summary 返回当前汇总快照；关闭后返回最终结果。
func (d *Diagnostics) Summary() RunSummary {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.final != nil {
		return *d.final
	}
	return d.buildSummaryLocked()
}

// Close 完成事件刷盘与 summary.json 写入，可重复调用。
func (d *Diagnostics) Close() error {
	d.closeOnce.Do(func() {
		d.closeErr = d.finalize()
	})
	return d.closeErr
}

func (d *Diagnostics) finalize() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.emitLocked(Event{
		Stage:   StageRun,
		Action:  "stop",
		Result:  ResultInfo,
		Context: map[string]string{"reason": "shutdown"},
	})
	d.closed = true
	d.endedAt = d.now()
	if d.terminalOut != nil {
		for _, message := range d.terminalOut.stop() {
			d.noteWriteErrorLocked(errors.New(message))
		}
		d.terminalOut = nil
	}
	if d.counts.DroppedTerminal > 0 {
		d.noteWriteErrorLocked(fmt.Errorf(
			"terminal output queue overflow: %d event(s) not displayed; structured events remain complete",
			d.counts.DroppedTerminal))
	}
	summary := d.buildSummaryLocked()

	summary.Output.Status = OutputNotRequested
	summary.Output.EventsComplete = !d.eventsBroken
	if d.resultsDir != "" {
		summary.Output.Status = OutputComplete
	}
	if d.eventsFile != nil {
		if d.eventsBroken {
			summary.Output.Status = OutputFailed
		}
		if err := d.eventsFile.Sync(); err != nil {
			d.noteWriteErrorLocked(fmt.Errorf("cannot flush events file: %w", err))
			summary.Output.Status = OutputFailed
			summary.Output.EventsComplete = false
		}
		if err := d.eventsFile.Close(); err != nil {
			d.noteWriteErrorLocked(fmt.Errorf("cannot close events file: %w", err))
			summary.Output.Status = OutputFailed
			summary.Output.EventsComplete = false
		}
		d.eventsFile = nil
	}
	summary.Output.WriteErrors = append([]string(nil), d.writeErrors...)
	if d.summaryPath != "" {
		summary.Output.SummaryFile = d.summaryPath
		if err := writeSummaryFile(d.summaryPath, summary); err != nil {
			d.noteWriteErrorLocked(err)
			summary.Output.Status = OutputFailed
			summary.Output.WriteErrors = append([]string(nil), d.writeErrors...)
			// 尝试再次写入包含错误状态的汇总；若仍失败则只能返回错误。
			if retryErr := writeSummaryFile(d.summaryPath, summary); retryErr != nil {
				d.noteWriteErrorLocked(retryErr)
				summary.Output.WriteErrors = append([]string(nil), d.writeErrors...)
			}
		}
	}
	d.final = &summary
	if len(d.writeErrors) > 0 {
		return errors.New(strings.Join(d.writeErrors, "; "))
	}
	return nil
}

func writeSummaryFile(path string, summary RunSummary) error {
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode summary: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".summary-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temporary summary in %s: %w", dir, err)
	}
	tempName := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	if err := temp.Chmod(0o644); err != nil {
		cleanup()
		return fmt.Errorf("cannot set temporary summary permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("cannot write summary: %w", err)
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("cannot flush summary: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("cannot close summary: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("cannot publish summary %s: %w", path, err)
	}
	return nil
}

func (d *Diagnostics) buildSummaryLocked() RunSummary {
	summary := RunSummary{
		RunID:        d.runID,
		StartedAt:    d.startedAt.Format(time.RFC3339Nano),
		Metadata:     d.sanitizedMetadataLocked(),
		Expectations: d.expectations,
		Counts:       d.counts,
	}
	if !d.endedAt.IsZero() {
		summary.EndedAt = d.endedAt.Format(time.RFC3339Nano)
		summary.DurationMS = d.endedAt.Sub(d.startedAt).Milliseconds()
	} else {
		summary.EndedAt = ""
		summary.DurationMS = d.now().Sub(d.startedAt).Milliseconds()
	}
	summary.EffectiveConfig = d.effectiveConfigLocked()
	summary.Output = OutputStatus{
		Status:         OutputNotRequested,
		EventsFile:     d.eventsPath,
		SummaryFile:    d.summaryPath,
		EventsComplete: !d.eventsBroken,
		WriteErrors:    append([]string(nil), d.writeErrors...),
	}
	if d.resultsDir != "" {
		summary.Output.Status = OutputComplete
	}
	if d.eventsBroken {
		summary.Output.Status = OutputFailed
	}
	summary.Connections = make([]ConnectionSummary, 0, len(d.order))
	for _, rec := range d.order {
		summary.Connections = append(summary.Connections, d.connectionSummaryLocked(rec))
	}
	summary.Verdict, summary.VerdictReasons = runVerdict(summary.Connections, d.expectations)
	return summary
}

func (d *Diagnostics) sanitizedMetadataLocked() RunMetadata {
	return RunMetadata{
		DeviceModel:     d.sanitizeLocked(d.metadata.DeviceModel),
		FirmwareVersion: d.sanitizeLocked(d.metadata.FirmwareVersion),
		TestCase:        d.sanitizeLocked(d.metadata.TestCase),
		Note:            d.sanitizeLocked(d.metadata.Note),
	}
}

func (d *Diagnostics) effectiveConfigLocked() EffectiveConfig {
	config := EffectiveConfig{
		Mode:                   string(d.mode),
		ListenRequested:        d.listenRequested,
		ListenerAddress:        d.listenerAddress,
		AllowInvalidServerCert: d.allowInvalidCert,
		LogPayload:             d.logPayload,
		ServerCertificates:     append([]CertificateInfo(nil), d.serverCertificates...),
	}
	if d.mode != ModePlain {
		config.TLSMin = d.tlsMin
		config.TLSMax = d.tlsMax
		config.ClientAuth = d.clientAuth
		config.ClientCertificateNeeded = d.clientCertRequired
	}
	if d.report != nil {
		config.ClientTrustAnchors = d.report.ClientCAs
		config.ServerKeyFormat = d.report.ServerKeyFormat
		config.ServerKeyPasswordSource = d.report.ServerKeyPasswordSource
		config.BypassedChecks = append([]string(nil), d.report.Bypassed...)
		config.Warnings = append([]string(nil), d.report.Warnings...)
	}
	return config
}

func (d *Diagnostics) connectionSummaryLocked(rec *connectionRecord) ConnectionSummary {
	observed := ObservedFacts{
		TCP:                  "accepted",
		TLS:                  rec.tlsResult,
		TLSDetail:            rec.tlsDetail,
		ClientCertificate:    rec.clientCert,
		CertEvidenceQuality:  rec.clientCertCertainty,
		MQTTConnect:          "not_received",
		MQTTAuth:             rec.authResult,
		MQTTAuthReasonCode:   rec.authCode,
		ClientID:             rec.clientID,
		Username:             rec.username,
		Subscriptions:        rec.subscriptions,
		SubscriptionsDenied:  rec.subscriptionsDenied,
		Publishes:            rec.publishes,
		AuthorizationDenied:  rec.aclDenied,
		Disconnect:           rec.disconnect,
		DisconnectReason:     rec.disconnectReason,
		DisconnectCertainty:  rec.disconnectCertain,
		TransportCloseReason: rec.transportClose,
	}
	if rec.connectReceived {
		observed.MQTTConnect = "received"
	}
	if rec.tcpAccepted && rec.tlsResult == ResultUnknown {
		observed.TLS = "unknown"
	}
	if d.mode == ModePlain {
		observed.TLS = "not_applicable"
		observed.ClientCertificate = "not_applicable"
	}
	// 汇总与事件使用同一套脱敏规则，避免自由文本客户端标识进入结果文件。
	observed.ClientID = d.sanitizeLocked(observed.ClientID)
	observed.Username = d.sanitizeLocked(observed.Username)
	summary := ConnectionSummary{
		ConnectionID: rec.id,
		Remote:       d.sanitizeLocked(rec.remote),
		StartedAt:    rec.started.Format(time.RFC3339Nano),
		Observed:     observed,
		Expected:     d.expectations,
	}
	if !rec.ended.IsZero() {
		summary.EndedAt = rec.ended.Format(time.RFC3339Nano)
	}
	summary.Verdict, summary.Evidence, summary.Failures, summary.UndeterminedReason =
		judgeConnection(d.mode, observed, d.expectations)
	return summary
}

func judgeConnection(mode SecurityMode, observed ObservedFacts, expected Expectations) (verdict string, evidence, failures, undetermined []string) {
	evidence = append(evidence,
		"tcp="+observed.TCP,
		"tls="+observed.TLS,
		"mqtt_connect="+observed.MQTTConnect,
		"mqtt_auth="+observed.MQTTAuth,
		"client_certificate="+observed.ClientCertificate,
	)
	if observed.TLSDetail != nil && observed.TLS == ResultSuccess {
		evidence = append(evidence, fmt.Sprintf("tls.version=%s", observed.TLSDetail.Version))
		evidence = append(evidence, fmt.Sprintf("tls.cipher_suite=%s", observed.TLSDetail.CipherSuite))
		evidence = append(evidence, "tls.did_resume="+boolPointerString(observed.TLSDetail.DidResume))
		evidence = append(evidence, "tls.peer_certificates="+intPointerString(observed.TLSDetail.PeerCertificateCount))
		evidence = append(evidence, "tls.verified_chains="+intPointerString(observed.TLSDetail.VerifiedChainCount))
	}
	if observed.MQTTAuthReasonCode != "" {
		evidence = append(evidence, "mqtt.connack="+observed.MQTTAuthReasonCode)
	}

	switch expected.TLS {
	case "success":
		switch observed.TLS {
		case ResultSuccess:
			// 满足。
		case ResultFailure:
			failures = append(failures, "TLS 握手失败，但预期成功")
		default:
			undetermined = append(undetermined, "未观察到完整的 TLS 握手结果")
		}
	case "failure":
		switch observed.TLS {
		case ResultFailure:
			// 满足。
		case ResultSuccess:
			failures = append(failures, "TLS 握手成功，但预期失败")
		default:
			undetermined = append(undetermined, "未观察到完整的 TLS 握手结果")
		}
	}

	switch expected.MQTTConnect {
	case "accepted":
		switch observed.MQTTAuth {
		case ResultAccepted:
			// 满足。
		case ResultRejected:
			failures = append(failures, "MQTT 认证被拒绝（CONNACK "+observed.MQTTAuthReasonCode+"），但预期接受")
		default:
			undetermined = append(undetermined, "未观察到 CONNACK，无法确认 MQTT 认证结果")
		}
	case "rejected":
		switch observed.MQTTAuth {
		case ResultRejected:
			// 满足。
		case ResultAccepted:
			failures = append(failures, "MQTT 认证被接受，但预期拒绝")
		default:
			undetermined = append(undetermined, "未观察到 CONNACK，无法确认 MQTT 认证结果")
		}
	}

	if expected.ClientCertificate != expectationAny {
		switch {
		case mode == ModePlain:
			undetermined = append(undetermined, "未启用 TLS，无法观察设备是否提供证书")
		case mode != ModeMTLS:
			undetermined = append(undetermined, "单向 TLS 模式未请求设备证书，未提供证书不代表设备能力")
		case observed.TLS != ResultSuccess:
			if observed.CertEvidenceQuality == CertaintyHeuristic && observed.ClientCertificate == "absent" {
				undetermined = append(undetermined, "TLS 握手未完成；错误提示可能表示未提供证书，但不足以自动判定")
			} else {
				undetermined = append(undetermined, "TLS 握手未成功，无法观察客户端证书")
			}
		default:
			if observed.ClientCertificate == expected.ClientCertificate {
				// 满足。
			} else if expected.ClientCertificate == "present" {
				failures = append(failures, "未观察到经验证的客户端证书（observed="+observed.ClientCertificate+"），但预期提供")
			} else {
				if observed.ClientCertificate == "present_unverified" {
					failures = append(failures, "观察到未经验证的客户端证书，但预期不提供")
				} else {
					failures = append(failures, "观察到经验证的客户端证书，但预期不提供")
				}
			}
		}
	}

	switch {
	case len(failures) > 0:
		verdict = VerdictFail
	case len(undetermined) > 0:
		verdict = VerdictUndetermined
	case !expected.specified():
		verdict = VerdictUndetermined
		undetermined = append(undetermined, "未指定预期结果（-expect-* 均为 any）")
	default:
		verdict = VerdictPass
	}
	return verdict, evidence, failures, undetermined
}

func runVerdict(connections []ConnectionSummary, expected Expectations) (string, []string) {
	if len(connections) == 0 {
		return VerdictUndetermined, []string{"本轮没有任何连接记录"}
	}
	var reasons []string
	failed := false
	undetermined := false
	for _, conn := range connections {
		switch conn.Verdict {
		case VerdictFail:
			failed = true
			reasons = append(reasons, fmt.Sprintf("连接 %s 失败：%s", conn.ConnectionID, strings.Join(conn.Failures, "；")))
		case VerdictUndetermined:
			undetermined = true
			reasons = append(reasons, fmt.Sprintf("连接 %s 未判定：%s", conn.ConnectionID, strings.Join(conn.UndeterminedReason, "；")))
		}
	}
	switch {
	case failed:
		return VerdictFail, reasons
	case undetermined:
		return VerdictUndetermined, reasons
	case !expected.specified():
		return VerdictUndetermined, []string{"未指定预期结果，无法自动判定"}
	default:
		return VerdictPass, []string{fmt.Sprintf("所有 %d 个连接均满足明确预期", len(connections))}
	}
}

// certificateInfo 生成证书的非敏感摘要，包含 SHA-256 指纹。
func certificateInfo(cert *x509.Certificate) CertificateInfo {
	fingerprint := sha256.Sum256(cert.Raw)
	info := CertificateInfo{
		Subject:   cert.Subject.String(),
		Issuer:    cert.Issuer.String(),
		Serial:    strings.ToUpper(cert.SerialNumber.Text(16)),
		NotBefore: cert.NotBefore.Format(time.RFC3339),
		NotAfter:  cert.NotAfter.Format(time.RFC3339),
		SHA256:    formatFingerprint(fingerprint[:]),
		DNSNames:  append([]string(nil), cert.DNSNames...),
		IsCA:      cert.IsCA,
	}
	for _, ip := range cert.IPAddresses {
		info.IPAddresses = append(info.IPAddresses, ip.String())
	}
	return info
}

func formatFingerprint(raw []byte) string {
	hexText := strings.ToUpper(hex.EncodeToString(raw))
	parts := make([]string, 0, len(raw))
	for i := 0; i < len(hexText); i += 2 {
		parts = append(parts, hexText[i:i+2])
	}
	return strings.Join(parts, ":")
}

func tlsRecordFromState(state tls.ConnectionState) TLSRecord {
	didResume := state.DidResume
	clientCertPresent := len(state.PeerCertificates) > 0
	peerCertificates := len(state.PeerCertificates)
	verifiedChains := len(state.VerifiedChains)
	record := TLSRecord{
		Version:                  tls.VersionName(state.Version),
		CipherSuite:              tls.CipherSuiteName(state.CipherSuite),
		DidResume:                &didResume,
		ServerName:               state.ServerName,
		NegotiatedProtocol:       state.NegotiatedProtocol,
		ClientCertificatePresent: &clientCertPresent,
		PeerCertificateCount:     &peerCertificates,
		VerifiedChainCount:       &verifiedChains,
	}
	for _, cert := range state.PeerCertificates {
		record.PeerCertificates = append(record.PeerCertificates, certificateInfo(cert))
	}
	for _, chain := range state.VerifiedChains {
		subjects := make([]string, 0, len(chain))
		for _, cert := range chain {
			subjects = append(subjects, cert.Subject.String())
		}
		record.VerifiedChainSubjects = append(record.VerifiedChainSubjects, subjects)
	}
	return record
}
