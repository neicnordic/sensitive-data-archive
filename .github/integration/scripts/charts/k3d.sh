#!/bin/bash
set -ex

k8s="$(curl -L -s https://dl.k8s.io/release/stable.txt)"

curl -s -L https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | sudo bash
curl -sLO https://storage.googleapis.com/kubernetes-release/release/"$k8s"/bin/linux/amd64/kubectl
chmod +x ./kubectl
sudo mv ./kubectl /usr/local/bin/kubectl

sudo k3d cluster create sda --image=rancher/k3s:"$k8s"-k3s1 --wait --timeout 10m
sudo k3d kubeconfig merge sda --kubeconfig-switch-context
mkdir -p ~/.kube/ && sudo cp /root/.k3d/kubeconfig-sda.yaml ~/.kube/config
sudo chown $UID:$UID ~/.kube/config && chmod 600 ~/.kube/config
