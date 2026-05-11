# Baseline bench harness (variant 00, pre-Karl `SDAdb`)

This directory holds bench code that compiles against `main`'s old `SDAdb`
wrapper (no `context.Context`, has the per-call `Ping` in
`checkAndReconnectIfNeeded`, has the retry loops). It lives here as `.go.tmpl`
files because the same code cannot compile against `feature/database_interface`
(PR #2389) — `SDAdb` is replaced there by Karl's `Database` interface.

The bench function names match those in `sda/internal/database/benchmark/`
so `benchstat` can compare the runs across versions of the codebase.

## How it runs

`run.sh` creates a throwaway git worktree at a chosen SHA, copies the three
`.tmpl` files into the worktree's `sda/internal/database/benchmark/` as `.go`,
runs `go test -bench`, and cleans up. From the repo root:

```bash
make bench-baseline            # default: origin/main, count=10, tee /tmp/sda-bench-00.txt
make bench-all                 # baseline + current (prep + noprep) + benchstat across all three
```

Env vars (all optional):

| Var | Default | Meaning |
|---|---|---|
| `BENCH_BASELINE_SHA` | `52046dfb` (pinned, see below) | git ref to bench against |
| `BENCH_COUNT` | `10` | `go test -count` |
| `BENCH_OUT` | `/tmp/sda-bench-00.txt` | where to tee stdout |

## Why the SHA is pinned, not `origin/main`

`origin/main` moves. The day Karl's PR #2389 merges, `origin/main` no longer
contains the `SDAdb` API and the templates here cannot compile against it.
The default is therefore pinned to `52046dfb`, which was `origin/main` on
2026-05-11 (a known pre-Karl SHA with `SDAdb` intact). `run.sh` also has a
sanity check: if it cannot find `type SDAdb struct` at the resolved SHA it
exits early with a clear message instead of producing confusing compile
errors.

To intentionally bench against a different pre-Karl commit:

```bash
BENCH_BASELINE_SHA=<sha> make bench-baseline
```

To bump the pinned default after a non-Karl main bump, just edit
`BASELINE_SHA_DEFAULT` at the top of `run.sh` — the sanity check will refuse
the bump if the new SHA has lost `SDAdb`.

## Why this exists

`feature/prepared-statements-1957` measures prep-vs-noprep on Karl's branch in
isolation. That answers #1957 ("does adopting prepared statements help?"). It
does **not** answer "how much faster is Karl's PR vs today's main?" because
the two API surfaces are incompatible. The variant 00 baseline is the missing
data point. Keeping the template + script here means anyone can reproduce all
three variants from this one branch.
