# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Reviewers of the author's work: hiring managers and engineers who click the live-demo link in the README, have about two minutes, and need to see Raft consensus actually work (an election after a leader dies, a partition that does not split the brain) without reading the code or docs first.

## Product Purpose

RaftKV is a fault-tolerant key-value store built on Raft. The dashboard runs a real three-node cluster in one process and lets a visitor break it (kill, restart, partition, heal nodes), write and read keys through it, and load-test it, then watch the cluster recover. Success: within one viewport a visitor kills the leader and sees a new one elected, with the failover time, without scrolling.

## Positioning

Not a simulation or animation of Raft: every node runs the real implementation with a write-ahead log, Pre-Vote, linearizable reads and idempotent retries, and the dashboard is a thin client over its HTTP control API. Its failure behaviour is the product.

## Operating Context

- Served by the Go binary from `cluster/web` via `go:embed`; deployed on Render (`render.yaml`) and Docker.
- Control API: `GET /api/cluster` (polled), `POST /api/nodes/{id}/{kill|start|isolate|heal}`, `GET|PUT|DELETE /api/kv/{key}` (with `?consistency=stale`), `POST /api/bench?ops&clients&reads`.
- Visitors use desktop browsers mostly, phones occasionally from a shared link.

## Capabilities and Constraints

- Go module stays dependency-free; the dashboard is now a Vite + React + strict TypeScript + Tailwind build whose output lands in `cluster/web` so `go:embed` keeps working.
- Cluster is three nodes (`n1`–`n3`) today; the UI must not hard-code three.
- Writes stall (do not return wrong data) when a majority is down; the UI must show that as correct behaviour, not as a bug.

## Evidence on Hand

- Real measurements come from the API only: failover ms, commit indexes, benchmark ops/s and p50/p99. Never invent numbers.
- `docs/dashboard.png` is the old dashboard screenshot.

## Product Principles

1. Cause and effect on one screen: an action and its consequence are visible together.
2. Show divergence, not totals: what is different between nodes is the story.
3. Failure is a feature: down, partitioned and stalled states are first-class, never error styling.
4. Real numbers only.

## Accessibility & Inclusion

WCAG 2.2 AA: 4.5:1 text contrast, state never conveyed by colour alone, live regions for role changes, full keyboard operation, reduced-motion respected.
