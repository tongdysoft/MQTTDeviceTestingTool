package main

// 本文件集中处理 TLS、传输和 MQTT 错误分类。
// 分类只描述可观察到的事实；对端关闭、超时和未知 alert 保留不确定性，
// 不据此断言设备内部的失败原因。

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/mochi-mqtt/server/v2/packets"
)

type tlsFailureResult struct {
	Record     TLSRecord
	Kind       string
	Alert      string
	ReasonCode string
	Reason     string
	// RawReason 是脱敏前的底层错误链，仅作为诊断上下文保留，
	// 不作为稳定分类或自动判定的依据。
	RawReason         string
	Certainty         string
	ClientCertMissing bool
}

func classifyTLSFailure(err error) tlsFailureResult {
	result := tlsFailureResult{
		Certainty: CertaintyUnknown,
		Kind:      "unknown",
	}
	if err == nil {
		result.Reason = "TLS handshake failed without an error"
		result.Certainty = CertaintyUncertain
		return result
	}
	result.Reason = err.Error()
	result.RawReason = errorChainText(err)

	// 本地无法设置或清除期限不是对端行为：单独分类，不能归因于证书或设备。
	var setupErr *deadlineSetupError
	if errors.As(err, &setupErr) {
		result.Kind = "local_setup_error"
		result.Certainty = CertaintyObserved
		result.ReasonCode = "local_setup_error"
		result.Record.FailureKind = result.Kind
		result.Reason = setupErr.Error()
		return result
	}

	var alertErr tls.AlertError
	if errors.As(err, &alertErr) {
		code := uint8(alertErr)
		result.Record.AlertCode = int(code)
		result.Record.Alert = tlsAlertName(code)
		result.Alert = result.Record.Alert
		result.Kind = "alert"
		result.ReasonCode = fmt.Sprintf("alert:%d", code)
		if result.Record.Alert == "unknown_alert" {
			result.Certainty = CertaintyUncertain
			result.Reason = fmt.Sprintf("unknown TLS alert 0x%02X", code)
		} else {
			result.Certainty = CertaintyObserved
		}
		result.Record.FailureKind = result.Kind
		return result
	}

	if isTimeoutError(err) {
		result.Kind = "timeout"
		result.Certainty = CertaintyUncertain
		result.ReasonCode = "timeout"
		result.Record.FailureKind = result.Kind
		return result
	}

	if isPeerClosedError(err) {
		result.Kind = "peer_closed"
		result.Certainty = CertaintyUncertain
		result.ReasonCode = "peer_closed"
		result.Record.FailureKind = result.Kind
		return result
	}

	var recordErr tls.RecordHeaderError
	if errors.As(err, &recordErr) {
		result.Kind = "not_tls_record"
		result.Certainty = CertaintyObserved
		result.ReasonCode = "not_tls_record"
		result.Record.FailureKind = result.Kind
		result.Reason = "对端发送的数据不是 TLS 记录（可能使用了明文或错误的端口）"
		return result
	}

	if isCertificateVerificationError(err) {
		result.Kind = "certificate_verification"
		result.Certainty = CertaintyObserved
		result.ReasonCode = "certificate_verification"
		result.Record.FailureKind = result.Kind
		return result
	}

	if isClientCertificateMissing(err) {
		result.Kind = "client_certificate_missing"
		result.Certainty = CertaintyHeuristic
		result.ReasonCode = "client_certificate_missing"
		result.Record.FailureKind = result.Kind
		result.ClientCertMissing = true
		result.Reason = "服务端未收到客户端证书（依据库的错误提示，属于启发式判断）"
		return result
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		result.Kind = "transport_error"
		result.Certainty = CertaintyUncertain
		result.ReasonCode = "transport_error"
		result.Record.FailureKind = result.Kind
		return result
	}

	result.Kind = "handshake_error"
	result.Certainty = CertaintyObserved
	result.ReasonCode = "handshake_error"
	result.Record.FailureKind = result.Kind
	return result
}

func isTimeoutError(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// errorChainText 展开错误链并去掉已被外层消息包含的重复片段。
// 它只用于诊断上下文，不作为稳定分类依据。
func errorChainText(err error) string {
	if err == nil {
		return ""
	}
	var texts []string
	seen := map[string]bool{}
	var walk func(error, int)
	walk = func(current error, depth int) {
		if current == nil || depth > 6 {
			return
		}
		text := current.Error()
		if !seen[text] {
			seen[text] = true
			contained := false
			for _, existing := range texts {
				if strings.Contains(existing, text) {
					contained = true
					break
				}
			}
			if !contained {
				texts = append(texts, text)
			}
		}
		switch unwrapped := current.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range unwrapped.Unwrap() {
				walk(child, depth+1)
			}
		case interface{ Unwrap() error }:
			walk(unwrapped.Unwrap(), depth+1)
		}
	}
	walk(err, 0)
	return strings.Join(texts, " <- ")
}

func isPeerClosedError(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNABORTED)
}

func isCertificateVerificationError(err error) bool {
	var verification *tls.CertificateVerificationError
	if errors.As(err, &verification) {
		return true
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return true
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		return true
	}
	var hostname x509.HostnameError
	return errors.As(err, &hostname)
}

// isClientCertificateMissing 识别标准库服务端“未提供客户端证书”的提示。
// 这只是启发式分类，不把错误全文当作稳定接口；无法匹配时保留 unknown。
func isClientCertificateMissing(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "client didn't provide a certificate") ||
		(strings.Contains(text, "client certificate") && strings.Contains(text, "required"))
}

func classifyTransportError(err error) (kind, reason, certainty string) {
	if err == nil {
		return "connection_closed", "connection closed", CertaintyObserved
	}
	if isTimeoutError(err) {
		return "timeout", err.Error(), CertaintyUncertain
	}
	if isPeerClosedError(err) {
		return "peer_closed", err.Error(), CertaintyUncertain
	}
	return "transport_error", err.Error(), CertaintyUncertain
}

func classifyMQTTError(err error) (reason, reasonCode, certainty string) {
	if err == nil {
		return "client_disconnected", "", CertaintyObserved
	}
	var code packets.Code
	if errors.As(err, &code) {
		return code.Reason, fmt.Sprintf("0x%02X", code.Code), CertaintyObserved
	}
	if isTimeoutError(err) {
		return err.Error(), "timeout", CertaintyUncertain
	}
	if isPeerClosedError(err) {
		return err.Error(), "peer_closed", CertaintyUncertain
	}
	return err.Error(), "", CertaintyObserved
}

// tlsAlertName 返回常见 TLS alert 的稳定名称。
// 未列出的 alert 返回 unknown_alert，并在事件中保留原始数值。
func tlsAlertName(code uint8) string {
	names := map[uint8]string{
		0:   "close_notify",
		10:  "unexpected_message",
		20:  "bad_record_mac",
		21:  "decryption_failed",
		22:  "record_overflow",
		30:  "decompression_failure",
		40:  "handshake_failure",
		41:  "no_certificate",
		42:  "bad_certificate",
		43:  "unsupported_certificate",
		44:  "certificate_revoked",
		45:  "certificate_expired",
		46:  "certificate_unknown",
		47:  "illegal_parameter",
		48:  "unknown_ca",
		49:  "access_denied",
		50:  "decode_error",
		51:  "decrypt_error",
		60:  "export_restriction",
		70:  "protocol_version",
		71:  "insufficient_security",
		80:  "internal_error",
		86:  "inappropriate_fallback",
		90:  "user_canceled",
		100: "no_renegotiation",
		109: "missing_extension",
		110: "unsupported_extension",
		111: "certificate_unobtainable",
		112: "unrecognized_name",
		113: "bad_certificate_status_response",
		114: "bad_certificate_hash_value",
		115: "unknown_psk_identity",
		116: "certificate_required",
		120: "no_application_protocol",
	}
	if name, ok := names[code]; ok {
		return name
	}
	return "unknown_alert"
}
