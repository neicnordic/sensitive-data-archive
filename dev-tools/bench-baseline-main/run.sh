#!/usr/bin/env bash
#
# Bench the variant 00 baseline (pre-Karl SDAdb on origin/main) by creating
# an isolated worktree at a pinned SHA, dropping the *.go.tmpl files into
# sda/internal/database/benchmark/, and running `go test -bench`. Cleans up
# after itself.
#
# Env vars:
#   BENCH_BASELINE_SHA   git ref to checkout. Default is pinned to a pre-Karl
#                        commit (see BASELINE_SHA_DEFAULT below). Override only
#                        if you know what you're doing.
#   BENCH_COUNT          go test -count value (default: 10)
#   BENCH_OUT            file to tee bench stdout to (default: /tmp/sda-bench-00.txt)

set -euo pipefail

# Pinned to origin/main as of 2026-05-11, the most recent commit confirmed to
# still ship the SDAdb wrapper. Bumping this is safe as long as the new SHA
# still pre-dates Karl's database-interface refactor (#2389) merging into main
# — the sanity check below verifies that.
BASELINE_SHA_DEFAULT="52046dfb6096c6374970c9c0b4e9224df3614bab"

BENCH_BASELINE_SHA="${BENCH_BASELINE_SHA:-$BASELINE_SHA_DEFAULT}"
BENCH_COUNT="${BENCH_COUNT:-10}"
BENCH_OUT="${BENCH_OUT:-/tmp/sda-bench-00.txt}"

REPO_ROOT="$(git rev-parse --show-toplevel)"
TMPL_DIR="$REPO_ROOT/dev-tools/bench-baseline-main"
WORKTREE_DIR="$(mktemp -d -t bench-baseline-main.XXXXXX)"

cleanup() {
	git -C "$REPO_ROOT" worktree remove --force "$WORKTREE_DIR" >/dev/null 2>&1 || true
	rm -rf "$WORKTREE_DIR"
}
trap cleanup EXIT

# Refuse to run against a tree that has already absorbed Karl's refactor.
# After #2389 merges, the SDAdb type is gone from origin/main and the
# templates will not compile. Surface that early with a clear message.
if ! git -C "$REPO_ROOT" show "$BENCH_BASELINE_SHA:sda/internal/database/database.go" 2>/dev/null | grep -q '^type SDAdb struct'; then
	cat >&2 <<EOF
ERROR: SDAdb type not found in sda/internal/database/database.go at $BENCH_BASELINE_SHA.

This usually means #2389 has merged into main and the chosen SHA is post-merge.
Pin BENCH_BASELINE_SHA to a pre-merge commit, for example:
    BENCH_BASELINE_SHA=$BASELINE_SHA_DEFAULT $0
EOF
	exit 1
fi

echo ">>> creating worktree at $BENCH_BASELINE_SHA in $WORKTREE_DIR"
git -C "$REPO_ROOT" worktree add --detach "$WORKTREE_DIR" "$BENCH_BASELINE_SHA" >/dev/null

DEST="$WORKTREE_DIR/sda/internal/database/benchmark"
mkdir -p "$DEST"
for f in setup_test.go.tmpl fixtures_test.go.tmpl bench_test.go.tmpl; do
	cp "$TMPL_DIR/$f" "$DEST/${f%.tmpl}"
done
echo ">>> dropped baseline bench package into $DEST"

cd "$WORKTREE_DIR/sda"
echo ">>> running bench (count=$BENCH_COUNT, writing to $BENCH_OUT)"
go test -bench=. -run='^$' -benchmem -count="$BENCH_COUNT" \
	./internal/database/benchmark/... 2>&1 | tee "$BENCH_OUT"

echo ">>> baseline output: $BENCH_OUT"
