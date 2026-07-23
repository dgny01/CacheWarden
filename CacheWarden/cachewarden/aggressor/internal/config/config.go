package config

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const MaximumMemoryBytes = 1024 * 1024 * 1024

// Config controls the bounded interference workload.
type Config struct {
	Mode           string
	MemoryBytes    int
	Workers        int
	ReportInterval time.Duration
}

// Parse reads environment values first and command-line flags second.
func Parse(args []string, getenv func(string) string) (Config, error) {
	defaultWorkers := runtime.NumCPU() / 2
	if defaultWorkers < 1 {
		defaultWorkers = 1
	}
	if defaultWorkers > 2 {
		defaultWorkers = 2
	}
	cfg := Config{
		Mode:           envOrDefault(getenv, "AGGRESSOR_MODE", "low"),
		Workers:        defaultWorkers,
		ReportInterval: 5 * time.Second,
	}
	memory := envOrDefault(getenv, "AGGRESSOR_MEMORY", "128MiB")
	if value := getenv("AGGRESSOR_WORKERS"); value != "" {
		workers, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse AGGRESSOR_WORKERS: %w", err)
		}
		cfg.Workers = workers
	}
	if value := getenv("AGGRESSOR_REPORT_INTERVAL"); value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse AGGRESSOR_REPORT_INTERVAL: %w", err)
		}
		cfg.ReportInterval = interval
	}

	flags := flag.NewFlagSet("aggressor", flag.ContinueOnError)
	flags.StringVar(&cfg.Mode, "mode", cfg.Mode, "workload level: low, medium, or high")
	flags.StringVar(&memory, "memory", memory, "total memory allocation, for example 128MiB")
	flags.IntVar(&cfg.Workers, "workers", cfg.Workers, "number of workload workers")
	flags.DurationVar(&cfg.ReportInterval, "report-interval", cfg.ReportInterval, "throughput reporting interval")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}

	memoryBytes, err := parseByteSize(memory)
	if err != nil {
		return Config{}, fmt.Errorf("invalid memory size: %w", err)
	}
	cfg.MemoryBytes = memoryBytes
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate enforces conservative resource boundaries.
func (c Config) Validate() error {
	switch c.Mode {
	case "low", "medium", "high":
	default:
		return fmt.Errorf("mode must be low, medium, or high; got %q", c.Mode)
	}
	switch {
	case c.MemoryBytes < 1024*1024:
		return errors.New("memory size must be at least 1MiB")
	case c.MemoryBytes > MaximumMemoryBytes:
		return errors.New("memory size must not exceed 1GiB")
	case c.Workers < 1:
		return errors.New("worker count must be at least 1")
	case c.Workers > 64:
		return errors.New("worker count must not exceed 64")
	case c.MemoryBytes/c.Workers < 1024:
		return errors.New("memory size must provide at least 1KiB per worker")
	case c.ReportInterval < 100*time.Millisecond:
		return errors.New("report interval must be at least 100ms")
	default:
		return nil
	}
}

// CheckAvailableMemory refuses allocations that approach all currently available RAM.
func CheckAvailableMemory(requested int, path string) error {
	available, err := availableMemory(path)
	if err != nil {
		return nil
	}
	if int64(requested) > available/2 {
		return fmt.Errorf("requested memory (%d bytes) exceeds 50%% of available system memory (%d bytes)", requested, available)
	}
	return nil
}

func availableMemory(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kib, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return kib * 1024, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("MemAvailable was not found")
}

func parseByteSize(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("size must not be empty")
	}
	multiplier := int64(1)
	number := trimmed
	for _, unit := range []struct {
		suffix string
		factor int64
	}{
		{"GiB", 1024 * 1024 * 1024},
		{"MiB", 1024 * 1024},
		{"KiB", 1024},
		{"B", 1},
	} {
		if strings.HasSuffix(trimmed, unit.suffix) {
			multiplier = unit.factor
			number = strings.TrimSpace(strings.TrimSuffix(trimmed, unit.suffix))
			break
		}
	}
	parsed, err := strconv.ParseInt(number, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("expected a positive integer with an optional binary suffix, got %q", value)
	}
	if parsed > int64(MaximumMemoryBytes)/multiplier {
		return 0, fmt.Errorf("size %q exceeds the supported maximum", value)
	}
	return int(parsed * multiplier), nil
}

func envOrDefault(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}
