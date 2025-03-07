#!/bin/bash
set -euo pipefail

echo "Creating Kind cluster using config file..."
kind create cluster --config ./kafka/k8s/config.yaml --name kafka-cluster

echo "Waiting for Kind cluster nodes to be ready..."
kubectl get nodes

echo "Creating namespace 'kafka'..."
kubectl create namespace kafka

echo "Deploying Kafka resources..."
kubectl apply -f ./kafka/k8s/kafka.yaml -n kafka

echo "Waiting for Kafka StatefulSet rollout to complete..."
kubectl rollout status statefulset/kafka -n kafka --timeout=300s

echo "Waiting for Kafka broker pod(s) to be ready..."
kubectl wait --for=condition=ready pod -l app=kafka-app -n kafka --timeout=300s

echo "Kafka is successfully deployed!"
echo "Kafka bootstrap address: host.docker.internal:30092"