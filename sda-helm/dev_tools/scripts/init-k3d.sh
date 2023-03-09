#!/bin/bash

if ! command -v k3d > /dev/null
then
  wget -q -O - https://raw.githubusercontent.com/rancher/k3d/main/install.sh | TAG=v5.4.6 bash
fi

k3d cluster list | grep "sda"
cluster_exists=$?

if [ $cluster_exists -ne 0 ]; then
  sudo k3d cluster create sda --image=rancher/k3s:v1.25.6-rc1-k3s1-amd64
  sudo k3d kubeconfig merge sda --kubeconfig-switch-context
  sudo mkdir -p ~/.kube/ && sudo cp /root/.k3d/kubeconfig-sda.yaml ~/.kube/config
  sudo chmod 666 ~/.kube/config
else
  echo "Cluster sda already exists!"
fi
