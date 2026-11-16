# shellcheck shell=sh
# Versions of the tools the integration tests download. They are pinned so that a
# rate-limited GitHub API or a new upstream release cannot break CI. Bump them by hand.
# shellcheck disable=SC2034 # used by the scripts that source this file
CRYPT4GH_VERSION=v1.16.0
# shellcheck disable=SC2034
GRPCURL_VERSION=1.9.4
