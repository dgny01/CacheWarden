#!/usr/bin/env bash
set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
readonly RESULTS_DIR="${PROJECT_ROOT}/results"
readonly COMPOSE_FILE="${PROJECT_ROOT}/docker-compose.yml"
readonly RUN_TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
readonly BASELINE_CSV="${RESULTS_DIR}/${RUN_TIMESTAMP}-baseline.csv"
readonly INTERFERENCE_CSV="${RESULTS_DIR}/${RUN_TIMESTAMP}-interference.csv"
readonly SUMMARY_FILE="${RESULTS_DIR}/${RUN_TIMESTAMP}-summary.txt"
readonly BASELINE_SCHEDULER_CSV="${RESULTS_DIR}/${RUN_TIMESTAMP}-baseline-scheduler.csv"
readonly INTERFERENCE_SCHEDULER_CSV="${RESULTS_DIR}/${RUN_TIMESTAMP}-interference-scheduler.csv"
readonly BENCHMARK_DURATION="${BENCHMARK_DURATION:-20s}"
readonly BENCHMARK_CONCURRENCY="${BENCHMARK_CONCURRENCY:-4}"
readonly BENCHMARK_TIMEOUT="${BENCHMARK_TIMEOUT:-10s}"
readonly VICTIM_PORT="${VICTIM_PORT:-18080}"
readonly TARGET_URL="${TARGET_URL:-http://127.0.0.1:${VICTIM_PORT}/work}"
readonly HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:${VICTIM_PORT}/health}"
readonly CACHEWARDEN_COMPOSE_PROJECT="${CACHEWARDEN_COMPOSE_PROJECT:-cachewarden-benchmark}"
readonly ENABLE_SCHEDULER="${ENABLE_SCHEDULER:-0}"
readonly SCHEDULER_OBJECT="${SCHEDULER_OBJECT:-${PROJECT_ROOT}/bin/ebpf/scheduler.bpf.o}"
readonly SCHEDULER_COLLECTOR="${SCHEDULER_COLLECTOR:-${PROJECT_ROOT}/bin/ebpf/scheduler-collector}"
scheduler_pid=""

if [[ "${ENABLE_SCHEDULER}" != "0" && "${ENABLE_SCHEDULER}" != "1" ]]; then
  echo "ENABLE_SCHEDULER must be 0 or 1." >&2
  exit 1
fi

for command in go docker curl; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "Required command '${command}' was not found." >&2
    exit 1
  fi
done
if ! docker compose version >/dev/null 2>&1; then
  echo "Docker Compose v2 is required. Install the Docker Compose plugin and retry." >&2
  exit 1
fi

mkdir -p "${RESULTS_DIR}"
outputs=("${BASELINE_CSV}" "${INTERFERENCE_CSV}" "${SUMMARY_FILE}")
if [[ "${ENABLE_SCHEDULER}" == "1" ]]; then
  [[ -x "${SCHEDULER_COLLECTOR}" ]] || { echo "Scheduler collector not found; run 'make ebpf' first: ${SCHEDULER_COLLECTOR}" >&2; exit 1; }
  [[ -r "${SCHEDULER_OBJECT}" ]] || { echo "Scheduler BPF object not found; run 'make ebpf' first: ${SCHEDULER_OBJECT}" >&2; exit 1; }
  outputs+=("${BASELINE_SCHEDULER_CSV}" "${INTERFERENCE_SCHEDULER_CSV}")
fi
for output in "${outputs[@]}"; do
  if [[ -e "${output}" ]]; then
    echo "Refusing to overwrite existing result file: ${output}" >&2
    exit 1
  fi
done

compose() {
  env \
    COMPOSE_PROJECT_NAME="${CACHEWARDEN_COMPOSE_PROJECT}" \
    VICTIM_PORT="${VICTIM_PORT}" \
    docker compose -f "${COMPOSE_FILE}" "$@"
}

cleanup() {
  stop_scheduler || true
  compose --profile interference down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

wait_for_victim() {
  local deadline=$((SECONDS + 90))
  until curl --fail --silent --show-error "${HEALTH_URL}" >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      echo "Victim did not become healthy within 90 seconds." >&2
      compose logs victim >&2 || true
      exit 1
    fi
    sleep 1
  done
}

start_scheduler() {
  local output_path="$1" container_id host_pid
  [[ "${ENABLE_SCHEDULER}" == "1" ]] || return 0
  container_id="$(compose ps --quiet victim)"
  [[ -n "${container_id}" ]] || { echo "Cannot identify the victim container." >&2; return 1; }
  host_pid="$(docker inspect --format '{{.State.Pid}}' "${container_id}")"
  [[ "${host_pid}" =~ ^[1-9][0-9]*$ ]] || { echo "Cannot identify the victim container's host PID." >&2; return 1; }
  "${SCHEDULER_COLLECTOR}" --object "${SCHEDULER_OBJECT}" --pid "${host_pid}" --output "${output_path}" &
  scheduler_pid=$!
  # Detect load/attach failures before starting the measured workload.
  sleep 1
  if ! kill -0 "${scheduler_pid}" 2>/dev/null; then
    wait "${scheduler_pid}" || true
    scheduler_pid=""
    echo "Scheduler collector failed to start." >&2
    return 1
  fi
}

stop_scheduler() {
  local status
  [[ -n "${scheduler_pid}" ]] || return 0
  kill -TERM "${scheduler_pid}" 2>/dev/null || true
  if wait "${scheduler_pid}"; then status=0; else status=$?; fi
  scheduler_pid=""
  if (( status != 0 )); then
    echo "Scheduler collector exited with status ${status}." >&2
    return "${status}"
  fi
}

validate_scheduler_output() {
  local output_path="$1"
  [[ "${ENABLE_SCHEDULER}" == "1" ]] || return 0
  awk -F, '
    NR == 1 { valid_header = ($1 == "metric" && $2 == "value" && $3 == "unit"); next }
    $1 == "observed_delays" && $2 ~ /^[0-9]+$/ { observed = 1 }
    $1 == "schedule_ins" && $2 ~ /^[0-9]+$/ { schedule_ins = 1 }
    $1 == "mean_delay_ns" && $2 ~ /^[0-9]+$/ { mean = 1 }
    $1 == "p50_delay_upper_bound_ns" && $2 ~ /^[0-9]+$/ { p50 = 1 }
    $1 == "p95_delay_upper_bound_ns" && $2 ~ /^[0-9]+$/ { p95 = 1 }
    $1 == "p99_delay_upper_bound_ns" && $2 ~ /^[0-9]+$/ { p99 = 1 }
    END { exit !(valid_header && observed && schedule_ins && mean && p50 && p95 && p99) }
  ' "${output_path}" || {
    echo "Scheduler collector produced an invalid or incomplete result: ${output_path}" >&2
    return 1
  }
}

run_load() {
  local output_path="$1"
  (
    cd "${PROJECT_ROOT}"
    go run ./tools/loadgen/cmd/loadgen \
      --url "${TARGET_URL}" \
      --duration "${BENCHMARK_DURATION}" \
      --concurrency "${BENCHMARK_CONCURRENCY}" \
      --timeout "${BENCHMARK_TIMEOUT}" \
      --output "${output_path}"
  )
}

metric() {
  local data="$1"
  local key="$2"
  awk -F= -v wanted="${key}" '$1 == wanted { print $2 }' <<<"${data}"
}

percent_change() {
  local baseline="$1"
  local interference="$2"
  awk -v baseline="${baseline}" -v interference="${interference}" \
    'BEGIN {
      if (baseline == 0) {
        print "n/a"
      } else {
        printf "%.2f%%", ((interference - baseline) / baseline) * 100
      }
    }'
}

echo "Starting baseline experiment on ${TARGET_URL}."
compose --profile interference down --remove-orphans >/dev/null 2>&1 || true
compose up --build --detach victim
wait_for_victim
start_scheduler "${BASELINE_SCHEDULER_CSV}"
baseline_stats="$(run_load "${BASELINE_CSV}")"
stop_scheduler
validate_scheduler_output "${BASELINE_SCHEDULER_CSV}"

echo "Starting interference experiment."
compose --profile interference up --build --detach
wait_for_victim
sleep 3
start_scheduler "${INTERFERENCE_SCHEDULER_CSV}"
interference_stats="$(run_load "${INTERFERENCE_CSV}")"
stop_scheduler
validate_scheduler_output "${INTERFERENCE_SCHEDULER_CSV}"

baseline_mean="$(metric "${baseline_stats}" mean_latency_ms)"
interference_mean="$(metric "${interference_stats}" mean_latency_ms)"
baseline_p95="$(metric "${baseline_stats}" p95_latency_ms)"
interference_p95="$(metric "${interference_stats}" p95_latency_ms)"
baseline_p99="$(metric "${baseline_stats}" p99_latency_ms)"
interference_p99="$(metric "${interference_stats}" p99_latency_ms)"

{
  echo "CacheWarden benchmark summary"
  echo "Timestamp (UTC): ${RUN_TIMESTAMP}"
  echo "Target: ${TARGET_URL}"
  echo "Duration per phase: ${BENCHMARK_DURATION}"
  echo "Concurrency: ${BENCHMARK_CONCURRENCY}"
  echo
  echo "Baseline"
  echo "  Total requests: $(metric "${baseline_stats}" total_requests)"
  echo "  Successful requests: $(metric "${baseline_stats}" successful_requests)"
  echo "  Failed requests: $(metric "${baseline_stats}" failed_requests)"
  echo "  Mean latency: ${baseline_mean} ms"
  echo "  p50 latency: $(metric "${baseline_stats}" p50_latency_ms) ms"
  echo "  p95 latency: ${baseline_p95} ms"
  echo "  p99 latency: ${baseline_p99} ms"
  echo
  echo "Interference"
  echo "  Total requests: $(metric "${interference_stats}" total_requests)"
  echo "  Successful requests: $(metric "${interference_stats}" successful_requests)"
  echo "  Failed requests: $(metric "${interference_stats}" failed_requests)"
  echo "  Mean latency: ${interference_mean} ms"
  echo "  p50 latency: $(metric "${interference_stats}" p50_latency_ms) ms"
  echo "  p95 latency: ${interference_p95} ms"
  echo "  p99 latency: ${interference_p99} ms"
  echo
  echo "Latency change from baseline"
  echo "  Mean: $(percent_change "${baseline_mean}" "${interference_mean}")"
  echo "  p95: $(percent_change "${baseline_p95}" "${interference_p95}")"
  echo "  p99: $(percent_change "${baseline_p99}" "${interference_p99}")"
} >"${SUMMARY_FILE}"

echo "Benchmark completed."
echo "Baseline CSV: ${BASELINE_CSV}"
echo "Interference CSV: ${INTERFERENCE_CSV}"
echo "Summary: ${SUMMARY_FILE}"
if [[ "${ENABLE_SCHEDULER}" == "1" ]]; then
  echo "Baseline scheduler CSV: ${BASELINE_SCHEDULER_CSV}"
  echo "Interference scheduler CSV: ${INTERFERENCE_SCHEDULER_CSV}"
fi
