# Database benchmark package

Microbenchmarks for the database hot path. Used to support [#1957](https://github.com/neicnordic/sensitive-data-archive/issues/1957) (prepared statements) and the postgres-driver question (see `docs/decisions/0006-postgres-driver-stay-on-libpq.md`).

The package lives next to `sda/internal/database/postgres/` so it can copy SQL verbatim from the production methods and reuse the dockertest fixture pattern from `db_test.go`.

## What it measures

Five queries that run on every file lifecycle event in ingest and download. All single-statement, no transaction.

| Bench target                       | Type   | Source                                              |
|------------------------------------|--------|-----------------------------------------------------|
| `BenchmarkUpdateFileEventLog`      | INSERT | `method_update_file_event_log.go`                   |
| `BenchmarkGetFileStatus`           | SELECT | `method_get_file_status.go`                         |
| `BenchmarkStoreHeader`             | UPDATE | `method_store_header.go`                            |
| `BenchmarkGetHeader`               | SELECT | `method_get_header.go`                              |
| `BenchmarkGetArchivePathAndLocation` | SELECT | `method_get_archive_path_and_location.go`         |

The SQL strings are copied verbatim into `queries_test.go`. A drift check (`make bench-verify`) fails the test suite if the strings stop matching upstream.

## Two adapter modes, and why

The bench package wraps every query in a small adapter (`adapters_test.go`) with two modes, picked by the `BENCH_ADAPTER` env var:

| Mode      | Per-iteration call            | Models                                                                |
|-----------|-------------------------------|-----------------------------------------------------------------------|
| `prep`    | `stmt.ExecContext(ctx, args)` | Production code path (`getPreparedStmt(tx, name)` returns a cached `*sql.Stmt`) |
| `noprep`  | `db.ExecContext(ctx, sql, args)` | Counterfactual: same SQL, no prepared-statement cache                 |

`prep` is what ships. `noprep` is a measurement trick. There is no production code path that runs without prepared statements; we only run in noprep mode so we can subtract it from prep and read off the isolated contribution. That subtraction is the answer to #1957.

## What this bench measures

The package in `sda/internal/database/benchmark/` is scoped to one question: **does adding prepared statements on top of Karl's database refactor (#2389) help, and by how much?** Answered by toggling `BENCH_ADAPTER=prep` vs `noprep` and comparing the same SQL through `db.PrepareContext` + `stmt.ExecContext` against raw `db.ExecContext`. Same SQL, same fixture, same Postgres, only the prep cache changes.

That alone does not tell you how much faster Karl's PR is vs today's `main`. For the full picture there is a separate harness at `dev-tools/bench-baseline-main/` that runs variant 00 (pre-Karl `SDAdb`) against `origin/main` in a throwaway worktree. The two API surfaces are incompatible, so they cannot live in the same Go module, but `make bench-all` glues them together and produces all three benchstat comparisons in one go.

### Two packages in this PR

```
sda/internal/database/benchmark/         # adapter-based, uses copied SQL,
                                         # toggles prep vs noprep
sda/internal/database/benchmark_wrapper/ # calls pgDb's public methods directly
                                         # via the Database interface
```

`benchmark/` is the primary suite. `benchmark_wrapper/` ran once, to confirm the adapter is a faithful model of `pgDb`'s prep-stmt path. The wrapper-vs-adapter deltas were within CI on writes (StoreHeader was even marginally faster through Karl's wrapper at -12%) and unmeasurable on reads, where both sides had ±60 to 92 percent variance at µs scale. Result: the adapter is the right surface for the prep-vs-noprep comparison and the wrapper overhead is not a confounder. The package is left in the tree so the validation can be re-run on demand.

## Reproducing the numbers

From repo root:

```bash
make bench-current     # 01a + 01b on this branch, benchstats the prep-stmt delta
make bench-baseline    # variant 00 only: pre-Karl SDAdb via throwaway worktree
make bench-all         # all three: baseline + current + 3-way benchstat
```

`bench-baseline` defaults to `origin/main`; override with `BENCH_BASELINE_SHA=<sha>` (needed once Karl's PR merges, since the `SDAdb` API disappears from main). `BENCH_COUNT` (default 10) controls iterations. See `dev-tools/bench-baseline-main/README.md` for details.

Under the hood `make bench-current` is just:

```bash
cd sda && BENCH_ADAPTER=prep   go test -bench=. -run='^$' -benchmem -count=10 ./internal/database/benchmark/... | tee /tmp/sda-bench-01b.txt
cd sda && BENCH_ADAPTER=noprep go test -bench=. -run='^$' -benchmem -count=10 ./internal/database/benchmark/... | tee /tmp/sda-bench-01a.txt
benchstat /tmp/sda-bench-01a.txt /tmp/sda-bench-01b.txt
```

Concrete medians from the run on Karl SHA `5b09a151` (this hardware: 12-core i7-1365U, 32 GB RAM, Ubuntu 24.04, Docker Engine 29.4.2 CE, dockertest fixture against `postgres:15.2-alpine3.17`):

```
                              01a noprep      01b prep        delta
UpdateFileEventLog-12         1.614m  ± 2%    1.497m  ± 2%    -7.27%   (write, commit dominates)
StoreHeader-12                1.602m  ± 4%    1.515m  ± 3%    -5.42%   (write)
GetFileStatus-12              137.51µ ± 19%   59.12µ  ± 22%   -57.01%  (read, parse+plan skipped)
GetHeader-12                  120.68µ ± 9%    57.30µ  ± 35%   -52.52%
GetArchivePathAndLocation-12  131.67µ ± 48%   50.62µ  ± 39%   -61.56%
geomean                       355.1µ          207.9µ          -41.45%
```

(All deltas significant, p ≤ 0.003, n=10. The earlier run on Karl SHA `1424461f` showed -47.07% geomean; the slightly smaller delta on `5b09a151` is because Karl's noprep path also got faster in his rebase.)

Reads win bigger than writes. Each prepared SELECT skips Parse, Analyze, and Plan server-side; the savings are visible because the query is cheap enough that planning was a meaningful fraction of the total cost. Writes still pay WAL and commit, so the prep-stmt slice is smaller as a percentage.

## Drift check

```bash
make bench-verify
```

Greps each SQL string from `queries_test.go` against `sda/internal/database/postgres/method_*.go`. Trailing whitespace per line is stripped before compare so gofmt's whitespace stripping doesn't trigger false drift. Run after rebasing onto a moved upstream. If it fails, copy the new SQL into `queries_test.go` verbatim.

## SHA stability

Two Karl SHAs were pinned during the iterations: `1424461f` (initial pin during WIP) and `5b09a151` (after Karl moved out of WIP). The prep-vs-noprep numbers above are from the `5b09a151` run. Re-running `01a` + `01b` on the older `1424461f` gave a -47.07% geomean (vs the -41.45% on `5b09a151`); B/op and allocs/op were sample-equal across both runs. The signal is robust against the WIP churn.

## Notes for next runner

The bench bypasses `postgres.NewPostgresSQLDatabase` and uses raw `sql.Open` so it can pin `MaxOpenConns=10` and `MaxIdleConns=10` deterministically. Karl's branch now exposes those as options on the public constructor, which is why `benchmark_wrapper/` uses them; `benchmark/` does not need to.

There is a small JSONB asymmetry in variant 00 only. Main's `SDAdb.UpdateFileEventLog(fileUUID, event, user, details, message string)` takes strings, so the baseline bench passes `"null"` (a valid JSON literal) for the `details` and `message` columns. Variants 01 and later pass Go `nil`, which becomes SQL NULL. This affects the `UpdateFileEventLog` row in the 01a-vs-00 and 01b-vs-00 comparisons. It does not affect the prep-vs-noprep signal, since 01a and 01b both pass nil.

Read variance is wide. On the latest run (Karl SHA `5b09a151`) read targets sat between ±9% and ±48% per side; the earlier `1424461f` run hit ±60 to 92% on some targets. Docker Engine loopback latency plus kernel scheduling are the noise floor. The sign of every prep-vs-noprep delta is unambiguous across both runs (p ≤ 0.003, n=10), but absolute numbers will differ on CI hardware.
