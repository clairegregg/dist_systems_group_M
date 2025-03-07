#!/bin/bash
set -euo pipefail

# Delete the Kafka namespace (removes all resources within the namespace)
echo "Deleting namespace 'kafka'..."
kubectl delete namespace kafka --ignore-not-found

# Delete the Kind cluster named "kafka-cluster"
echo "Deleting Kind cluster 'kafka-cluster'..."
kind delete cluster --name kafka-cluster

echo "Cleanup completed!"