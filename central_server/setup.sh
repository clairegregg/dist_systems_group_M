#!/bin/bash

# Load environment variables from .env file
if [ -f .env ]; then
  set -o allexport
  source .env
  set +o allexport
else
  echo ".env file not found."
  exit 1
fi
# Check if DOCKER_USERNAME is set
if [ -z "$DOCKER_USERNAME" ]; then
  echo "DOCKER_USERNAME is not set in the .env file"
  exit 1
fi

# Create central cluster
kind create cluster --name central

# Create and wait for mongodb service
kubectl apply -f central_server/k8s/mongodb.yaml
sleep 5s
kubectl wait --for=condition=ready pod -l app=mongodb --timeout=300s

# Using your docker username
sed 's/clairegregg/${DOCKER_USERNAME}/g' central_server/k8s/pacman-central.yaml | envsubst | kubectl apply -f -
sleep 5s
kubectl wait --for=condition=ready pod -l app=pacman-central --timeout=300s

# Port forward the web application
nohup kubectl port-forward --address 0.0.0.0 svc/pacman-central 8080:80 > central-server-port.log 2>&1 &