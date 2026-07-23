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
readonly BENCHMARK_DURATION="${BENCHMARK_DURATION:-20s}"
readonly BENCHMARK_CONCURRENCY="${BENCHMARK_CONCURRENCY:-4}"
readonly BENCHMARK_TIMEOUT="${BENCHMARK_TIMEOUT:-10s}"
readonly VICTIM_PORT="${VICTIM_PORT:-18080}"
readonly TARGET_URL="${TARGET_URL:-http://127.0.0.1:${VICTIM_PORT}/work}"
readonly HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:${VICTIM_PORT}/health}"
readonly CACHEWARDEN_COMPOSE_PROJECT="${CACHEWARDEN_COMPOSE_PROJECT:-cachewarden-benchmark}"

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
for output in "${BASELINE_CSV}" "${INTERFERENCE_CSV}" "${SUMMARY_FILE}"; do
  if [[ -e "${output}" ]]; then
    echo "Refusing to overwrite existing result file: ${output}" >&2
    exit 1
  fi
done

compose() {
  COMPOSE_PROJECT_NAME="${CACHEWARDEN_COMPOSE_PROJECT}" \
    VICTIM_PORT="${VICTIM_PORT}" \
    docker compose -f "${COMPOSE_FILE}" "$@"
}

cleanup() {
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
baseline_stats="$(run_load "${BASELINE_CSV}")"

echo "Starting interference experiment."
compose --profile interference up --build --detach
wait_for_victim
sleep 3
interference_stats="$(run_load "${INTERFERENCE_CSV}")"

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
