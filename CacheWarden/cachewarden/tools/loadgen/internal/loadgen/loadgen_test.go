package loadgen

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRunAndSummarize(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			time.Sleep(100 * time.Microsecond)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     make(http.Header),
			}, nil
		}),
	}
	cfg := Config{
		URL:         "http://victim.test/work",
		Duration:    30 * time.Millisecond,
		Concurrency: 2,
		Timeout:     time.Second,
	}
	results, err := runWithClient(context.Background(), cfg, client)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	summary := Summarize(results)
	if summary.Total == 0 || summary.Successful != summary.Total || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestSummarizePercentiles(t *testing.T) {
	results := make([]Result, 100)
	for index := range results {
		results[index] = Result{
			StatusCode: http.StatusOK,
			Latency:    time.Duration(index+1) * time.Millisecond,
		}
	}
	summary := Summarize(results)
	if summary.Mean != 50*time.Millisecond+500*time.Microsecond {
		t.Fatalf("Mean = %s, want 50.5ms", summary.Mean)
	}
	if summary.P50 != 50*time.Millisecond || summary.P95 != 95*time.Millisecond || summary.P99 != 99*time.Millisecond {
		t.Fatalf("unexpected percentiles: p50=%s p95=%s p99=%s", summary.P50, summary.P95, summary.P99)
	}
}

func TestRunRejectsInvalidConfig(t *testing.T) {
	_, err := Run(context.Background(), Config{URL: "http://localhost", Duration: time.Second, Concurrency: 0, Timeout: time.Second})
	if err == nil {
		t.Fatal("Run succeeded, want an error")
	}
}
