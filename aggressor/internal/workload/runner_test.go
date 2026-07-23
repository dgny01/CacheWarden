package workload

import (
	"context"
	"testing"
	"time"
)

func TestRunnerProcessesBytesAndStops(t *testing.T) {
	runner, err := New(1024*1024, 2, "high")
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	runner.Run(ctx)
	if runner.TotalBytes() == 0 {
		t.Fatal("TotalBytes = 0, want processed data")
	}
}
