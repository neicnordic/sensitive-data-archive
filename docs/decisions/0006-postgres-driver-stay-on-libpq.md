---
status: proposed
date: "2026-05-06"
decision-makers: [Jonas Hagberg]
consulted: [Karl (author of feature/database_interface PR #2389)]
informed: [SDA team]
---

# Stay on lib/pq for the postgres driver, defer pgx migration

## Context and Problem Statement

Issue [#1957](https://github.com/neicnordic/sensitive-data-archive/issues/1957) (prepared statements) is being delivered as part of [PR #2389](https://github.com/neicnordic/sensitive-data-archive/pull/2389). Karl's database-interface refactor introduces `db.Prepare(query)` at startup and `getPreparedStmt(tx, name)` per call. The prep-stmts adoption itself does not need its own ADR. The implementation lands with the refactor and the bench numbers (see `sda/internal/database/benchmark/BENCH.md`) show the gain.

While the bench infrastructure was up, we used it to answer a separate question: should we replace `github.com/lib/pq` with `github.com/jackc/pgx/v5`? That question does deserve an ADR. The answer is non-obvious. pgx is the actively developed driver and many Go projects assume it is faster.

## Decision Drivers

* Performance on hot paths (the same five queries we benched for the prep-stmts question).
* Migration cost and risk. A driver swap touches every error-handling and array-binding site.
* Compatibility with `database/sql`. Karl's refactor keeps the standard abstractions, so an answer that requires dropping them is a different decision.
* Long-term maintenance. `lib/pq` is in maintenance mode, `pgx` is actively developed.

## Considered Options

1. Stay on lib/pq. Keep the current driver.
2. Swap to pgx via the `database/sql` shim (`pgx/v5/stdlib`). Minimal port, abstractions preserved.
3. Migrate to the pgx native API (`pgxpool.Pool`). Drop `database/sql`, rewrite the Database interface.

## Decision Outcome

Chosen: Option 1, stay on lib/pq.

Evidence from the same bench package (`sda/internal/database/benchmark/`) re-run after swapping the driver, n=10 per variant, p=0.000 on every delta, measured against Karl SHA `1424461f` on a 12-core i7-1365U, 32 GB RAM, Ubuntu 24.04, Docker Engine 29.4.2 CE. The exact percentages below come from a single run on a single day; later re-runs on the same hardware swung the absolute timings by roughly 3× (Docker daemon state and system load dominate the per-call ms cost on localhost). The DIRECTION of the regression — pgx slower than lib/pq+prep — has been stable across re-runs; the magnitudes have not.

**Variant 02 — pgx/v5/stdlib (drop-in `database/sql` shim), all 5 targets:**

```
                              01b lib/pq+prep   02 pgx/stdlib    delta ns/op
UpdateFileEventLog-12         1.693m  ± 3%      3.750m  ± 13%    +121.47%
StoreHeader-12                1.694m  ± 9%      3.556m  ± 15%    +109.92%
GetFileStatus-12              65.83µ  ± 18%     1059.79µ ± 20%   +1509.95%
GetHeader-12                  97.90µ  ± 92%     944.92µ ± 16%    +865.24%
GetArchivePathAndLocation-12  61.04µ  ± 24%     957.52µ ± 19%    +1468.64%
geomean                       257.3µ            1.665m           +546.95%
```

**Variant 03 — `pgxpool.Pool` native, no `database/sql`, 2 targets only:**

```
                              01b lib/pq+prep   03 pgx-native    delta ns/op
UpdateFileEventLog-12         1.693m  ± 3%      3.473m  ± 21%    +105%
GetFileStatus-12              65.83µ  ± 18%     929.6µ  ± 11%    +1312%
```

pgx does win on allocations. Variant 03 (`pgxpool.Pool`) uses 4 vs 13 allocs on `UpdateFileEventLog` and 8 vs 18 on `GetFileStatus` (about half), with 32 to 58 percent fewer bytes per op. Allocation counts are deterministic per code path and have been stable across re-runs. The wall-clock regression is real but the exact factor is not stable; the smaller wall-clock difference between lib/pq+prep and 01a noprep on subsequent runs suggests the same is true here.

### Consequences

* Good, because the dependency stays put. No ecosystem churn driven by microbench numbers alone.
* Good, because the `lib/pq` API surface we use is small (`pq.Error` for SQLSTATE 23503, `pq.Array` for `text[]`) and stable.
* Bad, because `lib/pq` is no longer actively developed. If Postgres adds wire-protocol features we want, we will have to migrate or fork.
* Neutral, because the revisit triggers below are real but conditional. If production GC pressure becomes a signal, or we need a pgx-only feature, we have a documented baseline to compare against.

### Confirmation

The bench infrastructure stays in `sda/internal/database/benchmark/`, drift-checked against Karl's SQL and re-runnable on demand. If the conclusion needs to be revisited, run the same suite on a pgx-port branch and compare to the lib/pq baseline.

The pgx-port spike is preserved on the local-only branch `spike/pgx-on-karl` (worktree at `~/src/sda-worktrees/pgx-on-karl`) so a future migration evaluation has somewhere to start.

## More Information

### When to revisit

* Production observability shows GC pressure tied to the database client path.
* We need a pgx-only feature: `pgx.Batch` (multi-statement single roundtrip), `CopyFrom` (bulk ingest), LISTEN/NOTIFY, or native array scanning into `[]string`.
* A `lib/pq` security or correctness issue makes maintenance cost untenable.
* Postgres releases a wire-protocol feature only `pgx` exposes.

### Related artifacts

* `sda/internal/database/benchmark/BENCH.md` (methodology, reproduction steps, prep-stmt numbers)
* Spike branch (local only): `spike/pgx-on-karl`, worktree at `~/src/sda-worktrees/pgx-on-karl`. Variant 02 at SHA `9bfd7c03`, variant 03 on a follow-up commit. Includes the full pgx-port diff (`pq.Error → pgconn.PgError`, `pq.Array → pgx native binding`) and a `benchmark_native/` package using `pgxpool.Pool`. The branch is the starting point if anyone re-evaluates pgx later.
