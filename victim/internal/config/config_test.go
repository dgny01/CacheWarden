package config

import (
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse(nil, func(string) string { return "" })
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if cfg.ListenAddress != ":8080" {
		t.Fatalf("ListenAddress = %q, want :8080", cfg.ListenAddress)
	}
	if cfg.WorkloadBytes != 32*1024*1024 {
		t.Fatalf("WorkloadBytes = %d, want %d", cfg.WorkloadBytes, 32*1024*1024)
	}
	if cfg.WorkloadIterations != 2 {
		t.Fatalf("WorkloadIterations = %d, want 2", cfg.WorkloadIterations)
	}
}

func TestParseEnvironmentAndFlagOverride(t *testing.T) {
	environment := map[string]string{
		"VICTIM_LISTEN_ADDRESS":      ":9000",
		"VICTIM_WORKLOAD_SIZE":       "2MiB",
		"VICTIM_WORKLOAD_ITERATIONS": "3",
		"VICTIM_SHUTDOWN_TIMEOUT":    "4s",
	}
	cfg, err := Parse(
		[]string{"--listen-address=:9100", "--workload-size=4MiB", "--workload-iterations=5"},
		func(key string) string { return environment[key] },
	)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if cfg.ListenAddress != ":9100" || cfg.WorkloadBytes != 4*1024*1024 || cfg.WorkloadIterations != 5 {
		t.Fatalf("flags did not override environment: %+v", cfg)
	}
	if cfg.ShutdownTimeout != 4*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want 4s", cfg.ShutdownTimeout)
	}
}

func TestParseRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "memory size", args: []string{"--workload-size=not-a-size"}},
		{name: "zero iterations", args: []string{"--workload-iterations=0"}},
		{name: "empty address", args: []string{"--listen-address="}},
		{name: "negative timeout", args: []string{"--read-timeout=-1s"}},
		{name: "unexpected argument", args: []string{"extra"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.args, func(string) string { return "" }); err == nil {
				t.Fatal("Parse succeeded, want an error")
			}
		})
	}
}

func TestParseByteSize(t *testing.T) {
	for input, want := range map[string]int{
		"1024": 1024,
		"1KiB": 1024,
		"2MiB": 2 * 1024 * 1024,
	} {
		got, err := ParseByteSize(input)
		if err != nil {
			t.Fatalf("ParseByteSize(%q) returned an error: %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseByteSize(%q) = %d, want %d", input, got, want)
		}
	}
}
