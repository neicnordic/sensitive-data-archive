#!/bin/bash
set -e

YQ_VERSION=v4.20.1
YQ_BINARY=yq_linux_amd64
C4GH_VERSION=1.4.0
# Workaround for some MacOS installations
#export PATH=$PATH:/home/ubuntu/.local/bin

if [ ! -d LocalEGA-helm ]; then
  git clone https://github.com/nbisweden/LocalEGA-helm
fi

# install s3cmd
pip install s3cmd


# install yq for creating secrets
sudo wget "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/${YQ_BINARY}" -O /usr/bin/yq &&\
    sudo chmod +x /usr/bin/yq

# install crypt4gh
curl -L https://github.com/elixir-oslo/crypt4gh/releases/download/v"${C4GH_VERSION}"/crypt4gh_linux_x86_64.tar.gz | sudo tar -xz -C /usr/bin/ &&\
  sudo chmod +x /usr/bin/crypt4gh
