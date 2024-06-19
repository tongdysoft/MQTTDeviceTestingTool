package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// ServerKeyPasswordEnvVar 是私钥密码环境变量的固定名称。
// 环境变量不会出现在进程命令行中，但仍可能被同用户的其他进程读取。
const ServerKeyPasswordEnvVar = "MQTT_SERVER_KEY_PASSWORD"

// ErrPasswordSourceUnavailable 表示当前运行方式无法取得私钥密码。
var ErrPasswordSourceUnavailable = errors.New("no server key password source is available")

// KeyPasswordSpec 描述私钥密码的来源。它只保存来源和按需读取函数，
// 不参与日志格式化，避免把密码写入日志。
type KeyPasswordSpec struct {
	source     string
	configured bool
	provider   func() (string, error)
	warnings   []string
}

// NewStaticKeyPassword 返回一个固定密码来源，仅用于测试或嵌入式调用。
// 命令行不提供明文密码参数。
func NewStaticKeyPassword(value string) *KeyPasswordSpec {
	return &KeyPasswordSpec{
		source:     "explicit",
		configured: true,
		provider:   func() (string, error) { return value, nil },
	}
}

// Configured 表示用户是否显式提供了密码来源（密码文件或环境变量）。
// 自动交互提示和“无可用来源”不算显式提供。
func (s *KeyPasswordSpec) Configured() bool {
	return s != nil && s.configured
}

// Source 返回可安全写入日志的来源名称。
func (s *KeyPasswordSpec) Source() string {
	if s == nil {
		return "none"
	}
	return s.source
}

// Warnings 返回读取密码来源时产生的非敏感提示。
func (s *KeyPasswordSpec) Warnings() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.warnings...)
}

// Password 返回密码。只有确认私钥已加密时才会调用。
func (s *KeyPasswordSpec) Password() (string, error) {
	if s == nil || s.provider == nil {
		return "", ErrPasswordSourceUnavailable
	}
	return s.provider()
}

// PasswordSourceOptions 控制密码来源解析，便于测试替换终端与环境变量。
type PasswordSourceOptions struct {
	File            string
	EnvVar          string
	LookupEnv       func(string) (string, bool)
	StdinFd         int
	StdinIsTerminal func(fd int) bool
	ReadPassword    func(fd int) ([]byte, error)
	Prompt          io.Writer
	PromptLabel     string
}

// ResolveKeyPassword 按“密码文件 > 环境变量 > 交互式隐藏输入”的顺序确定密码来源。
// 密码文件与环境变量同时存在时直接报错，避免来源歧义。
// 非交互环境且没有显式来源时，只在私钥确实加密的情况下由加载器报错，
// 因此未加密私钥仍可在无终端环境下正常使用。
func ResolveKeyPassword(opts PasswordSourceOptions) (*KeyPasswordSpec, error) {
	if opts.EnvVar == "" {
		opts.EnvVar = ServerKeyPasswordEnvVar
	}
	lookupEnv := opts.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	isTerminal := opts.StdinIsTerminal
	if isTerminal == nil {
		isTerminal = term.IsTerminal
	}
	readPassword := opts.ReadPassword
	if readPassword == nil {
		readPassword = term.ReadPassword
	}
	if opts.Prompt == nil {
		opts.Prompt = os.Stderr
	}
	if opts.PromptLabel == "" {
		opts.PromptLabel = "Server key password: "
	}

	envValue, envSet := lookupEnv(opts.EnvVar)
	if opts.File != "" && envSet {
		return nil, fmt.Errorf("both -server-key-password-file and %s are set; choose one password source", opts.EnvVar)
	}

	if opts.File != "" {
		value, warnings, err := readPasswordFile(opts.File)
		if err != nil {
			return nil, err
		}
		return &KeyPasswordSpec{
			source:     "file",
			configured: true,
			provider:   func() (string, error) { return value, nil },
			warnings:   warnings,
		}, nil
	}

	if envSet {
		if envValue == "" {
			return nil, fmt.Errorf("environment variable %s is set but empty", opts.EnvVar)
		}
		return &KeyPasswordSpec{
			source:     "environment",
			configured: true,
			provider:   func() (string, error) { return envValue, nil },
		}, nil
	}

	if isTerminal(opts.StdinFd) {
		return &KeyPasswordSpec{
			source:     "interactive prompt",
			configured: false,
			provider: func() (string, error) {
				fmt.Fprint(opts.Prompt, opts.PromptLabel)
				raw, err := readPassword(opts.StdinFd)
				fmt.Fprintln(opts.Prompt)
				if err != nil {
					wipeBytes(raw)
					return "", fmt.Errorf("cannot read server key password from terminal: %w", err)
				}
				value := string(raw)
				wipeBytes(raw)
				if value == "" {
					return "", errors.New("empty server key password entered")
				}
				return value, nil
			},
		}, nil
	}

	return &KeyPasswordSpec{
		source:     "none",
		configured: false,
		provider: func() (string, error) {
			return "", fmt.Errorf("%w: encrypted server key requires -server-key-password-file or %s in non-interactive mode", ErrPasswordSourceUnavailable, opts.EnvVar)
		},
	}, nil
}

func readPasswordFile(path string) (string, []string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("cannot access server key password file %s: %w", path, err)
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("server key password path is a directory: %s", path)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("server key password path is not a regular file: %s", path)
	}
	if info.Size() > 64*1024 {
		return "", nil, fmt.Errorf("server key password file is unreasonably large: %s", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("cannot read server key password file %s: %w", path, err)
	}
	var warnings []string
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		warnings = append(warnings, fmt.Sprintf("server key password file %s is accessible by group or others (mode %04o); restrict it to 0600", path, info.Mode().Perm()))
	}
	value := strings.TrimSuffix(string(content), "\n")
	value = strings.TrimSuffix(value, "\r")
	if value == "" {
		return "", nil, fmt.Errorf("server key password file is empty: %s", path)
	}
	return value, warnings, nil
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
