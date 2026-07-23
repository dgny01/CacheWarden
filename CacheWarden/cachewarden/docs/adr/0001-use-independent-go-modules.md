# ADR 0001: Use Independent Go Modules for Experiment Components

- Status: Accepted
- Date: 2026-07-23

## Context

Milestone 1 contains a measured service, an interference workload, and a load
generator. They have different runtime responsibilities and must be buildable
and testable independently. Later milestones may deploy or version these
components separately.

## Decision

Maintain `victim`, `aggressor`, and `tools/loadgen` as independent Go modules.
Use a root `go.work` file for convenient local development without coupling
their dependency graphs.

## Consequences

- Each component can be built, tested, and containerized independently.
- CI must validate every module explicitly.
- Dependency versions may differ between modules when justified.
- Repository-wide commands require a small amount of Makefile orchestration.

