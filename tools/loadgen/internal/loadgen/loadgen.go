package loadgen

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Config controls one bounded load generation run.
type Config struct {
	URL         string
	Duration    time.Duration
	Concurrency int
	Timeout     time.Duration
}

// Result describes one HTTP request attempt.
type Result struct {
	Timestamp  time.Time
	StatusCode int
	Latency    time.Duration
	Error      string
}

// Summary contains aggregate latency and status measurements.
type Summary struct {
	Total      int
	Successful int
	Failed     int
	Mean       time.Duration
	P50        time.Duration
	P95        time.Duration
	P99        time.Duration
}

// Run sends requests until the configured duration expires.
func Run(parent context.Context, cfg Config) ([]Result, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("URL must not be empty")
	}
	if cfg.Duration <= 0 {
		return nil, fmt.Errorf("duration must be positive")
	}
	if cfg.Concurrency < 1 || cfg.Concurrency > 256 {
		return nil, fmt.Errorf("concurrency must be between 1 and 256")
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("request timeout must be positive")
	}

	transport := &http.Transport{
		MaxIdleConns:        cfg.Concurrency,
		MaxIdleConnsPerHost: cfg.Concurrency,
		IdleConnTimeout:     30 * time.Second,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: cfg.Timeout}
	return runWithClient(parent, cfg, client)
}

func runWithClient(parent context.Context, cfg Config, client *http.Client) ([]Result, error) {
	ctx, cancel := context.WithTimeout(parent, cfg.Duration)
	defer cancel()

	results := make(chan Result, cfg.Concurrency*2)
	var workers sync.WaitGroup
	for worker := 0; worker < cfg.Concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				started := time.Now()
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
				if err != nil {
					results <- Result{Timestamp: started.UTC(), Latency: time.Since(started), Error: err.Error()}
					return
				}
				response, err := client.Do(request)
				result := Result{Timestamp: started.UTC(), Latency: time.Since(started)}
				if err != nil {
					if ctx.Err() == nil {
						result.Error = err.Error()
						results <- result
					}
					return
				}
				result.StatusCode = response.StatusCode
				if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 1024*1024)); err != nil {
					result.Error = err.Error()
				}
				if err := response.Body.Close(); err != nil && result.Error == "" {
					result.Error = err.Error()
				}
				results <- result
			}
		}()
	}

	go func() {
		workers.Wait()
		close(results)
	}()

	collected := make([]Result, 0, 1024)
	for result := range results {
		collected = append(collected, result)
	}
	if len(collected) == 0 {
		return nil, fmt.Errorf("load run completed without a request result")
	}
	return collected, nil
}

// Summarize calculates request counts and nearest-rank percentiles.
func Summarize(results []Result) Summary {
	if len(results) == 0 {
		return Summary{}
	}
	latencies := make([]time.Duration, 0, len(results))
	var sum time.Duration
	summary := Summary{Total: len(results)}
	for _, result := range results {
		latencies = append(latencies, result.Latency)
		sum += result.Latency
		if result.Error == "" && result.StatusCode >= 200 && result.StatusCode < 300 {
			summary.Successful++
		} else {
			summary.Failed++
		}
	}
	sort.Slice(latencies, func(left, right int) bool { return latencies[left] < latencies[right] })
	summary.Mean = sum / time.Duration(len(latencies))
	summary.P50 = nearestRank(latencies, 50)
	summary.P95 = nearestRank(latencies, 95)
	summary.P99 = nearestRank(latencies, 99)
	return summary
}

func nearestRank(sorted []time.Duration, percentile int) time.Duration {
	index := (percentile*len(sorted) + 99) / 100
	if index < 1 {
		index = 1
	}
	return sorted[index-1]
}
