package workload

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type modeSettings struct {
	passes int
	pause  time.Duration
}

// Runner owns the allocated buffer and runs bounded memory workers.
type Runner struct {
	buffer  []byte
	workers int
	mode    modeSettings
	bytes   atomic.Uint64
	sink    atomic.Uint64
}

// New allocates and touches the requested memory, returning a clear error on panic.
func New(memoryBytes, workers int, mode string) (runner *Runner, err error) {
	settings := map[string]modeSettings{
		"low":    {passes: 1, pause: 20 * time.Millisecond},
		"medium": {passes: 2, pause: 5 * time.Millisecond},
		"high":   {passes: 4, pause: 0},
	}[mode]

	defer func() {
		if recovered := recover(); recovered != nil {
			runner = nil
			err = fmt.Errorf("allocate %d bytes for aggressor: %v", memoryBytes, recovered)
		}
	}()
	buffer := make([]byte, memoryBytes)
	for index := 0; index < len(buffer); index += 4096 {
		buffer[index] = byte(index)
	}
	return &Runner{buffer: buffer, workers: workers, mode: settings}, nil
}

// Run starts workers and blocks until cancellation.
func (r *Runner) Run(ctx context.Context) {
	var group sync.WaitGroup
	for worker := 0; worker < r.workers; worker++ {
		start := worker * len(r.buffer) / r.workers
		end := (worker + 1) * len(r.buffer) / r.workers
		group.Add(1)
		go func(identifier int, chunk []byte) {
			defer group.Done()
			r.runWorker(ctx, identifier, chunk)
		}(worker, r.buffer[start:end])
	}
	group.Wait()
	runtime.KeepAlive(r.buffer)
}

// TotalBytes returns an approximate count of bytes read and written.
func (r *Runner) TotalBytes() uint64 {
	return r.bytes.Load()
}

func (r *Runner) runWorker(ctx context.Context, identifier int, chunk []byte) {
	var checksum uint64
	for {
		for pass := 0; pass < r.mode.passes; pass++ {
			for index := range chunk {
				value := chunk[index]
				value += byte(index + pass + identifier + 1)
				chunk[index] = value
				checksum += uint64(value)
			}
			r.bytes.Add(uint64(len(chunk)) * 2)
			select {
			case <-ctx.Done():
				r.sink.Add(checksum)
				return
			default:
			}
		}
		if r.mode.pause > 0 {
			timer := time.NewTimer(r.mode.pause)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				r.sink.Add(checksum)
				return
			case <-timer.C:
			}
		}
	}
}
