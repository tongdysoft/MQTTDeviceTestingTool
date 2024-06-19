package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func passwordOptionsWithFile(t *testing.T, path string) PasswordSourceOptions {
	t.Helper()
	return PasswordSourceOptions{
		File:      path,
		EnvVar:    "MQTT_TEST_KEY_PASSWORD",
		LookupEnv: func(string) (string, bool) { return "", false },
		StdinFd:   42,
		StdinIsTerminal: func(int) bool {
			return false
		},
	}
}

func TestResolveKeyPasswordFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.key.password")
	if err := os.WriteFile(path, []byte("file-secret\r\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	spec, err := ResolveKeyPassword(passwordOptionsWithFile(t, path))
	if err != nil {
		t.Fatalf("ResolveKeyPassword() error = %v", err)
	}
	if !spec.Configured() || spec.Source() != "file" {
		t.Errorf("spec = configured %v, source %q; want configured file", spec.Configured(), spec.Source())
	}
	value, err := spec.Password()
	if err != nil {
		t.Fatalf("Password() error = %v", err)
	}
	if value != "file-secret" {
		t.Errorf("Password() = %q, want trailing newline stripped", value)
	}
	if strings.Contains(fmt.Sprintf("%v", spec), "file-secret") {
		t.Error("KeyPasswordSpec formatting leaks the password")
	}
}

func TestResolveKeyPasswordFileWarnings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on Windows")
	}
	path := filepath.Join(t.TempDir(), "server.key.password")
	if err := os.WriteFile(path, []byte("secret\n"), 0o644); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	spec, err := ResolveKeyPassword(passwordOptionsWithFile(t, path))
	if err != nil {
		t.Fatalf("ResolveKeyPassword() error = %v", err)
	}
	if !containsSubstring(spec.Warnings(), "accessible by group or others") {
		t.Errorf("warnings = %v, want permission warning", spec.Warnings())
	}
}

func TestResolveKeyPasswordEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.password")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	_, err := ResolveKeyPassword(passwordOptionsWithFile(t, path))
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("ResolveKeyPassword() error = %v, want empty-file error", err)
	}
}

func TestResolveKeyPasswordFromEnvironment(t *testing.T) {
	spec, err := ResolveKeyPassword(PasswordSourceOptions{
		EnvVar:          "MQTT_TEST_KEY_PASSWORD",
		LookupEnv:       func(string) (string, bool) { return "env-secret", true },
		StdinFd:         42,
		StdinIsTerminal: func(int) bool { return false },
	})
	if err != nil {
		t.Fatalf("ResolveKeyPassword() error = %v", err)
	}
	if !spec.Configured() || spec.Source() != "environment" {
		t.Errorf("spec = configured %v, source %q; want environment", spec.Configured(), spec.Source())
	}
	value, err := spec.Password()
	if err != nil || value != "env-secret" {
		t.Fatalf("Password() = (%q, %v), want env-secret", value, err)
	}
}

func TestResolveKeyPasswordConflicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.key.password")
	if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	_, err := ResolveKeyPassword(PasswordSourceOptions{
		File:            path,
		EnvVar:          "MQTT_TEST_KEY_PASSWORD",
		LookupEnv:       func(string) (string, bool) { return "env-secret", true },
		StdinIsTerminal: func(int) bool { return false },
	})
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("ResolveKeyPassword() error = %v, want conflict error", err)
	}
}

func TestResolveKeyPasswordEmptyEnvironment(t *testing.T) {
	_, err := ResolveKeyPassword(PasswordSourceOptions{
		EnvVar:          "MQTT_TEST_KEY_PASSWORD",
		LookupEnv:       func(string) (string, bool) { return "", true },
		StdinIsTerminal: func(int) bool { return false },
	})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("ResolveKeyPassword() error = %v, want empty-environment error", err)
	}
}

func TestResolveKeyPasswordNonInteractive(t *testing.T) {
	spec, err := ResolveKeyPassword(PasswordSourceOptions{
		EnvVar:          "MQTT_TEST_KEY_PASSWORD",
		LookupEnv:       func(string) (string, bool) { return "", false },
		StdinFd:         42,
		StdinIsTerminal: func(int) bool { return false },
	})
	if err != nil {
		t.Fatalf("ResolveKeyPassword() error = %v", err)
	}
	if spec.Configured() {
		t.Error("non-interactive source should not be marked configured")
	}
	_, err = spec.Password()
	if !errors.Is(err, ErrPasswordSourceUnavailable) {
		t.Fatalf("Password() error = %v, want ErrPasswordSourceUnavailable", err)
	}
	if !strings.Contains(err.Error(), "non-interactive") || !strings.Contains(err.Error(), "MQTT_TEST_KEY_PASSWORD") {
		t.Errorf("Password() error = %v, want non-interactive hint with environment variable name", err)
	}
}

func TestResolveKeyPasswordInteractivePrompt(t *testing.T) {
	var prompt bytes.Buffer
	spec, err := ResolveKeyPassword(PasswordSourceOptions{
		EnvVar:          "MQTT_TEST_KEY_PASSWORD",
		LookupEnv:       func(string) (string, bool) { return "", false },
		StdinFd:         42,
		StdinIsTerminal: func(fd int) bool { return fd == 42 },
		ReadPassword: func(fd int) ([]byte, error) {
			if fd != 42 {
				t.Errorf("ReadPassword fd = %d, want 42", fd)
			}
			return []byte("prompt-secret"), nil
		},
		Prompt:      &prompt,
		PromptLabel: "Server key password: ",
	})
	if err != nil {
		t.Fatalf("ResolveKeyPassword() error = %v", err)
	}
	if spec.Configured() {
		t.Error("interactive prompt should not be marked configured")
	}
	value, err := spec.Password()
	if err != nil || value != "prompt-secret" {
		t.Fatalf("Password() = (%q, %v), want prompt-secret", value, err)
	}
	if strings.Contains(prompt.String(), "prompt-secret") {
		t.Error("hidden password input echoed the password")
	}
	if !strings.Contains(prompt.String(), "Server key password") {
		t.Errorf("prompt = %q, want label", prompt.String())
	}
}

func TestResolveKeyPasswordInteractiveEmpty(t *testing.T) {
	spec, err := ResolveKeyPassword(PasswordSourceOptions{
		EnvVar:          "MQTT_TEST_KEY_PASSWORD",
		LookupEnv:       func(string) (string, bool) { return "", false },
		StdinFd:         42,
		StdinIsTerminal: func(int) bool { return true },
		ReadPassword:    func(int) ([]byte, error) { return nil, nil },
		Prompt:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("ResolveKeyPassword() error = %v", err)
	}
	if _, err := spec.Password(); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("Password() error = %v, want empty-password error", err)
	}
}

func TestNewStaticKeyPassword(t *testing.T) {
	spec := NewStaticKeyPassword("static-secret")
	if !spec.Configured() {
		t.Error("static password should be configured")
	}
	if value, err := spec.Password(); err != nil || value != "static-secret" {
		t.Fatalf("Password() = (%q, %v)", value, err)
	}
}
