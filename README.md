# CacheWarden

CacheWarden is an experimental resource-management project for studying
performance interference caused by workloads that share hardware. Its long-term
goal is to detect noisy neighbors in Kubernetes and support explainable,
policy-controlled remediation.

This repository currently implements only **Milestone 1: Interference Lab**.
It does not yet include eBPF, Kubernetes integration, anomaly detection, or an
LLM.

## Quick start

Run these first three commands from the repository root:

```bash
make test
make run-victim
curl http://127.0.0.1:8080/work
```

The second command stays in the foreground. Run the third command in another
terminal, then stop the victim with `Ctrl+C`.

## Milestone scope

Milestone 1 provides:

- a Go HTTP service named `victim` whose controlled memory workload can be
  measured;
- a bounded Go workload named `aggressor` that creates configurable memory
  bandwidth pressure;
- an opt-in Docker Compose interference profile;
- a dependency-free Go load generator and a reproducible two-phase benchmark;
- tests, local build commands, container images, CI, and operational
  documentation.

Later milestones listed below are intentionally not represented as completed.

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

The components are independent Go modules coordinated by the root `go.work`
file:

- `victim` exposes `/health`, `/work`, and `/metrics`.
- `aggressor` allocates a bounded buffer and runs sequential read/write workers.
- `tools/loadgen` records every request in CSV and calculates latency
  percentiles.

Architecture decisions are recorded in [`docs/adr`](docs/adr).

## Requirements

For local development:

- Go 1.22 or newer;
- GNU Make;
- Bash for the benchmark script;
- `curl` for health checks and examples.

For container and full benchmark workflows:

- Docker Engine or Docker Desktop;
- Docker Compose v2 (`docker compose`);
- enough free capacity for the conservative container limits described below.

No `hey`, `vegeta`, or `wrk` installation is required. The repository includes
its own small Go load generator to keep the experiment reproducible.

## Local operation

Build, test, and validate all modules:

```bash
make build
make test
make lint
make race
```

Start the victim:

```bash
make run-victim
```

Exercise its endpoints:

```bash
curl http://127.0.0.1:8080/health
curl http://127.0.0.1:8080/work
curl http://127.0.0.1:8080/metrics
```

Start the aggressor separately only when you intend to create interference:

```bash
make run-aggressor
```

Both programs accept command-line flags. Flags override environment variables.
Use `go run ./cmd/victim --help` inside `victim`, or
`go run ./cmd/aggressor --help` inside `aggressor`, for the complete flag list.

Common victim settings:

| Environment variable | Default | Purpose |
| --- | ---: | --- |
| `VICTIM_LISTEN_ADDRESS` | `:8080` | HTTP listen address |
| `VICTIM_WORKLOAD_SIZE` | `32MiB` | Preallocated data-set size |
| `VICTIM_WORKLOAD_ITERATIONS` | `2` | Full scans per `/work` request |
| `VICTIM_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |

Common aggressor settings:

| Environment variable | Default | Purpose |
| --- | ---: | --- |
| `AGGRESSOR_MODE` | `low` | `low`, `medium`, or `high` duty cycle |
| `AGGRESSOR_MEMORY` | `128MiB` | Total buffer size |
| `AGGRESSOR_WORKERS` | at most `2` | Concurrent memory workers |
| `AGGRESSOR_REPORT_INTERVAL` | `5s` | Throughput log interval |

Example of a deliberate local interference run:

```bash
AGGRESSOR_MODE=medium AGGRESSOR_MEMORY=256MiB make run-aggressor
```

## Docker operation

Build and start only the victim:

```bash
docker compose up --build victim
```

Start the victim and the opt-in aggressor:

```bash
docker compose --profile interference up --build
```

Stop both:

```bash
docker compose --profile interference down --remove-orphans
```

The Compose file applies service-level `cpus` and `mem_limit` settings. Modern
Docker Compose v2 passes these limits to the local container runtime. Older
Compose implementations, non-Docker runtimes, and some compatibility layers may
interpret or ignore resource controls differently. Confirm the effective limits
with `docker inspect` before relying on them, and do not assume that Compose
limits are equivalent to Kubernetes resource requests or limits.

## Baseline experiment

The complete benchmark automates both phases, but a baseline can also be
observed manually:

```bash
docker compose up --build victim
curl http://127.0.0.1:8080/work
```

Leave the `interference` profile disabled. Repeated requests should establish
the host-specific latency distribution for the configured workload size and
iteration count.

## Interference experiment

For the comparable interference phase, keep the victim configuration unchanged
and explicitly enable the aggressor:

```bash
docker compose --profile interference up --build
curl http://127.0.0.1:8080/work
```

The most reproducible workflow is:

```bash
make benchmark
```

The script:

1. verifies `go`, `docker`, Docker Compose v2, and `curl`;
2. starts an isolated Compose project with only the victim;
3. records baseline traffic;
4. enables the aggressor and records the same traffic;
5. stops its containers without deleting benchmark files.

Safe benchmark settings can be changed explicitly:

```bash
BENCHMARK_DURATION=30s BENCHMARK_CONCURRENCY=8 make benchmark
```

The default benchmark host port is `18080` to reduce collisions with a locally
running victim. Set `VICTIM_PORT` and `TARGET_URL` together if a different target
is required.

## Benchmark output and interpretation

Each run creates new timestamped files and refuses to overwrite an existing
file:

```text
results/<timestamp>-baseline.csv
results/<timestamp>-interference.csv
results/<timestamp>-summary.txt
```

CSV rows contain UTC start time, status code, latency in milliseconds, and any
request error. The summary reports total, successful, and failed requests plus
mean, p50, p95, and p99 latency for both phases. It also calculates the
percentage change in mean, p95, and p99 latency.

A positive latency change indicates that the interference phase was slower. One
run is not proof of causation: repeat the experiment, keep configuration fixed,
check thermal throttling and unrelated host activity, and compare distributions
rather than only the mean. Results are host-specific and generated result files
are intentionally ignored by Git.

## Metrics

`GET /metrics` exposes Prometheus text format with:

- `cachewarden_http_requests_total`;
- `cachewarden_http_request_duration_seconds`;
- `cachewarden_work_duration_seconds`;
- `cachewarden_http_errors_total`;
- `cachewarden_http_active_requests`.

Prometheus is not bundled in this milestone. Point an existing Prometheus
instance at the victim if time-series collection is needed.

## Safety and resource use

The aggressor is a controlled experiment workload, not a denial-of-service or
general-purpose stress-testing tool.

- Defaults are intentionally conservative.
- The program rejects invalid modes, worker counts, and allocations above
  `1GiB`.
- On Linux it also rejects requests above half of `MemAvailable`.
- The Compose aggressor has a `512MiB` memory limit and `2.0` CPU limit.
- The aggressor never starts in the default Compose profile.

Run experiments only on systems you own or are authorized to test. Save work
before changing defaults. Avoid production nodes, shared development hosts, and
systems already under memory pressure. Container OOM termination remains
possible when runtime overhead plus the configured buffer reaches the effective
limit.

## Known limitations

- The workload is synthetic and does not model every application access
  pattern.
- Scheduler placement, NUMA topology, CPU caches, memory channels, frequency
  scaling, and background activity are not controlled.
- The custom metrics implementation is process-local and resets on restart.
- The benchmark uses closed-loop concurrent requests rather than a fixed-rate
  arrival model.
- Container limits vary by runtime and platform.
- There is no eBPF telemetry, cgroup-to-container mapping, hardware performance
  counter collection, Kubernetes controller, or automated remediation yet.

## Next milestones

Planned work, not implemented in this repository version:

1. eBPF telemetry;
2. cgroup-to-container mapping;
3. hardware performance counters;
4. Isolation Forest anomaly detection;
5. Kubernetes remediation;
6. LLM-assisted reporting.

## Creating the GitHub repository

GitHub CLI was not available when this local repository was prepared, so no
remote was created. After installing and authenticating `gh`, run this exact
command from the repository root:

```bash
gh repo create cachewarden --public --source=. --remote=origin --push
```

Before running it, use `git remote -v` to confirm that no remote was added by
someone else. Never overwrite an existing remote or repository.

## Repository language and contributions

All source code, identifiers, comments, logs, errors, documentation, ADRs,
commit messages, issues, and pull requests use English. See
[`CONTRIBUTING.md`](CONTRIBUTING.md) for the workflow and Conventional Commit
policy.

CacheWarden is available under the [MIT License](LICENSE).

