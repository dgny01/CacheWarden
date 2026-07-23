package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cachewarden/cachewarden/tools/loadgen/internal/loadgen"
)

func main() {
	var cfg loadgen.Config
	var outputPath string
	flags := flag.NewFlagSet("loadgen", flag.ExitOnError)
	flags.StringVar(&cfg.URL, "url", "http://127.0.0.1:8080/work", "target HTTP URL")
	flags.DurationVar(&cfg.Duration, "duration", 20*time.Second, "test duration")
	flags.IntVar(&cfg.Concurrency, "concurrency", 4, "number of concurrent request workers")
	flags.DurationVar(&cfg.Timeout, "timeout", 10*time.Second, "timeout for each request")
	flags.StringVar(&outputPath, "output", "", "CSV output path; the file must not already exist")
	flags.Parse(os.Args[1:])
	if flags.NArg() != 0 {
		fail("unexpected positional arguments")
	}
	if outputPath == "" {
		fail("--output is required")
	}

	results, err := loadgen.Run(context.Background(), cfg)
	if err != nil {
		fail(err.Error())
	}
	if err := writeCSV(outputPath, results); err != nil {
		fail(err.Error())
	}

	summary := loadgen.Summarize(results)
	fmt.Printf("total_requests=%d\n", summary.Total)
	fmt.Printf("successful_requests=%d\n", summary.Successful)
	fmt.Printf("failed_requests=%d\n", summary.Failed)
	fmt.Printf("mean_latency_ms=%.3f\n", milliseconds(summary.Mean))
	fmt.Printf("p50_latency_ms=%.3f\n", milliseconds(summary.P50))
	fmt.Printf("p95_latency_ms=%.3f\n", milliseconds(summary.P95))
	fmt.Printf("p99_latency_ms=%.3f\n", milliseconds(summary.P99))
}

func writeCSV(path string, results []loadgen.Result) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create CSV output %q: %w", path, err)
	}
	writer := csv.NewWriter(file)
	writeError := writer.Write([]string{"timestamp_utc", "status_code", "latency_ms", "error"})
	for _, result := range results {
		if writeError != nil {
			break
		}
		writeError = writer.Write([]string{
			result.Timestamp.Format(time.RFC3339Nano),
			strconv.Itoa(result.StatusCode),
			fmt.Sprintf("%.3f", milliseconds(result.Latency)),
			result.Error,
		})
	}
	writer.Flush()
	if writeError == nil {
		writeError = writer.Error()
	}
	if closeError := file.Close(); writeError == nil {
		writeError = closeError
	}
	if writeError != nil {
		return fmt.Errorf("write CSV output %q: %w", path, writeError)
	}
	return nil
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "loadgen:", message)
	os.Exit(1)
}
