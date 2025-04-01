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

cd chunk_server/src/chunk_server/

# Build the Docker image
echo "Building Docker image for $DOCKER_USERNAME/pacman-chunk:latest"
docker build -t "$DOCKER_USERNAME/pacman-chunk:latest" .

# Push the Docker image to Docker Hub
echo "Pushing Docker image to Docker Hub"
docker push "$DOCKER_USERNAME/pacman-chunk:latest"
