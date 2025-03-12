#!/bin/bash

# Load environment variables from .env file
if [ -f .env ]; then
  export $(cat .env | grep -v '#' | awk '/=/ {print $1}')
else
  echo ".env file not found. Please create one with your DOCKER_USERNAME set."
  exit 1
fi
# Check if DOCKER_USERNAME is set
if [ -z "$DOCKER_USERNAME" ]; then
  echo "DOCKER_USERNAME is not set in the .env file"
  exit 1
fi

export NUM_CLUSTERS=3

for ((i=1; i<=$NUM_CLUSTERS; i++)); do
  # Create chunk cluster
  kind create cluster --name chunk$i

  # Using your docker username
  sed 's/clairegregg/${DOCKER_USERNAME}/g' chunk_server/k8s/pacman-chunk.yaml | envsubst | kubectl apply -f -
  sed -e "s/{i}/${i}/g" chunk_server/k8s/caddy.yaml | kubectl apply -f -
  sleep 5s
  kubectl wait --for=condition=ready pod -l app=pacman-chunk --timeout=300s
  kubectl wait --for=condition=ready pod -l app=caddy --timeout=300s

  # Port forward the web application
  nohup kubectl port-forward --address 0.0.0.0 svc/caddy 808$i:80 > caddy-${i}-port.log 2>&1 &
  nohup kubectl port-forward --address 0.0.0.0 svc/caddy 443$i:443 > caddy-${i}-https-port.log 2>&1 &
done