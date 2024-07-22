package main

import (
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readCSVRecords(t *testing.T, path string) [][]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open CSV %s: %v", path, err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV %s: %v", path, err)
	}
	return records
}

func TestEventLogWritesValidCSVAndTruncates(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "data.csv")
	statusPath := filepath.Join(dir, "status.csv")
	if err := os.WriteFile(dataPath, []byte("old-content\n"), 0o600); err != nil {
		t.Fatalf("seed old data log: %v", err)
	}

	log, err := OpenEventLog(EventLogOptions{
		Terminal:    io.Discard,
		ErrorWriter: io.Discard,
		DataPath:    dataPath,
		StatusPath:  statusPath,
	})
	if err != nil {
		t.Fatalf("OpenEventLog: %v", err)
	}
	log.Data("client-1", "topic,with,commas", "payload with \"quotes\"\nand newline")
	log.Status("client-1", "connected", "ok")
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data := readCSVRecords(t, dataPath)
	if len(data) != 1 || len(data[0]) != 4 {
		t.Fatalf("data records = %#v, want one record with timestamp and 3 fields", data)
	}
	if data[0][1] != "client-1" || data[0][2] != "topic,with,commas" ||
		data[0][3] != "payload with \"quotes\"\nand newline" {
		t.Fatalf("data record was not preserved: %#v", data[0])
	}
	if strings.Contains(strings.Join(data[0], ","), "old-content") {
		t.Fatal("previous log content was not truncated")
	}

	status := readCSVRecords(t, statusPath)
	if len(status) != 1 || len(status[0]) != 4 || status[0][2] != "connected" {
		t.Fatalf("status records = %#v", status)
	}
}

func TestEventLogFileTimestampUsesUnixNanos(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	log, err := OpenEventLog(EventLogOptions{
		Terminal:      io.Discard,
		ErrorWriter:   io.Discard,
		DataPath:      path,
		FileTimestamp: true,
	})
	if err != nil {
		t.Fatalf("OpenEventLog: %v", err)
	}
	log.Data("client", "topic", "payload")
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	records := readCSVRecords(t, path)
	if len(records) != 1 || len(records[0]) == 0 {
		t.Fatalf("records = %#v", records)
	}
	for _, r := range records[0][0] {
		if r < '0' || r > '9' {
			t.Fatalf("timestamp %q is not a Unix timestamp", records[0][0])
		}
	}
}
