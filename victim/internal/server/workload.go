package server

import (
	"context"
	"runtime"
)

type workload struct {
	values     []uint64
	iterations int
}

func newWorkload(sizeBytes, iterations int) *workload {
	count := sizeBytes / 8
	values := make([]uint64, count)
	for index := range values {
		values[index] = uint64(index)*0x9e3779b97f4a7c15 + 0x517cc1b727220a95
	}
	return &workload{values: values, iterations: iterations}
}

func (w *workload) run(ctx context.Context) (uint64, error) {
	var checksum uint64
	for iteration := 0; iteration < w.iterations; iteration++ {
		for index, value := range w.values {
			checksum ^= (value + uint64(iteration)) * uint64(index+1)
			if index%4096 == 0 {
				select {
				case <-ctx.Done():
					return 0, ctx.Err()
				default:
				}
			}
		}
	}
	// Keep the backing array live until the scan completes so the compiler cannot discard the reads.
	runtime.KeepAlive(w.values)
	return checksum, nil
}
