#!/bin/bash
set -e

if [ "$OSTYPE" == "linux-gnu" ]; then
  BTYPE="linux"
elif [ "$OSTYPE" == "darwin" ]; then
  BTYPE="darwin"
fi

curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.25.0/bin/"$BTYPE"/amd64/kubectl
chmod +x ./kubectl
sudo mv ./kubectl /usr/local/bin/kubectl

wget https://get.helm.sh/helm-v3.11.0-"$BTYPE"-amd64.tar.gz -O - | tar -xz
sudo cp "$BTYPE"-amd64/helm /usr/local/bin/helm

rm -r ./*-amd64/
