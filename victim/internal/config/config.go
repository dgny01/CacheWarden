package config

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddress      = ":8080"
	defaultWorkloadSize       = "32MiB"
	defaultWorkloadIterations = 2
	minimumWorkloadBytes      = 1024
	maximumWorkloadBytes      = 512 * 1024 * 1024
)

// Config contains the victim's runtime settings.
type Config struct {
	ListenAddress      string
	WorkloadBytes      int
	WorkloadIterations int
	ReadHeaderTimeout  time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	ShutdownTimeout    time.Duration
}

// Parse reads environment values first and lets command-line flags override them.
func Parse(args []string, getenv func(string) string) (Config, error) {
	cfg := Config{
		ListenAddress:      envOrDefault(getenv, "VICTIM_LISTEN_ADDRESS", defaultListenAddress),
		WorkloadIterations: defaultWorkloadIterations,
		ReadHeaderTimeout:  5 * time.Second,
		ReadTimeout:        15 * time.Second,
		WriteTimeout:       30 * time.Second,
		IdleTimeout:        60 * time.Second,
		ShutdownTimeout:    10 * time.Second,
	}

	workloadSize := envOrDefault(getenv, "VICTIM_WORKLOAD_SIZE", defaultWorkloadSize)
	if value := getenv("VICTIM_WORKLOAD_ITERATIONS"); value != "" {
		iterations, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse VICTIM_WORKLOAD_ITERATIONS: %w", err)
		}
		cfg.WorkloadIterations = iterations
	}

	durationValues := []struct {
		name   string
		target *time.Duration
	}{
		{"VICTIM_READ_HEADER_TIMEOUT", &cfg.ReadHeaderTimeout},
		{"VICTIM_READ_TIMEOUT", &cfg.ReadTimeout},
		{"VICTIM_WRITE_TIMEOUT", &cfg.WriteTimeout},
		{"VICTIM_IDLE_TIMEOUT", &cfg.IdleTimeout},
		{"VICTIM_SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout},
	}
	for _, item := range durationValues {
		if value := getenv(item.name); value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return Config{}, fmt.Errorf("parse %s: %w", item.name, err)
			}
			*item.target = parsed
		}
	}

	flags := flag.NewFlagSet("victim", flag.ContinueOnError)
	flags.StringVar(&cfg.ListenAddress, "listen-address", cfg.ListenAddress, "HTTP listen address")
	flags.StringVar(&workloadSize, "workload-size", workloadSize, "in-memory workload size, for example 32MiB")
	flags.IntVar(&cfg.WorkloadIterations, "workload-iterations", cfg.WorkloadIterations, "number of full buffer scans per request")
	flags.DurationVar(&cfg.ReadHeaderTimeout, "read-header-timeout", cfg.ReadHeaderTimeout, "HTTP header read timeout")
	flags.DurationVar(&cfg.ReadTimeout, "read-timeout", cfg.ReadTimeout, "HTTP request read timeout")
	flags.DurationVar(&cfg.WriteTimeout, "write-timeout", cfg.WriteTimeout, "HTTP response write timeout")
	flags.DurationVar(&cfg.IdleTimeout, "idle-timeout", cfg.IdleTimeout, "HTTP keep-alive idle timeout")
	flags.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", cfg.ShutdownTimeout, "graceful shutdown timeout")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}

	bytes, err := ParseByteSize(workloadSize)
	if err != nil {
		return Config{}, fmt.Errorf("invalid workload size: %w", err)
	}
	cfg.WorkloadBytes = bytes
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate rejects settings that would make the experiment unsafe or unreliable.
func (c Config) Validate() error {
	switch {
	case strings.TrimSpace(c.ListenAddress) == "":
		return errors.New("listen address must not be empty")
	case c.WorkloadBytes < minimumWorkloadBytes:
		return fmt.Errorf("workload size must be at least %d bytes", minimumWorkloadBytes)
	case c.WorkloadBytes > maximumWorkloadBytes:
		return fmt.Errorf("workload size must not exceed %d bytes", maximumWorkloadBytes)
	case c.WorkloadIterations < 1 || c.WorkloadIterations > 100:
		return errors.New("workload iterations must be between 1 and 100")
	case c.ReadHeaderTimeout <= 0, c.ReadTimeout <= 0, c.WriteTimeout <= 0, c.IdleTimeout <= 0, c.ShutdownTimeout <= 0:
		return errors.New("HTTP and shutdown timeouts must be positive")
	default:
		return nil
	}
}

// ParseByteSize parses an integer with an optional B, KiB, MiB, or GiB suffix.
func ParseByteSize(value string) (int, error) {
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
	if parsed > int64(maximumWorkloadBytes)/multiplier {
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
