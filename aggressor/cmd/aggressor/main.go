package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cachewarden/cachewarden/aggressor/internal/config"
	"github.com/cachewarden/cachewarden/aggressor/internal/workload"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.Parse(os.Args[1:], os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	if err := config.CheckAvailableMemory(cfg.MemoryBytes, "/proc/meminfo"); err != nil {
		logger.Error("unsafe memory request", "error", err)
		os.Exit(2)
	}

	runner, err := workload.New(cfg.MemoryBytes, cfg.Workers, cfg.Mode)
	if err != nil {
		logger.Error("memory allocation failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("aggressor started",
		"mode", cfg.Mode,
		"memory_bytes", cfg.MemoryBytes,
		"workers", cfg.Workers,
		"report_interval", cfg.ReportInterval,
	)

	finished := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(finished)
	}()

	ticker := time.NewTicker(cfg.ReportInterval)
	defer ticker.Stop()
	var previous uint64
	var previousTime = time.Now()
	for {
		select {
		case now := <-ticker.C:
			total := runner.TotalBytes()
			elapsed := now.Sub(previousTime).Seconds()
			throughputMiB := float64(total-previous) / elapsed / 1024 / 1024
			logger.Info("aggressor throughput",
				"processed_bytes_total", total,
				"throughput_mib_per_second", throughputMiB,
			)
			previous = total
			previousTime = now
		case <-finished:
			logger.Info("aggressor stopped", "processed_bytes_total", runner.TotalBytes())
			return
		}
	}
}
