# ADR 0002: Make the Interference Workload Opt In

- Status: Accepted
- Date: 2026-07-23

## Context

The aggressor intentionally consumes memory bandwidth so that interference can
be measured. Starting it implicitly would surprise developers and could make a
workstation temporarily less responsive.

## Decision

Place the aggressor behind the Docker Compose `interference` profile. Apply
conservative defaults and explicit CPU and memory limits. Keep the victim
available without activating that profile.

## Consequences

- `docker compose up victim` starts only the measured service.
- An interference experiment requires an explicit profile or a direct
  `make run-aggressor` command.
- Resource limits remain runtime-dependent and must be verified on the host.
- Larger experiments require deliberate configuration changes.

