#!/bin/bash

# Waverless Docker build and push script
# Usage:
#   ./build-and-push.sh [TAG] [COMPONENT]
#   COMPONENT: waverless (default) | web-ui
#
# Examples:
#   ./build-and-push.sh latest waverless  # Build API server
#   ./build-and-push.sh latest web-ui     # Build Web UI
#
# Or via deploy.sh:
#   ./deploy.sh build                     # Build API server
#   ./deploy.sh build-web                 # Build Web UI

set -e

# Default configuration
IMAGE_REGISTRY="${IMAGE_REGISTRY:-docker.io/wavespeed}"
IMAGE_TAG="${1:-latest}"
COMPONENT="${2:-waverless}"
ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin}"

# Determine image name and build path based on component
if [ "$COMPONENT" = "web-ui" ]; then
    IMAGE_NAME="${IMAGE_REGISTRY}/waverless-web:${IMAGE_TAG}"
    BUILD_CONTEXT="web-ui"
    DOCKERFILE="web-ui/Dockerfile"
    COMPONENT_NAME="Web UI"
    BUILD_ARGS="--build-arg VITE_ADMIN_USERNAME=${ADMIN_USERNAME} --build-arg VITE_ADMIN_PASSWORD=${ADMIN_PASSWORD}"
else
    IMAGE_NAME="${IMAGE_REGISTRY}/waverless:${IMAGE_TAG}"
    BUILD_CONTEXT="."
    DOCKERFILE="Dockerfile"
    COMPONENT_NAME="API Server"
    BUILD_ARGS=""
fi

echo "=================================="
echo "Waverless Docker Build & Push"
echo "=================================="
echo "Component: ${COMPONENT_NAME}"
echo "Image:     ${IMAGE_NAME}"
echo ""

# Check if Dockerfile exists
if [ ! -f "$DOCKERFILE" ]; then
    echo "✗ Dockerfile not found: $DOCKERFILE"
    exit 1
fi

# 1. Build Docker image
echo "Step 1: Building Docker image..."
docker build -f ${DOCKERFILE} -t ${IMAGE_NAME} ${BUILD_ARGS} ${BUILD_CONTEXT}

if [ $? -eq 0 ]; then
    echo "✓ Docker image built successfully"
else
    echo "✗ Failed to build Docker image"
    exit 1
fi

echo ""

# 2. Push to registry
echo "Step 2: Pushing to registry..."
docker push ${IMAGE_NAME}

if [ $? -eq 0 ]; then
    echo "✓ Docker image pushed successfully"
else
    echo "✗ Failed to push Docker image"
    exit 1
fi

echo ""
echo "=================================="
echo "✓ All done!"
echo "Image: ${IMAGE_NAME}"
echo "=================================="
echo ""
