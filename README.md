# CacheWarden

CacheWarden is an experimental systems-performance project that studies noisy-neighbor interference between containerized workloads sharing hardware.

## Current status

CacheWarden currently provides an **interference lab**, not a production observability or remediation system. Part 1 established repeatable application-level degradation, and Part 2 used Linux `perf` and PMU counters to examine CPU execution, L3-cache behavior, and system-wide DRAM traffic. The repository implements a Go HTTP victim workload, a configurable Go aggressor workload, a concurrent Go HTTP load generator, Docker Compose configuration, an automated baseline-versus-interference benchmark, CSV latency output, summary statistics, tests, and CI-oriented build checks.

It does **not** currently implement eBPF telemetry, automatic noisy-neighbor detection, cache-contention diagnosis, Kubernetes integration, anomaly detection, AI reporting, or remediation.

## Key experimental finding

Across five controlled runs, enabling the aggressor increased mean victim latency by approximately 29% and reduced completed requests by approximately 23%, while preliminary container-level observations showed similar victim CPU and memory utilization.

These experiments establish repeatable interference, not its root cause.

## Architecture

```text
                         GET /work
  tools/loadgen  ---------------------------->  victim
       |                                         |
       | writes latency CSV                      | scans a fixed in-memory data set
       v                                         v
    results/                              Prometheus /metrics

  aggressor (opt-in)  ---> sequential read/write pressure on shared memory hardware
```

The independent Go modules are coordinated by [`go.work`](go.work):

- `victim` exposes `/health`, `/work`, and `/metrics`.
- `aggressor` allocates a bounded buffer and runs sequential read/write workers.
- `tools/loadgen` records individual request results as CSV and reports mean, p50, p95, and p99 latency.

Architecture decisions are recorded in [`docs/adr`](docs/adr/README.md).

## Quick start

From the repository root:

```bash
make test
make run-victim
```

In a second terminal:

```bash
curl http://127.0.0.1:8080/health
curl http://127.0.0.1:8080/work
curl http://127.0.0.1:8080/metrics
```

Stop the foreground victim with `Ctrl+C`.

## Requirements

Local development requires Go 1.22+, GNU Make, Bash, and `curl`. Container experiments additionally require Docker Engine or Docker Desktop and Docker Compose v2. No external HTTP load generator is required.

To build and check every Go module:

```bash
make build
make test
make lint
make race
```

For container operation:

```bash
# Victim only
docker compose up --build victim

# Victim plus the opt-in aggressor
docker compose --profile interference up --build

docker compose --profile interference down --remove-orphans
```

The Compose services define CPU and memory limits. Runtime support for these settings varies; inspect the effective container configuration before relying on it, and do not treat Compose limits as Kubernetes resource guarantees.

## Workload configuration

Both programs accept command-line flags that override environment variables. Run `go run ./cmd/victim --help` inside `victim`, or `go run ./cmd/aggressor --help` inside `aggressor`, for the complete flag list.

| Victim environment variable | Default | Purpose |
| --- | ---: | --- |
| `VICTIM_LISTEN_ADDRESS` | `:8080` | HTTP listen address |
| `VICTIM_WORKLOAD_SIZE` | `32MiB` | Preallocated data-set size |
| `VICTIM_WORKLOAD_ITERATIONS` | `2` | Full scans per `/work` request |
| `VICTIM_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |

| Aggressor environment variable | Default | Purpose |
| --- | ---: | --- |
| `AGGRESSOR_MODE` | `low` | `low`, `medium`, or `high` duty cycle |
| `AGGRESSOR_MEMORY` | `128MiB` | Total buffer size |
| `AGGRESSOR_WORKERS` | at most `2` | Concurrent memory workers |
| `AGGRESSOR_REPORT_INTERVAL` | `5s` | Throughput log interval |

For a deliberate local interference run:

```bash
AGGRESSOR_MODE=medium AGGRESSOR_MEMORY=256MiB make run-aggressor
```

## Reproducing the benchmark

Run:

```bash
make benchmark
```

The script checks prerequisites, starts the victim alone, records baseline traffic, enables the aggressor, records comparable interference traffic, and then removes its containers. Defaults are 20 seconds per phase with concurrency 4. The benchmark uses host port `18080` by default to avoid a local victim conflict.

```bash
BENCHMARK_DURATION=30s BENCHMARK_CONCURRENCY=8 make benchmark
```

Each run writes timestamped files without overwriting existing output:

```text
results/<timestamp>-baseline.csv
results/<timestamp>-interference.csv
results/<timestamp>-summary.txt
```

CSV rows contain a UTC start time, status code, latency in milliseconds, and any request error. The summary includes request totals, failures, mean, p50, p95, p99, and selected percentage changes.

## Part 1 — Reproducing noisy-neighbor interference

### Benchmark results

The reference dataset in [`docs/data/five-run-benchmark.csv`](docs/data/five-run-benchmark.csv) contains five independent runs using the same victim configuration, 20-second phases, concurrency 4, and zero failed requests. Baseline runs the victim without the aggressor; interference enables it.

![Five-run average victim latency, baseline versus interference](docs/images/latency-comparison.svg)

![Five-run average completed requests, baseline versus interference](docs/images/completed-requests.svg)

| Run | Baseline requests | Interference requests | Baseline mean (ms) | Interference mean (ms) | Baseline p50 (ms) | Interference p50 (ms) | Baseline p95 (ms) | Interference p95 (ms) | Baseline p99 (ms) | Interference p99 (ms) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 2,491 | 1,815 | 32.098 | 43.961 | 8.870 | 13.587 | 84.201 | 88.550 | 85.921 | 90.926 |
| 2 | 2,726 | 2,123 | 29.264 | 37.660 | 8.021 | 11.326 | 83.657 | 85.883 | 85.215 | 87.357 |
| 3 | 2,674 | 2,094 | 29.891 | 38.069 | 8.194 | 11.281 | 83.754 | 86.199 | 85.260 | 87.662 |
| 4 | 2,442 | 1,950 | 32.648 | 40.992 | 9.022 | 12.239 | 84.403 | 87.224 | 86.527 | 90.203 |
| 5 | 2,634 | 2,049 | 30.350 | 39.014 | 8.338 | 11.517 | 83.797 | 86.221 | 85.380 | 88.001 |

| Five-run average | Baseline | Interference | Change |
| --- | ---: | ---: | ---: |
| Mean latency | 30.85 ms | 39.94 ms | +29.42% |
| p50 latency | 8.49 ms | 11.99 ms | +41.17% |
| p95 latency | 83.96 ms | 86.82 ms | +3.40% |
| p99 latency | 85.66 ms | 88.83 ms | +3.69% |
| Completed requests | 2,593 | 2,006 | -22.66% |

All five runs had the same qualitative direction: latency increased and completed request count decreased. Failures were zero. These host- and configuration-specific measurements demonstrate repeatable degradation when the aggressor is active; they do not establish LLC/cache contention, memory-bandwidth saturation, or another single cause.

Regenerate the committed charts with only the Python standard library:

```bash
python3 tools/plot_results.py
```

## Why container-level metrics were insufficient

A preliminary `docker stats` observation recorded the following approximate snapshots:

| Phase / container | CPU | Memory |
| --- | ---: | ---: |
| Baseline victim | 103.58% | 40.28 MiB / 256 MiB |
| Interference victim | 104.11% | 41.15 MiB / 256 MiB |
| Aggressor during interference | 205.15% | 260.6 MiB / 512 MiB |

In this preliminary observation, victim application performance degraded while its conventional CPU and memory utilization remained broadly similar. This motivates investigation below ordinary container-level resource metrics. These snapshots are not rigorous proof, and Docker metrics remain useful inputs to an investigation.

## Part 2 — Low-level performance investigation

Part 2 used paired baseline/interference measurements to test whether the aggressor changed low-level execution and shared memory-subsystem behavior. These measurements narrow the investigation, but they do not by themselves identify a single root cause.

### CPU execution and IPC

A recent paired manual `perf` experiment produced the following measurements:

| Metric | Baseline | Interference | Approximate change |
| --- | ---: | ---: | ---: |
| task-clock | 9,979.85 ms | 9,994.61 ms | essentially unchanged |
| cycles | 41,526,308,835 | 37,582,130,258 | -9.5% |
| instructions | 119,969,148,554 | 102,537,856,119 | -14.5% |
| IPC | 2.89 | 2.73 | -5.6% |
| APERF / MPERF estimated frequency | 4.19 GHz | 3.78 GHz | -9.6% |

The frequency estimate uses APERF/MPERF and a 2.30 GHz nominal/base frequency; cycles per task-clock were approximately 4.16 GHz in the baseline. The victim received approximately the same CPU execution time in this paired run, so this measurement does not support simple scheduling starvation. The aggressor coincided with lower effective core frequency and a modest reduction in instructions retired per cycle. This does not prove that frequency behavior caused the application slowdown.

### L3-cache experiment

Three additional paired `perf` measurements collected `cycles`, `instructions`, `mem_load_retired.l3_hit`, `mem_load_retired.l3_miss`, and `cycle_activity.stalls_l3_miss`. The derived metrics are:

```text
IPC = instructions / cycles
L3 MPKI = L3 misses / instructions * 1000
L3 stall fraction = L3-miss stall cycles / cycles
```

The raw counter values were:

| Pair | Phase | Cycles | Instructions | L3 hits | L3 misses | L3-miss stall cycles |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | Baseline | 39,401,527,525 | 112,642,239,906 | 1,747,143 | 1,152,812 | 409,145,575 |
| 1 | Interference | 36,969,713,464 | 101,089,744,690 | 1,717,490 | 1,119,679 | 432,080,042 |
| 2 | Baseline | 40,251,014,300 | 116,989,087,530 | 1,810,135 | 1,215,698 | 403,299,746 |
| 2 | Interference | 27,505,150,330 | 77,972,711,839 | 1,668,254 | 1,089,851 | 287,413,785 |
| 3 | Baseline | 39,658,072,097 | 114,999,430,347 | 1,780,220 | 1,167,341 | 411,717,149 |
| 3 | Interference | 32,146,670,325 | 91,688,360,061 | 1,704,588 | 1,006,876 | 321,098,347 |

Derived results and within-pair changes were:

| Pair | IPC baseline | IPC interference | IPC change | L3 MPKI baseline | L3 MPKI interference | MPKI change | Stall baseline | Stall interference | Stall change |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 2.859 | 2.735 | -4.3% | 0.01023 | 0.01108 | +8.3% | 1.039% | 1.169% | +12.5% |
| 2 | 2.91 | 2.83 | -2.5% | 0.01039 | 0.01398 | +34.6% | 1.00% | 1.04% | +4.3% |
| 3 | 2.899 | 2.852 | -1.6% | 0.01015 | 0.01098 | +8.2% | 1.038% | 0.999% | -3.8% |

| Signal | Direction under interference | Consistency |
| --- | --- | --- |
| IPC | Decreased | 3 of 3 pairs |
| L3 MPKI | Increased | 3 of 3 pairs |
| L3-miss stall fraction | Increased twice, decreased once | Not consistent |

The aggressor therefore measurably changed the victim's execution and L3-cache behavior. The consistent IPC decrease and L3 MPKI increase are evidence of an association, but the inconsistent L3-miss stall fraction means these measurements do not establish L3 contention as the cause of the slowdown.

### DRAM / memory-controller experiment

The host exposes the uncore memory-controller events `unc_mc0_rdcas_count_freerun` and `unc_mc0_wrcas_count_freerun`. Each count represents one 64-byte DRAM transfer. A system-wide measurement during the controlled workload produced:

| Phase | Read requests | Write requests | Duration | Read bandwidth | Write bandwidth | Total bandwidth |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Baseline | 405,705,699 | 13,007,294 | ~10.0014 s | ~2.60 GB/s | ~0.083 GB/s | ~2.68 GB/s |
| Interference | 574,839,991 | 187,609,907 | ~10.0019 s | ~3.68 GB/s | ~1.20 GB/s | ~4.88 GB/s |

Total system-wide DRAM traffic increased from approximately 2.68 GB/s to 4.88 GB/s, an increase of approximately 82%. This establishes that enabling the aggressor created substantially more traffic at the shared memory controller during the measurement.

These are **system-wide uncore counters**, not victim-process counters. The 4.88 GB/s value must not be attributed entirely to the victim, and the measurement does not demonstrate DRAM bandwidth saturation, DRAM latency, or either one as the cause of the victim latency increase.

### PMU and TMA measurement limitations

The investigation also attempted to collect `tma_info_system_dram_bw_use`, `tma_dram_bound`, `tma_mem_bandwidth`, `tma_mem_latency`, and `tma_info_memory_load_miss_real_latency`. Although `perf list` exposed some of these metrics, `perf` could not evaluate them because required underlying events were unavailable on this system. Examples included:

- `UNC_ARB_TRK_REQUESTS.ALL`: not supported
- `UNC_ARB_COH_TRK_REQUESTS.ALL`: not supported
- `CYCLE_ACTIVITY.STALLS_L2_MISS`: unavailable
- `MEM_LOAD_RETIRED.FB_HIT`: unavailable

This is a host PMU/tooling limitation, not an application failure. It prevents this experiment from using those TMA metrics to distinguish bandwidth-bound from latency-bound behavior.

### What the measurements establish

| Supported by the measurements | Not established by the measurements |
| --- | --- |
| Repeatable application-level degradation | L3 contention as the root cause |
| Lower IPC in all three L3 pairs | DRAM bandwidth saturation |
| Higher L3 MPKI in all three L3 pairs | The victim consuming all measured DRAM traffic |
| Inconsistent L3-miss stall-fraction changes | DRAM latency as the root cause |
| Approximately 82% more system-wide DRAM traffic | CPU frequency as the root cause |

The Part 2 result is deliberately bounded: the aggressor measurably changes the victim's low-level execution behavior and substantially increases shared DRAM traffic. IPC decreased and L3 MPKI increased consistently across three paired measurements, while L3-miss-related stall behavior was inconsistent. The available evidence does not identify L3 contention, DRAM bandwidth saturation, memory latency, CPU frequency, or another single mechanism as the root cause of the application slowdown.

## Metrics and observability available today

`GET /metrics` exposes Prometheus text metrics:

- `cachewarden_http_requests_total`
- `cachewarden_http_request_duration_seconds`
- `cachewarden_work_duration_seconds`
- `cachewarden_http_errors_total`
- `cachewarden_http_active_requests`

Prometheus is not bundled; an existing instance can scrape the victim if time-series collection is needed. The benchmark additionally provides request-level CSV latency data and summary percentiles. PMU collection is currently a manual investigation, not an integrated telemetry feature.

## Limitations

- The workloads are synthetic and results are host- and configuration-specific.
- Scheduler placement, NUMA topology, caches, memory channels, frequency scaling, thermal state, and background activity are not controlled by this experiment.
- The benchmark is closed-loop concurrency, not a fixed-rate arrival model.
- Process-local metrics reset on restart, and container resource controls vary by runtime and platform.
- The available measurements cannot distinguish among the remaining root-cause hypotheses.

## Current project status and Part 3

1. Part 1: establish reproducible interference — completed.
2. Part 2: characterize CPU execution, L3 behavior, and system-wide DRAM traffic — completed, without claiming a single root cause.
3. Part 3: use the Part 2 findings to select useful signals and automate their collection where appropriate.
4. Investigate process- and cgroup-level attribution.
5. Use eBPF where kernel-level observability or attribution provides a concrete benefit, potentially alongside rather than instead of `perf`/PMU sources.
6. Evaluate detection and mitigation only after the signals are understood.
7. Validate any justified final approach in container and Kubernetes scenarios.

Part 3 is future work. eBPF telemetry, Kubernetes integration, and remediation are not currently implemented features or committed prerequisites.

## Repository structure

```text
aggressor/       Configurable Go interference workload
victim/          Go HTTP service under test
tools/loadgen/   Concurrent Go HTTP load generator
scripts/         Benchmark automation
docs/adr/        Architecture decision records
docs/data/       Committed Part 1 reference dataset
docs/images/     Reproducible Part 1 result charts
results/         Timestamped local benchmark output
```

## Safety

The aggressor is a controlled experiment workload, not a denial-of-service or general-purpose stress-testing tool. Defaults are conservative; invalid modes, worker counts, and allocations above 1 GiB are rejected, and Linux additionally rejects requests above half of `MemAvailable`. The Compose aggressor has a 512 MiB memory limit and a 2.0 CPU limit, and is absent from the default profile.

Run experiments only on systems you own or are authorized to test. Avoid production nodes, shared development hosts, or systems already under memory pressure. OOM termination remains possible when runtime overhead and the configured buffer reach the effective limit.

## License

CacheWarden is available under the [MIT License](LICENSE).
