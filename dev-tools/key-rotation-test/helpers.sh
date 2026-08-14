#!/usr/bin/env bash

compute_key_hash() {
    local pubkey_path="$1"

    if [ ! -f "$pubkey_path" ]; then
        echo "Error: Public key file not found: $pubkey_path" >&2
        return 1
    fi

    if command -v xxd >/dev/null 2>&1; then
        awk 'NR==2' "$pubkey_path" | base64 -d | xxd -p -c256 | tr -d '\r\n[:space:]'
    elif command -v hexdump >/dev/null 2>&1; then
        awk 'NR==2' "$pubkey_path" | base64 -d | hexdump -v -e '/1 "%02x"' | tr -d '\r\n[:space:]'
    else
        echo "Error: Neither xxd nor hexdump is installed." >&2
        return 1
    fi
}

pause_step() {
    local msg="${1:-">>> PRESS [ENTER] TO PROCEED TO THE NEXT TEST CASE..."}"
    printf '\n\033[1;33m%b\033[0m\n' "$msg"
    read -r
}

log_header() {
    local header_text="$1"
    printf '\n\033[1;32m========================================================================\033[0m\n'
    printf '\033[1;32m%b\033[0m\n' "$header_text"
    printf '\033[1;32m========================================================================\033[0m\n'
}