#!/bin/bash
set -ex

k8s="$(curl -L -s https://dl.k8s.io/release/stable.txt)"

curl -s -L https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | sudo bash

if [ -n "$1" ]; then
    k8s=$(k3d version list k3s | grep "$1" | head -n 1 | cut -d '-' -f 1)
fi

curl -sLO https://dl.k8s.io/release/"$k8s"/bin/linux/amd64/kubectl
chmod +x ./kubectl
sudo mv ./kubectl /usr/local/bin/kubectl

k3d cluster create sda --image=rancher/k3s:"$k8s"-k3s1 --wait --timeout 10m
k3d kubeconfig merge sda --kubeconfig-switch-context
mkdir -p ~/.kube/ && cp ~/.config/kubeconfig-sda.yaml ~/.kube/config
