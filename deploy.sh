#!/bin/bash

# Waverless unified deployment script
# Supports multi-environment, image building, K8s deployment, etc.

set -e

# Default configuration
NAMESPACE="${NAMESPACE:-wavespeed}"
ENVIRONMENT="${ENVIRONMENT:-dev}"
API_KEY="${RUNPOD_AI_API_KEY:-waverless-api-key-2025}"
IMAGE_REGISTRY="${IMAGE_REGISTRY:-docker.io/wavespeed}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
REDIS_PASSWORD="${REDIS_PASSWORD:-}"
API_BACKEND_URL="${API_BACKEND_URL:-http://waverless-svc:80}"

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_blue() {
    echo -e "${BLUE}$1${NC}"
}

# Print configuration information
print_config() {
    log_blue "╔═══════════════════════════════════════════════════════╗"
    log_blue "║        Waverless Deployment Tool                     ║"
    log_blue "╚═══════════════════════════════════════════════════════╝"
    echo ""
    log_info "Configuration:"
    echo "  Namespace:    ${NAMESPACE}"
    echo "  Environment:  ${ENVIRONMENT}"
    echo "  Image:        ${IMAGE_REGISTRY}/waverless:${IMAGE_TAG}"
    echo "  Registry:     ${IMAGE_REGISTRY}"
    echo ""
}

# Create namespace
create_namespace() {
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        log_info "Creating namespace: ${NAMESPACE}"
        kubectl create namespace "$NAMESPACE"
        kubectl label namespace "$NAMESPACE" environment="${ENVIRONMENT}"
    else
        log_info "Namespace ${NAMESPACE} already exists"
    fi
}

# Apply YAML with namespace replacement
apply_yaml() {
    local file="$1"
    local temp_file="/tmp/$(basename "$file")"

    # Replace namespace, image tag, and backend address
    sed -e "s/namespace: wavespeed/namespace: ${NAMESPACE}/g" \
        -e "s|docker.io/wavespeed/waverless:latest|${IMAGE_REGISTRY}/waverless:${IMAGE_TAG}|g" \
        -e "s|docker.io/wavespeed/waverless-web:latest|${IMAGE_REGISTRY}/waverless-web:${IMAGE_TAG}|g" \
        -e "s|value: \"http://waverless-svc:80\"|value: \"${API_BACKEND_URL}\"|g" \
        "$file" > "$temp_file"

    kubectl apply -f "$temp_file"
    rm "$temp_file"
}

# ============================================================
# Build-related commands
# ============================================================

# Build and push API Server image
build_image() {
    log_info "Building API Server Docker image..."
    print_config

    # Call build-and-push.sh script
    IMAGE_REGISTRY=${IMAGE_REGISTRY} IMAGE_TAG=${IMAGE_TAG} ./build-and-push.sh ${IMAGE_TAG} waverless
}

# Build and push Web UI image
build_web_image() {
    log_info "Building Web UI Docker image..."
    print_config

    # Call build-and-push.sh script
    IMAGE_REGISTRY=${IMAGE_REGISTRY} IMAGE_TAG=${IMAGE_TAG} ./build-and-push.sh ${IMAGE_TAG} web-ui
}

# ============================================================
# Deployment-related commands
# ============================================================

# Deploy complete environment
install() {
    log_info "Installing Waverless..."
    print_config

    # Create namespace
    create_namespace

    # Deploy RBAC
    log_info "Deploying RBAC..."
    apply_yaml "k8s/waverless-rbac.yaml"

    # Create ConfigMap
    log_info "Creating ConfigMap..."
    kubectl create configmap waverless-config \
        --from-file=config.yaml="config/config.yaml" \
        --from-file=specs.yaml="config/specs.yaml" \
        --from-file=deployment.yaml="config/templates/deployment.yaml" \
        --namespace="${NAMESPACE}" \
        --dry-run=client -o yaml | kubectl apply -f -

    # Deploy Redis
    log_info "Deploying Redis..."
    apply_yaml "k8s/redis-deployment.yaml"

    # Wait for Redis to be ready
    log_info "Waiting for Redis..."
    kubectl wait --for=condition=ready pod -l app=waverless-redis -n "${NAMESPACE}" --timeout=120s || true

    # Deploy Waverless
    log_info "Deploying Waverless..."
    apply_yaml "k8s/waverless-deployment.yaml"

    # Wait for Waverless to be ready
    log_info "Waiting for Waverless..."
    kubectl wait --for=condition=available --timeout=120s \
        deployment/waverless -n "${NAMESPACE}" || true

    # Deploy Web UI (if YAML file exists)
    if [ -f k8s/waverless-web-deployment.yaml ]; then
        log_info "Deploying Web UI..."
        apply_yaml "k8s/waverless-web-deployment.yaml"

        log_info "Waiting for Web UI..."
        kubectl wait --for=condition=available --timeout=120s \
            deployment/waverless-web -n "${NAMESPACE}" || true
    fi

    echo ""
    log_info "✓ Installation complete!"
    echo ""
    log_blue "Next steps:"
    echo "  1. Check status: $0 status -n ${NAMESPACE}"
    echo "  2. Port forward: kubectl port-forward -n ${NAMESPACE} svc/waverless-svc 8080:80"
    echo "  3. Test API: curl http://localhost:8080/health"
    echo ""
}

# Deploy Web UI separately
install_web() {
    log_info "Installing Web UI..."
    print_config

    # Check if namespace exists
    if ! kubectl get namespace "${NAMESPACE}" &> /dev/null; then
        log_error "Namespace ${NAMESPACE} does not exist. Please run 'install' first."
        exit 1
    fi

    # Check if waverless-svc exists
    if ! kubectl get service waverless-svc -n "${NAMESPACE}" &> /dev/null; then
        log_warn "API Server service (waverless-svc) not found in namespace ${NAMESPACE}"
        log_warn "Web UI may not be able to connect to the backend"
    fi

    # Deploy Web UI
    if [ -f k8s/waverless-web-deployment.yaml ]; then
        log_info "Deploying Web UI..."
        apply_yaml "k8s/waverless-web-deployment.yaml"

        log_info "Waiting for Web UI..."
        kubectl wait --for=condition=available --timeout=120s \
            deployment/waverless-web -n "${NAMESPACE}" || true

        echo ""
        log_info "✓ Web UI installation complete!"
        echo ""
        log_blue "Next steps:"
        echo "  1. Port forward: kubectl port-forward -n ${NAMESPACE} svc/waverless-web-svc 3000:80"
        echo "  2. Open browser: http://localhost:3000"
        echo ""
    else
        log_error "Web UI deployment file not found: k8s/waverless-web-deployment.yaml"
        exit 1
    fi
}

# Upgrade deployment
upgrade() {
    log_info "Upgrading Waverless..."
    print_config

    # Update deployment
    log_info "Updating deployment..."
    apply_yaml "k8s/waverless-deployment.yaml"

    # Wait for rollout
    log_info "Waiting for rollout..."
    kubectl rollout status deployment/waverless -n "${NAMESPACE}"

    log_info "✓ Upgrade complete!"
}

# Uninstall
uninstall() {
    log_warn "This will remove all Waverless resources from namespace ${NAMESPACE}"
    read -p "Are you sure? (yes/no): " -r
    echo
    if [[ ! $REPLY =~ ^[Yy]es$ ]]; then
        echo "Cancelled."
        exit 0
    fi

    log_info "Removing Waverless..."
    kubectl delete deployment waverless -n "${NAMESPACE}" --ignore-not-found=true
    kubectl delete service waverless-svc -n "${NAMESPACE}" --ignore-not-found=true
    kubectl delete configmap waverless-config -n "${NAMESPACE}" --ignore-not-found=true

    log_info "Removing Web UI..."
    kubectl delete deployment waverless-web -n "${NAMESPACE}" --ignore-not-found=true
    kubectl delete service waverless-web-svc -n "${NAMESPACE}" --ignore-not-found=true

    log_info "Removing Redis..."
    kubectl delete deployment waverless-redis -n "${NAMESPACE}" --ignore-not-found=true
    kubectl delete service waverless-redis-svc -n "${NAMESPACE}" --ignore-not-found=true

    log_info "Removing Workers..."
    kubectl delete deployment -n "${NAMESPACE}" -l managed-by=waverless --ignore-not-found=true
    kubectl delete service -n "${NAMESPACE}" -l managed-by=waverless --ignore-not-found=true

    log_info "Removing RBAC..."
    kubectl delete rolebinding waverless-manager-binding -n "${NAMESPACE}" --ignore-not-found=true
    kubectl delete role waverless-manager -n "${NAMESPACE}" --ignore-not-found=true
    kubectl delete serviceaccount waverless -n "${NAMESPACE}" --ignore-not-found=true

    log_info "✓ Uninstall complete!"
    echo ""
    log_warn "Note: Namespace ${NAMESPACE} was not deleted"
    echo "To delete the namespace: kubectl delete namespace ${NAMESPACE}"
    echo ""
}

# Deploy Worker
deploy_worker() {
    local worker_name=$1

    if [ -z "$worker_name" ]; then
        log_error "Please specify worker name"
        log_info "Available workers:"
        ls k8s/*-waverless.yaml k8s/flux-*.yaml 2>/dev/null | xargs -n1 basename | sed 's/.yaml$//'
        exit 1
    fi

    local worker_file="k8s/${worker_name}.yaml"

    if [ ! -f "$worker_file" ]; then
        log_error "Worker config file not found: $worker_file"
        exit 1
    fi

    log_info "Deploying Worker: $worker_name"
    apply_yaml "$worker_file"

    log_info "Waiting for Worker..."
    kubectl wait --for=condition=ready pod -l app=${worker_name} -n ${NAMESPACE} --timeout=300s || true

    log_info "✓ Worker deployed!"
}

# ============================================================
# Management-related commands
# ============================================================

# Restart service
restart_service() {
    log_info "Restarting Waverless..."
    kubectl rollout restart deployment/waverless -n ${NAMESPACE}
    kubectl rollout status deployment/waverless -n ${NAMESPACE}
    log_info "✓ Restart complete!"
}

# Show status
show_status() {
    print_config

    log_blue "Deployment Status:"
    echo ""

    # Check namespace
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        log_error "Namespace ${NAMESPACE} does not exist"
        exit 1
    fi

    # Deployments
    log_warn "Deployments:"
    kubectl get deployments -n "${NAMESPACE}" -o wide
    echo ""

    # Services
    log_warn "Services:"
    kubectl get services -n "${NAMESPACE}"
    echo ""

    # Pods
    log_warn "Pods:"
    kubectl get pods -n "${NAMESPACE}" -o wide
    echo ""

    # ConfigMaps
    log_warn "ConfigMaps:"
    kubectl get configmaps -n "${NAMESPACE}"
    echo ""
}

# View logs
view_logs() {
    local component=${1:-waverless}
    local lines=${2:-100}

    log_info "Viewing ${component} logs (last ${lines} lines)..."
    kubectl logs -n ${NAMESPACE} deployment/${component} --tail=${lines} -f
}

# Test service
test_service() {
    log_info "Testing Waverless service..."

    # Port forward
    log_info "Starting port-forward..."
    kubectl port-forward -n ${NAMESPACE} svc/waverless-svc 8080:80 > /dev/null 2>&1 &
    PF_PID=$!
    sleep 3

    # Health check
    log_info "Health check..."
    if curl -s http://localhost:8080/health | grep -q "ok"; then
        log_info "✓ Health check passed"
    else
        log_error "✗ Health check failed"
    fi

    # List workers
    log_info "Listing workers..."
    curl -s http://localhost:8080/v1/workers | jq . || echo "No workers online"

    # Cleanup
    kill $PF_PID 2>/dev/null || true

    log_info "✓ Test complete!"
}

# ============================================================
# Help information
# ============================================================

show_help() {
    cat << EOF
Waverless Deployment Tool

Usage: $0 [OPTIONS] COMMAND [ARGS]

Commands:
  build                      Build and push Waverless API Docker image
  build-web                  Build and push Web UI Docker image
  install                    Install Waverless (Redis + API + Web UI)
  install-web                Install Web UI only (requires existing API)
  upgrade                    Upgrade existing deployment
  uninstall                  Remove Waverless deployment
  deploy-worker <name>       Deploy a worker
  restart                    Restart Waverless deployment
  status                     Show deployment status
  logs [component] [lines]   View logs (default: waverless, 100 lines)
  test                       Test service health
  help                       Show this help message

Options:
  -n, --namespace NAMESPACE    Kubernetes namespace (default: wavespeed)
  -e, --environment ENV        Environment: dev, test, prod (default: dev)
  -t, --tag TAG               Image tag (default: latest)
  -r, --registry REGISTRY     Image registry (default: docker.io/wavespeed)
  -k, --api-key KEY           API key for worker authentication
  -b, --backend-url URL       Backend API URL for Web UI (default: http://waverless-svc:80)
  --redis-password PASSWORD    Redis password (optional)

Environment Variables:
  NAMESPACE         - Same as --namespace
  ENVIRONMENT       - Same as --environment
  IMAGE_TAG         - Same as --tag
  IMAGE_REGISTRY    - Same as --registry
  RUNPOD_AI_API_KEY - Same as --api-key
  API_BACKEND_URL   - Same as --backend-url
  REDIS_PASSWORD    - Same as --redis-password

Examples:
  # Build images
  $0 build              # Build Waverless API image
  $0 build-web          # Build Web UI image

  # Deploy to development
  $0 install

  # Deploy to test environment
  $0 -n wavespeed-test -e test -k "test-api-key" install

  # Deploy to production with specific version
  $0 -n wavespeed-prod -e prod -t v1.0.0 -k "prod-key" install

  # Deploy with custom backend URL
  $0 -n wavespeed-prod -b https://api.example.com install

  # Deploy Web UI only (if API already exists)
  $0 -n wavespeed-test -b http://waverless-svc:80 install-web

  # Deploy a worker
  $0 -n wavespeed-test deploy-worker flux-dev-lora-trainer

  # Check status
  $0 -n wavespeed-test status

  # View logs
  $0 logs waverless 50

  # Upgrade production
  $0 -n wavespeed-prod -t v1.0.1 upgrade

  # Clean up
  $0 -n wavespeed-test uninstall

More information: README.md and k8s/README.md
EOF
}

# ============================================================
# Main function
# ============================================================

main() {
    local command=""

    # Parse parameters
    while [[ $# -gt 0 ]]; do
        case $1 in
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            -e|--environment)
                ENVIRONMENT="$2"
                shift 2
                ;;
            -t|--tag)
                IMAGE_TAG="$2"
                shift 2
                ;;
            -r|--registry)
                IMAGE_REGISTRY="$2"
                shift 2
                ;;
            -k|--api-key)
                API_KEY="$2"
                shift 2
                ;;
            --redis-password)
                REDIS_PASSWORD="$2"
                shift 2
                ;;
            -b|--backend-url)
                API_BACKEND_URL="$2"
                shift 2
                ;;
            build|build-web|install|install-web|upgrade|uninstall|deploy-worker|restart|status|logs|test|help)
                command="$1"
                shift
                break
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done

    # Execute command
    case "$command" in
        build)
            build_image
            ;;
        build-web)
            build_web_image
            ;;
        install)
            install
            ;;
        install-web)
            install_web
            ;;
        upgrade)
            upgrade
            ;;
        uninstall)
            uninstall
            ;;
        deploy-worker)
            deploy_worker "$@"
            ;;
        restart)
            restart_service
            ;;
        status)
            show_status
            ;;
        logs)
            view_logs "$@"
            ;;
        test)
            test_service
            ;;
        help|"")
            show_help
            ;;
        *)
            log_error "Unknown command: $command"
            show_help
            exit 1
            ;;
    esac
}

main "$@"
