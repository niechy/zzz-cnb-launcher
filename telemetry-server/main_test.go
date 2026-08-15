package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestValidateEvent(t *testing.T) {
	valid := event{
		Schema:     1,
		InstallID:  "0123456789abcdef0123456789abcdef",
		AppVersion: "1.3.0",
		Name:       "app_start",
		Stage:      "startup",
		Result:     "success",
		ClientTime: time.Now().UTC().Format(time.RFC3339),
	}
	if err := validateEvent(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.ErrorCode = `C:\Users\Alice\secret.txt`
	if err := validateEvent(invalid); err == nil {
		t.Fatal("path-like telemetry value must be rejected")
	}
}

func TestWriterCreatesPrivateJSONL(t *testing.T) {
	dir := t.TempDir()
	w := &writer{dir: dir}
	item := event{
		Schema:     1,
		InstallID:  "0123456789abcdef0123456789abcdef",
		AppVersion: "1.3.0",
		Name:       "app_start",
		Result:     "success",
	}
	if err := w.append([]event{item}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "events-"+time.Now().UTC().Format("2006-01-02")+".jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected permissions: %o", info.Mode().Perm())
	}
}
