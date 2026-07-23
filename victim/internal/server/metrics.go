package server

import (
	"fmt"
	"io"
	"math"
	"sync/atomic"
)

var latencyBounds = [...]float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

type histogram struct {
	buckets [len(latencyBounds) + 1]atomic.Uint64
	count   atomic.Uint64
	sumBits atomic.Uint64
}

func (h *histogram) observe(value float64) {
	index := len(latencyBounds)
	for candidate, bound := range latencyBounds {
		if value <= bound {
			index = candidate
			break
		}
	}
	h.buckets[index].Add(1)
	h.count.Add(1)
	for {
		oldBits := h.sumBits.Load()
		next := math.Float64frombits(oldBits) + value
		if h.sumBits.CompareAndSwap(oldBits, math.Float64bits(next)) {
			break
		}
	}
}

type metrics struct {
	requests atomic.Uint64
	errors   atomic.Uint64
	active   atomic.Int64
	latency  histogram
	work     histogram
}

func (m *metrics) writePrometheus(writer io.Writer) {
	fmt.Fprintln(writer, "# HELP cachewarden_http_requests_total Total HTTP requests received.")
	fmt.Fprintln(writer, "# TYPE cachewarden_http_requests_total counter")
	fmt.Fprintf(writer, "cachewarden_http_requests_total %d\n", m.requests.Load())
	fmt.Fprintln(writer, "# HELP cachewarden_http_errors_total Total HTTP responses with a status code of 400 or greater.")
	fmt.Fprintln(writer, "# TYPE cachewarden_http_errors_total counter")
	fmt.Fprintf(writer, "cachewarden_http_errors_total %d\n", m.errors.Load())
	fmt.Fprintln(writer, "# HELP cachewarden_http_active_requests Current in-flight HTTP requests.")
	fmt.Fprintln(writer, "# TYPE cachewarden_http_active_requests gauge")
	fmt.Fprintf(writer, "cachewarden_http_active_requests %d\n", m.active.Load())
	writeHistogram(writer, "cachewarden_http_request_duration_seconds", "HTTP request latency in seconds.", &m.latency)
	writeHistogram(writer, "cachewarden_work_duration_seconds", "Victim workload execution time in seconds.", &m.work)
}

func writeHistogram(writer io.Writer, name, help string, value *histogram) {
	fmt.Fprintf(writer, "# HELP %s %s\n", name, help)
	fmt.Fprintf(writer, "# TYPE %s histogram\n", name)
	var cumulative uint64
	for index, bound := range latencyBounds {
		cumulative += value.buckets[index].Load()
		fmt.Fprintf(writer, "%s_bucket{le=\"%g\"} %d\n", name, bound, cumulative)
	}
	cumulative += value.buckets[len(latencyBounds)].Load()
	fmt.Fprintf(writer, "%s_bucket{le=\"+Inf\"} %d\n", name, cumulative)
	fmt.Fprintf(writer, "%s_sum %g\n", name, math.Float64frombits(value.sumBits.Load()))
	fmt.Fprintf(writer, "%s_count %d\n", name, value.count.Load())
}
