#!/bin/bash
set -e

# shellcheck source=/dev/null
. "$(git rev-parse --show-toplevel)/.github/integration/scripts/tool_versions.sh"
curl --retry 100 -fsSL -o /tmp/crypt4gh.tar.gz \
        "https://github.com/neicnordic/crypt4gh/releases/download/${CRYPT4GH_VERSION}/crypt4gh_linux_x86_64.tar.gz"
sudo tar -xzf /tmp/crypt4gh.tar.gz -C /usr/bin/ crypt4gh
sudo chmod +x /usr/bin/crypt4gh

YQ_VERSION="v4.20.1"
sudo curl --retry 100 -sL "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64" -o /usr/bin/yq &&
        sudo chmod +x /usr/bin/yq

sudo apt install -y jq s3cmd