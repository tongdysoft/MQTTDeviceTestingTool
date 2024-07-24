package main

// 本文件验证输入/输出路径冲突在任何截断发生之前被拒绝，
// 并使用真实启动路径确认合法配置仍能分别写入日志与结果文件。

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func assertContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("content of %s changed: got %q, want %q", path, content, want)
	}
}

func writeSentinel(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func requireSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("platform does not support symbolic links here (%v); link-based conflict test skipped", err)
	}
}

func TestParseConfigRejectsInputAsLogFile(t *testing.T) {
	dir := t.TempDir()
	users := filepath.Join(dir, "users.yaml")
	const secret = "auth:\n  - allow: true\n"
	writeSentinel(t, users, secret)

	_, err := ParseConfig([]string{"-users", users, "-log", users})
	if err == nil {
		t.Fatal("ParseConfig accepted -users and -log pointing to the same file")
	}
	for _, want := range []string{"file path conflict", "-users", "user authentication file", "-log", "text log"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err, want)
		}
	}
	assertContent(t, users, secret)
}

func TestParseConfigRejectsCertificateAsOutput(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.pem")
	const pem = "-----BEGIN CERTIFICATE-----\nsentinel\n-----END CERTIFICATE-----\n"
	writeSentinel(t, certPath, pem)

	_, err := ParseConfig([]string{
		"-mode", "tls",
		"-server-cert", certPath,
		"-server-key", certPath,
		"-log-data", certPath,
	})
	if err == nil {
		t.Fatal("ParseConfig accepted a log file that is also an input certificate")
	}
	if !strings.Contains(err.Error(), "file path conflict") {
		t.Fatalf("error = %q, want a path conflict", err)
	}
	assertContent(t, certPath, pem)
}

func TestParseConfigRejectsLogPathAliases(t *testing.T) {
	dir := t.TempDir()
	absolute := filepath.Join(dir, "out.log")
	dotted := dir + string(os.PathSeparator) + "." + string(os.PathSeparator) + "out.log"

	t.Run("dot segment", func(t *testing.T) {
		_, err := ParseConfig([]string{"-log", absolute, "-log-status", dotted})
		if err == nil || !strings.Contains(err.Error(), "file path conflict") {
			t.Fatalf("ParseConfig() error = %v, want conflict for . path segment", err)
		}
	})

	t.Run("parent reference", func(t *testing.T) {
		sub := filepath.Join(dir, "sub")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatalf("mkdir sub: %v", err)
		}
		viaParent := filepath.Join(sub, "..", "out.log")
		_, err := ParseConfig([]string{"-log", absolute, "-log-status", viaParent})
		if err == nil || !strings.Contains(err.Error(), "file path conflict") {
			t.Fatalf("ParseConfig() error = %v, want conflict for .. path segment", err)
		}
	})

	t.Run("dotdot after symlink", func(t *testing.T) {
		base := t.TempDir()
		lexical := filepath.Join(base, "linkdir")
		target := filepath.Join(base, "target", "nested")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		if err := os.MkdirAll(lexical, 0o755); err != nil {
			t.Fatalf("mkdir symlink parent: %v", err)
		}
		link := filepath.Join(lexical, "link")
		requireSymlink(t, target, link)
		// 内核语义下 link/.. 是 target 的父目录，而不是 linkdir。
		separator := string(os.PathSeparator)
		viaLink := link + separator + ".." + separator + "out.log"
		direct := filepath.Join(base, "target", "out.log")
		_, err := ParseConfig([]string{"-log", viaLink, "-log-data", direct})
		if err == nil || !strings.Contains(err.Error(), "file path conflict") {
			t.Fatalf("ParseConfig() error = %v, want conflict across symlink/..", err)
		}
	})

	t.Run("relative and absolute", func(t *testing.T) {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd: %v", err)
		}
		relativeDir, err := filepath.Rel(cwd, dir)
		if err != nil || filepath.IsAbs(relativeDir) {
			t.Skipf("temp dir is not reachable by a relative path from %s (%v)", cwd, err)
		}
		relative := filepath.Join(relativeDir, "out.log")
		if relative == absolute {
			t.Skip("relative path is identical to the absolute path on this platform")
		}
		_, err = ParseConfig([]string{"-log", relative, "-log-data", absolute})
		if err == nil || !strings.Contains(err.Error(), "file path conflict") {
			t.Fatalf("ParseConfig() error = %v, want conflict for relative path %q", err, relative)
		}
	})
}

func TestParseConfigRejectsResultsPathConflicts(t *testing.T) {
	dir := t.TempDir()
	results := filepath.Join(dir, "results")
	if err := os.Mkdir(results, 0o755); err != nil {
		t.Fatalf("mkdir results: %v", err)
	}
	eventsPath := filepath.Join(results, eventsFileName)
	summaryPath := filepath.Join(results, summaryFileName)
	const events = "{\"existing\":true}\n"
	const summary = "{\"existing\":true}\n"
	writeSentinel(t, eventsPath, events)
	writeSentinel(t, summaryPath, summary)

	tests := []struct {
		name string
		flag string
		path string
		want string
	}{
		{"log is events file", "-log", eventsPath, events},
		{"status is events file", "-log-status", filepath.Join(results, ".", eventsFileName), events},
		{"data is summary file", "-log-data", summaryPath, summary},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseConfig([]string{"-results", results, test.flag, test.path})
			if err == nil || !strings.Contains(err.Error(), "file path conflict") {
				t.Fatalf("ParseConfig() error = %v, want conflict with results file", err)
			}
			assertContent(t, eventsPath, events)
			assertContent(t, summaryPath, summary)
		})
	}

	t.Run("log is results directory", func(t *testing.T) {
		_, err := ParseConfig([]string{"-results", results, "-log", results})
		if err == nil {
			t.Fatal("ParseConfig accepted the results directory as a log file")
		}
		for _, want := range []string{"-results", "-log", "directory"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want substring %q", err, want)
			}
		}
	})
}

func TestParseConfigRejectsSymlinkedInput(t *testing.T) {
	dir := t.TempDir()
	users := filepath.Join(dir, "users.yaml")
	const secret = "auth:\n  - allow: true\n"
	writeSentinel(t, users, secret)
	logPath := filepath.Join(dir, "run.log")
	requireSymlink(t, users, logPath)

	_, err := ParseConfig([]string{"-users", users, "-log", logPath})
	if err == nil || !strings.Contains(err.Error(), "file path conflict") {
		t.Fatalf("ParseConfig() error = %v, want symbolic link conflict", err)
	}
	assertContent(t, users, secret)
}

func TestParseConfigRejectsParentDirectorySymlinkAlias(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("mkdir real dir: %v", err)
	}
	linkDir := filepath.Join(dir, "link")
	requireSymlink(t, realDir, linkDir)

	_, err := ParseConfig([]string{
		"-log", filepath.Join(linkDir, "out.log"),
		"-log-status", filepath.Join(realDir, "out.log"),
	})
	if err == nil || !strings.Contains(err.Error(), "file path conflict") {
		t.Fatalf("ParseConfig() error = %v, want conflict through parent symlink", err)
	}

	users := filepath.Join(realDir, "users.yaml")
	const secret = "auth:\n  - allow: true\n"
	writeSentinel(t, users, secret)
	_, err = ParseConfig([]string{"-users", users, "-log", filepath.Join(linkDir, "users.yaml")})
	if err == nil || !strings.Contains(err.Error(), "file path conflict") {
		t.Fatalf("ParseConfig() error = %v, want conflict for input reached through parent symlink", err)
	}
	assertContent(t, users, secret)
}

func TestParseConfigRejectsHardLinkAlias(t *testing.T) {
	dir := t.TempDir()
	users := filepath.Join(dir, "users.yaml")
	alias := filepath.Join(dir, "looks-like-log.log")
	const secret = "auth:\n  - allow: true\n"
	writeSentinel(t, users, secret)
	if err := os.Link(users, alias); err != nil {
		t.Skipf("platform does not support hard links here (%v); hard-link test skipped", err)
	}

	_, err := ParseConfig([]string{"-users", users, "-log", alias})
	if err == nil || !strings.Contains(err.Error(), "file path conflict") {
		t.Fatalf("ParseConfig() error = %v, want hard link conflict", err)
	}
	assertContent(t, users, secret)
	assertContent(t, alias, secret)
}

func TestParseConfigRejectsBrokenLogSymlink(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "dangling.log")
	requireSymlink(t, filepath.Join(dir, "missing-target.log"), logPath)

	_, err := ParseConfig([]string{"-log", logPath})
	if err == nil {
		t.Fatal("ParseConfig accepted a dangling symbolic link as a log path")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error = %q, want a clear symbolic link error", err)
	}
}

func TestParseConfigRejectsDirectoryAsLog(t *testing.T) {
	dir := t.TempDir()
	_, err := ParseConfig([]string{"-log", dir})
	if err == nil {
		t.Fatal("ParseConfig accepted a directory as a log file")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("error = %q, want clear directory error", err)
	}
}

func TestCaseOnlyPathsFollowFilesystemSemantics(t *testing.T) {
	dir := t.TempDir()
	lower := filepath.Join(dir, "case.log")
	upper := filepath.Join(dir, "CASE.LOG")
	_, err := ParseConfig([]string{"-log", lower, "-log-status", upper})
	if dirIsCaseInsensitive(dir) {
		if err == nil || !strings.Contains(err.Error(), "file path conflict") {
			t.Fatalf("error = %v, want conflict on case-insensitive filesystem", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("ParseConfig rejected case-distinct paths on a case-sensitive filesystem: %v", err)
	}

	log, err := OpenEventLog(EventLogOptions{
		Terminal:   io.Discard,
		TextPath:   lower,
		StatusPath: upper,
	})
	if err != nil {
		t.Fatalf("OpenEventLog: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	lowerInfo, err := os.Stat(lower)
	if err != nil {
		t.Fatalf("stat %s: %v", lower, err)
	}
	upperInfo, err := os.Stat(upper)
	if err != nil {
		t.Fatalf("stat %s: %v", upper, err)
	}
	if os.SameFile(lowerInfo, upperInfo) {
		t.Fatal("case-distinct paths were created as the same file")
	}
}

func TestParseConfigRejectsUnresolvableOutputParents(t *testing.T) {
	dir := t.TempDir()
	t.Run("dotdot below missing directory", func(t *testing.T) {
		path := filepath.Join(dir, "missing") + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "out.log"
		_, err := ParseConfig([]string{"-log", path})
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("ParseConfig() error = %v, want missing component error", err)
		}
	})
	t.Run("parent is a regular file", func(t *testing.T) {
		file := filepath.Join(dir, "plain.txt")
		writeSentinel(t, file, "sentinel\n")
		_, err := ParseConfig([]string{"-log", filepath.Join(file, "out.log")})
		if err == nil || !strings.Contains(err.Error(), "cannot determine the file identity") {
			t.Fatalf("ParseConfig() error = %v, want ENOTDIR identity error", err)
		}
		assertContent(t, file, "sentinel\n")
	})
}

func TestCheckFileRoleConflictsCoversEveryInputRole(t *testing.T) {
	dir := t.TempDir()
	material := filepath.Join(dir, "material.pem")
	writeSentinel(t, material, "sentinel-input\n")
	inputs := []FileRole{
		{Flag: "-server-cert", Role: "server certificate"},
		{Flag: "-server-key", Role: "server private key"},
		{Flag: "-server-key-password-file", Role: "server key password file"},
		{Flag: "-client-ca", Role: "client CA bundle"},
		{Flag: "-users", Role: "user authentication file"},
	}
	for _, input := range inputs {
		t.Run(input.Flag, func(t *testing.T) {
			input.Path = material
			input.Kind = PathRoleInput
			err := CheckFileRoleConflicts([]FileRole{
				input,
				{Flag: "-log", Role: "text log", Path: material, Kind: PathRoleOutput},
			})
			if err == nil || !strings.Contains(err.Error(), input.Flag) {
				t.Fatalf("error = %v, want conflict naming %s", err, input.Flag)
			}
			assertContent(t, material, "sentinel-input\n")
		})
	}
}

func TestInputPathsMayBeReused(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle.pem")
	writeSentinel(t, bundle, "shared-input\n")
	err := CheckFileRoleConflicts([]FileRole{
		{Flag: "-server-cert", Role: "server certificate", Path: bundle, Kind: PathRoleInput},
		{Flag: "-client-ca", Role: "client CA bundle", Path: bundle, Kind: PathRoleInput},
		{Flag: "-log", Role: "text log", Path: filepath.Join(dir, "run.log"), Kind: PathRoleOutput},
	})
	if err != nil {
		t.Fatalf("read-only reuse of the same input was rejected: %v", err)
	}
}

func TestOpenEventLogRejectsProtectedInputBeforeTruncate(t *testing.T) {
	dir := t.TempDir()
	users := filepath.Join(dir, "users.yaml")
	const secret = "auth:\n  - allow: true\n"
	writeSentinel(t, users, secret)
	protected := []FileRole{{Flag: "-users", Role: "user authentication file", Path: users, Kind: PathRoleInput}}

	t.Run("same path", func(t *testing.T) {
		_, err := OpenEventLog(EventLogOptions{
			Terminal:  io.Discard,
			TextPath:  users,
			Protected: protected,
		})
		if err == nil || !strings.Contains(err.Error(), "file path conflict") {
			t.Fatalf("OpenEventLog() error = %v, want conflict", err)
		}
		assertContent(t, users, secret)
	})

	t.Run("symlink alias", func(t *testing.T) {
		link := filepath.Join(dir, "log-link.log")
		requireSymlink(t, users, link)
		_, err := OpenEventLog(EventLogOptions{
			Terminal:  io.Discard,
			DataPath:  link,
			Protected: protected,
		})
		if err == nil || !strings.Contains(err.Error(), "file path conflict") {
			t.Fatalf("OpenEventLog() error = %v, want symlink conflict", err)
		}
		assertContent(t, users, secret)
	})

	t.Run("hard link alias", func(t *testing.T) {
		link := filepath.Join(dir, "log-hardlink.log")
		if err := os.Link(users, link); err != nil {
			t.Skipf("platform does not support hard links here (%v); hard-link test skipped", err)
		}
		_, err := OpenEventLog(EventLogOptions{
			Terminal:   io.Discard,
			StatusPath: link,
			Protected:  protected,
		})
		if err == nil || !strings.Contains(err.Error(), "file path conflict") {
			t.Fatalf("OpenEventLog() error = %v, want hard link conflict", err)
		}
		assertContent(t, users, secret)
	})
}

func TestOpenEventLogRejectsAliasedOutputsBeforeTruncate(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "data.csv")
	statusPath := filepath.Join(dir, "status.csv")
	const dataContent = "existing,data\n"
	writeSentinel(t, dataPath, dataContent)
	if err := os.Link(dataPath, statusPath); err != nil {
		t.Skipf("platform does not support hard links here (%v); hard-link output test skipped", err)
	}

	_, err := OpenEventLog(EventLogOptions{
		Terminal:   io.Discard,
		DataPath:   dataPath,
		StatusPath: statusPath,
	})
	if err == nil || !strings.Contains(err.Error(), "file path conflict") {
		t.Fatalf("OpenEventLog() error = %v, want output alias conflict", err)
	}
	assertContent(t, dataPath, dataContent)
	assertContent(t, statusPath, dataContent)
}

func TestOpenEventLogLeavesEarlierFilesIntactOnLaterFailure(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "data.csv")
	const dataContent = "existing,data\n"
	writeSentinel(t, dataPath, dataContent)

	_, err := OpenEventLog(EventLogOptions{
		Terminal: io.Discard,
		DataPath: dataPath,
		TextPath: filepath.Join(dir, "missing", "run.log"),
	})
	if err == nil {
		t.Fatal("OpenEventLog accepted a path in a missing directory")
	}
	assertContent(t, dataPath, dataContent)
}

func TestNewDiagnosticsRejectsInputResultConflict(t *testing.T) {
	dir := t.TempDir()
	results := filepath.Join(dir, "results")
	if err := os.Mkdir(results, 0o755); err != nil {
		t.Fatalf("mkdir results: %v", err)
	}
	eventsPath := filepath.Join(results, eventsFileName)
	const secret = "auth:\n  - allow: true\n"
	writeSentinel(t, eventsPath, secret)

	cfg := &Config{Mode: ModePlain, UserFile: eventsPath}
	_, err := NewDiagnostics(DiagnosticsOptions{
		Config:      cfg,
		ResultsDir:  results,
		Terminal:    io.Discard,
		ErrorWriter: io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "file path conflict") {
		t.Fatalf("NewDiagnostics() error = %v, want conflict", err)
	}
	assertContent(t, eventsPath, secret)
	if _, err := os.Stat(filepath.Join(results, summaryFileName)); !os.IsNotExist(err) {
		t.Fatalf("summary file was created despite the conflict (stat error = %v)", err)
	}
}

func TestNewDiagnosticsRejectsResultsConflictBeforeCreatingDirectory(t *testing.T) {
	dir := t.TempDir()
	results := filepath.Join(dir, "results")
	cfg := &Config{Mode: ModePlain, LogFile: filepath.Join(results, eventsFileName), ResultsDir: results}
	_, err := NewDiagnostics(DiagnosticsOptions{
		Config:      cfg,
		ResultsDir:  results,
		Terminal:    io.Discard,
		ErrorWriter: io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "file path conflict") {
		t.Fatalf("NewDiagnostics() error = %v, want conflict", err)
	}
	if _, err := os.Stat(results); !os.IsNotExist(err) {
		t.Fatalf("results directory was created before the conflict was reported (stat error = %v)", err)
	}
}

func TestDistinctPathsRecordAndExport(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.yaml")
	writeSentinel(t, usersPath, integrationAuthUsers)
	textPath := filepath.Join(dir, "run.log")
	statusPath := filepath.Join(dir, "status.csv")
	dataPath := filepath.Join(dir, "data.csv")
	resultsDir := filepath.Join(dir, "results")

	cfg, err := ParseConfig([]string{
		"-listen", "127.0.0.1:0",
		"-users", usersPath,
		"-log", textPath,
		"-log-status", statusPath,
		"-log-data", dataPath,
		"-results", resultsDir,
		"-run-id", "run-distinct-paths",
		"-shutdown-timeout", "2s",
	})
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	eventLog, err := OpenEventLog(EventLogOptions{
		Terminal:    io.Discard,
		ErrorWriter: io.Discard,
		TextPath:    textPath,
		StatusPath:  statusPath,
		DataPath:    dataPath,
		Protected:   cfg.InputFileRoles(),
	})
	if err != nil {
		t.Fatalf("OpenEventLog: %v", err)
	}
	server, err := StartServer(StartOptions{
		Config:    cfg,
		EventLog:  eventLog,
		LogWriter: io.Discard,
	})
	if err != nil {
		_ = eventLog.Close()
		t.Fatalf("StartServer: %v", err)
	}

	eventLog.Print("I", "distinct-path-marker")
	eventLog.Status("client-1", "connected", "ok")
	eventLog.Data("client-1", "devices/1/state", "payload")
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	if err := eventLog.Close(); err != nil {
		t.Fatalf("close event log: %v", err)
	}

	text, err := os.ReadFile(textPath)
	if err != nil {
		t.Fatalf("read text log: %v", err)
	}
	if !strings.Contains(string(text), "distinct-path-marker") {
		t.Fatalf("text log does not contain the marker: %q", text)
	}
	status := readCSVRecords(t, statusPath)
	if len(status) != 1 || status[0][2] != "connected" {
		t.Fatalf("status CSV = %#v, want one connection record", status)
	}
	data := readCSVRecords(t, dataPath)
	if len(data) != 1 || data[0][3] != "payload" {
		t.Fatalf("data CSV = %#v, want one data record", data)
	}

	eventsContent, err := os.ReadFile(filepath.Join(resultsDir, eventsFileName))
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}
	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(string(eventsContent)), "\n") {
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("events file is not valid JSONL: %v (%q)", err, line)
		}
		lines++
	}
	if lines < 2 {
		t.Fatalf("events file has %d events, want at least the run start and end events", lines)
	}
	summary := readRunSummary(t, filepath.Join(resultsDir, summaryFileName))
	if summary.RunID != "run-distinct-paths" {
		t.Fatalf("summary run id = %q", summary.RunID)
	}
	if summary.StartedAt == "" || summary.EndedAt == "" {
		t.Fatalf("summary timestamps missing: %#v", summary)
	}
	if time.Since(mustParseSummaryTime(t, summary.EndedAt)) > time.Minute {
		t.Fatalf("summary end time %q is not recent", summary.EndedAt)
	}
	assertContent(t, usersPath, integrationAuthUsers)
}

func mustParseSummaryTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("summary time %q is not RFC3339Nano: %v", value, err)
	}
	return parsed
}
