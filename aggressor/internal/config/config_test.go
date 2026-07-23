package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse(nil, func(string) string { return "" })
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if cfg.Mode != "low" || cfg.MemoryBytes != 128*1024*1024 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Workers < 1 || cfg.Workers > 2 {
		t.Fatalf("Workers = %d, want between 1 and 2", cfg.Workers)
	}
}

func TestParseEnvironmentAndFlagOverride(t *testing.T) {
	environment := map[string]string{
		"AGGRESSOR_MODE":            "medium",
		"AGGRESSOR_MEMORY":          "64MiB",
		"AGGRESSOR_WORKERS":         "2",
		"AGGRESSOR_REPORT_INTERVAL": "2s",
	}
	cfg, err := Parse(
		[]string{"--mode=high", "--memory=32MiB", "--workers=3", "--report-interval=1s"},
		func(key string) string { return environment[key] },
	)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if cfg.Mode != "high" || cfg.MemoryBytes != 32*1024*1024 || cfg.Workers != 3 || cfg.ReportInterval != time.Second {
		t.Fatalf("flags did not override environment: %+v", cfg)
	}
}

func TestParseRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "mode", args: []string{"--mode=maximum"}},
		{name: "memory text", args: []string{"--memory=large"}},
		{name: "memory too small", args: []string{"--memory=1KiB"}},
		{name: "memory too large", args: []string{"--memory=2GiB"}},
		{name: "zero workers", args: []string{"--workers=0"}},
		{name: "too many workers", args: []string{"--workers=65"}},
		{name: "short report interval", args: []string{"--report-interval=10ms"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.args, func(string) string { return "" }); err == nil {
				t.Fatal("Parse succeeded, want an error")
			}
		})
	}
}

func TestCheckAvailableMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte("MemTotal: 1048576 kB\nMemAvailable: 1024 kB\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := CheckAvailableMemory(600*1024, path); err == nil {
		t.Fatal("CheckAvailableMemory succeeded, want an error")
	}
	if err := CheckAvailableMemory(400*1024, path); err != nil {
		t.Fatalf("CheckAvailableMemory returned an error: %v", err)
	}
}
