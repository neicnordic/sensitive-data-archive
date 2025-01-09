#!/bin/bash
set -e

C4GH_VERSION="$(curl --retry 100 -sL https://api.github.com/repos/neicnordic/crypt4gh/releases/latest | jq -r '.name')"
curl --retry 100 -sL https://github.com/neicnordic/crypt4gh/releases/download/"${C4GH_VERSION}"/crypt4gh_linux_x86_64.tar.gz | sudo tar -xz -C /usr/bin/ &&
        sudo chmod +x /usr/bin/crypt4gh

YQ_VERSION="v4.20.1"
sudo curl --retry 100 -sL "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64" -o /usr/bin/yq &&
        sudo chmod +x /usr/bin/yq

sudo apt install -y jq s3cmd