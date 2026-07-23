package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// Handler provides the victim HTTP API.
type Handler struct {
	logger   *slog.Logger
	workload *workload
	metrics  metrics
	mux      *http.ServeMux
}

// New builds a handler with a preallocated, read-only workload.
func New(logger *slog.Logger, workloadBytes, iterations int) *Handler {
	handler := &Handler{
		logger:   logger,
		workload: newWorkload(workloadBytes, iterations),
		mux:      http.NewServeMux(),
	}
	handler.mux.HandleFunc("/health", handler.health)
	handler.mux.HandleFunc("/work", handler.work)
	handler.mux.HandleFunc("/metrics", handler.prometheus)
	return handler
}

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	h.metrics.requests.Add(1)
	h.metrics.active.Add(1)
	defer h.metrics.active.Add(-1)

	recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
	h.mux.ServeHTTP(recorder, request)
	if recorder.status >= http.StatusBadRequest {
		h.metrics.errors.Add(1)
	}
	h.metrics.latency.observe(time.Since(started).Seconds())
}

func (h *Handler) health(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeMethodNotAllowed(writer)
		return
	}
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte("OK\n"))
}

func (h *Handler) work(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeMethodNotAllowed(writer)
		return
	}
	started := time.Now()
	checksum, err := h.workload.run(request.Context())
	duration := time.Since(started)
	h.metrics.work.observe(duration.Seconds())
	if err != nil {
		http.Error(writer, "workload canceled", http.StatusRequestTimeout)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	response := struct {
		Checksum   string  `json:"checksum"`
		DurationMS float64 `json:"duration_ms"`
		WorkloadB  int     `json:"workload_bytes"`
		Iterations int     `json:"iterations"`
	}{
		Checksum:   strconv.FormatUint(checksum, 10),
		DurationMS: float64(duration.Microseconds()) / 1000,
		WorkloadB:  len(h.workload.values) * 8,
		Iterations: h.workload.iterations,
	}
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		h.logger.Error("encode work response", "error", err)
	}
}

func (h *Handler) prometheus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeMethodNotAllowed(writer)
		return
	}
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	h.metrics.writePrometheus(writer)
}

func writeMethodNotAllowed(writer http.ResponseWriter) {
	writer.Header().Set("Allow", http.MethodGet)
	http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
