#!/usr/bin/env bash
# Sprint Review Demo Script — SDA Download API v2
#
# Starts the dev stack automatically if not already running.
# Run from the repository root.
#
# Usage:
#   ./dev-tools/download-v2-dev/demo.sh              # run all steps interactively
#   ./dev-tools/download-v2-dev/demo.sh <step>        # run a single step (1-7)
#   ./dev-tools/download-v2-dev/demo.sh --tmux        # tmux split with service logs
#   ./dev-tools/download-v2-dev/demo.sh --tmux <step> # tmux + single step

set -euo pipefail

BASE="http://localhost:8085"
AUTH="http://localhost:8000"
BOLD='\033[1m'
DIM='\033[2m'
CYAN='\033[1;36m'
GREEN='\033[1;32m'
YELLOW='\033[1;33m'
RED='\033[1;31m'
RESET='\033[0m'
TOKEN=""

# ── helpers ──────────────────────────────────────────────────────────

banner() {
    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
    echo -e "${BOLD}  $1${RESET}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
}

narrate() {
    echo -e "\n${DIM}# $1${RESET}"
}

run() {
    local display_cmd="$1"
    # Show the command with $TOKEN truncated for readability
    if [[ -n "$TOKEN" ]]; then
        local short_token="${TOKEN:0:20}..."
        echo -e "${YELLOW}\$ ${display_cmd//$TOKEN/$short_token}${RESET}"
    else
        echo -e "${YELLOW}\$ $display_cmd${RESET}"
    fi
    eval "$1"
    echo ""
}

pause() {
    echo -e "${DIM}  [press Enter to continue]${RESET}"
    read -r
}

# ── token setup ──────────────────────────────────────────────────────

get_token() {
    TOKEN=$(curl -sf "$AUTH/tokens" | jq -r '.[0]')
    if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
        echo -e "${RED}ERROR: Could not get token from mockauth. Is docker compose running?${RESET}"
        exit 1
    fi
    echo -e "${GREEN}Token acquired from mock OIDC.${RESET}"
}

# ── demo steps ───────────────────────────────────────────────────────

step1() {
    banner "1/7  Health Check"
    narrate "Liveness — is the process up?"
    run "curl -s $BASE/health/live | jq ."

    narrate "Readiness — are all dependencies healthy? (DB, storage, gRPC)"
    run "curl -s $BASE/health/ready | jq ."
    pause
}

step2() {
    banner "2/7  GA4GH Service Info"
    narrate "Public endpoint, no auth required."
    narrate "Follows the GA4GH service-info spec — ID and organization are configurable."
    run "curl -s $BASE/service-info | jq ."
    pause
}

step3() {
    banner "3/7  Authentication & Dataset Listing"
    narrate "Get a JWT from the mock OIDC provider, then list accessible datasets."
    get_token
    echo ""
    run "curl -s -H \"Authorization: Bearer $TOKEN\" $BASE/datasets | jq ."
    pause
}

step4() {
    banner "4/7  Dataset Details & Files"
    narrate "Drill into a dataset and list its files."
    run "curl -s -H \"Authorization: Bearer $TOKEN\" $BASE/datasets/EGAD00000000001 | jq ."

    narrate "Files in the dataset:"
    run "curl -s -H \"Authorization: Bearer $TOKEN\" $BASE/datasets/EGAD00000000001/files | jq ."
    pause
}

step5() {
    banner "5/7  DRS Object Resolution (GA4GH DRS 1.5)"
    narrate "Resolve dataset + file path → DRS object with download URL."
    narrate "This is how htsget-rs and other GA4GH clients discover files."
    run "curl -s -H \"Authorization: Bearer $TOKEN\" $BASE/objects/EGAD00000000001/test-file.c4gh | jq ."
    pause
}

step6() {
    banner "6/7  File Download (Crypt4GH)"
    narrate "Three-tier download: combined, header-only, content-only."
    narrate "Downloads require a Crypt4GH public key in the X-C4GH-Public-Key header."

    # Get c4gh public key from the running stack
    local c4gh_key=""
    local container_id
    container_id=$(docker ps -qf 'label=com.docker.compose.project=download-v2-dev' -f 'label=com.docker.compose.service=reencrypt' | head -1)
    if [[ -n "$container_id" ]]; then
        docker cp "$container_id:/shared/c4gh.pub.pem" /tmp/dev-c4gh.pub.pem 2>/dev/null
        c4gh_key=$(base64 -w0 /tmp/dev-c4gh.pub.pem 2>/dev/null)
    fi
    if [[ -z "$c4gh_key" ]]; then
        narrate "WARNING: Could not get c4gh key from container — download will fail with 400."
        c4gh_key="missing"
    fi

    narrate "HEAD request — file size and ETag without downloading:"
    run "curl -sI -H \"Authorization: Bearer $TOKEN\" -H \"X-C4GH-Public-Key: $c4gh_key\" $BASE/files/EGAF00000000001"

    narrate "Combined download (re-encrypted header + data segments):"
    run "curl -s -o /dev/null -w 'HTTP %{http_code}  Size: %{size_download} bytes\n' -H \"Authorization: Bearer $TOKEN\" -H \"X-C4GH-Public-Key: $c4gh_key\" $BASE/files/EGAF00000000001"

    narrate "Content-only with Range request (first 512 bytes):"
    run "curl -s -o /dev/null -w 'HTTP %{http_code}  Size: %{size_download} bytes\n' -H \"Authorization: Bearer $TOKEN\" -H \"X-C4GH-Public-Key: $c4gh_key\" -H 'Range: bytes=0-511' $BASE/files/EGAF00000000001/content"
    pause
}

step7() {
    banner "7/7  Error Handling (RFC 9457 Problem Details)"
    narrate "No token → 401:"
    run "curl -s $BASE/datasets | jq ."

    narrate "Non-existent resource → 403 (prevents existence leakage):"
    run "curl -s -H \"Authorization: Bearer $TOKEN\" $BASE/datasets/DOES_NOT_EXIST | jq ."
    pause
}

# ── auto-start ──────────────────────────────────────────────────────

ensure_running() {
    if curl -sf "$BASE/health/live" > /dev/null 2>&1; then
        return
    fi
    REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo ".")
    echo -e "${YELLOW}Dev stack not running — starting with make dev-download-v2-up ...${RESET}"
    make -C "$REPO_ROOT" dev-download-v2-up
    echo "Waiting for download service ..."
    for _ in $(seq 1 30); do
        if curl -sf "$BASE/health/live" > /dev/null 2>&1; then
            echo -e "${GREEN}Ready.${RESET}"
            return
        fi
        sleep 2
    done
    echo "ERROR: download service did not become ready in time."
    exit 1
}

# ── tmux mode ───────────────────────────────────────────────────────

launch_tmux() {
    local step="${1:-all}"
    local script
    script=$(readlink -f "$0")
    local session="sda-demo"

    if tmux has-session -t "$session" 2>/dev/null; then
        tmux kill-session -t "$session"
    fi

    local repo_root
    repo_root=$(git rev-parse --show-toplevel 2>/dev/null || echo ".")

    # Start logs pane first so it catches all requests
    tmux new-session -d -s "$session"
    tmux send-keys -t "$session" "PR_NUMBER=\$(date +%F) docker compose -f $repo_root/dev-tools/download-v2-dev/compose.yml logs -f --tail 0 download mockauth" Enter

    # Demo pane on the left
    tmux split-window -h -b -t "$session"
    tmux send-keys -t "$session" "sleep 2 && $script $step" Enter

    tmux attach -t "$session"
}

# ── main ─────────────────────────────────────────────────────────────

main() {
    ensure_running
    banner "SDA Download API v2 — Sprint Review Demo"
    echo ""
    echo -e "  Download API:  ${BOLD}$BASE${RESET}"
    echo -e "  Mock OIDC:     ${BOLD}$AUTH${RESET}"
    echo -e "  Test dataset:  ${BOLD}EGAD00000000001${RESET}"
    echo -e "  Test file:     ${BOLD}EGAF00000000001${RESET}"
    echo ""

    if [[ ${1:-all} == "all" ]]; then
        get_token
        pause
        step1; step2; step3; step4; step5; step6; step7
        banner "Demo Complete!"
    else
        if [[ $1 -ge 3 ]]; then
            get_token
        fi
        "step$1"
    fi
}

# ── entrypoint ──────────────────────────────────────────────────────

if [[ "${1:-}" == "--tmux" ]]; then
    shift
    launch_tmux "${1:-all}"
else
    main "${1:-all}"
fi
