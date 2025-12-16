.PHONY: dev clean build build-images run-proxy run-target test docker-build-proxy docker-build-target docker-build-init docker-build-python deploy load-images undeploy kind-create kind-delete

# Docker build targets
docker-build-proxy:
	podman build -t auth-proxy:latest .

docker-build-target:
	cd target && podman build -t auth-target:latest -f Dockerfile .

docker-build-init:
	podman build -f Dockerfile.init -t proxy-init:latest .

docker-build-go-processor:
	podman build -f go-processor/Dockerfile -t go-processor:latest .

docker-build-envoy:
	podman build -f Dockerfile.envoy -t envoy-with-processor:latest .

# Build all Docker images
build-images: docker-build-proxy docker-build-target docker-build-init docker-build-envoy

# Load Docker images into kind cluster
load-images:
	kind load docker-image auth-proxy:latest --name agent-platform
	kind load docker-image auth-target:latest --name agent-platform
	kind load docker-image proxy-init:latest --name agent-platform
	kind load docker-image envoy-with-processor:latest --name agent-platform

# Deploy to Kubernetes
deploy:
	kubectl apply -f k8s/auth-target-deployment.yaml
	kubectl apply -f k8s/auth-proxy-deployment.yaml

# Remove deployment from Kubernetes
undeploy:
	kubectl delete -f k8s/auth-proxy-deployment.yaml --ignore-not-found=true
	kubectl delete -f k8s/auth-target-deployment.yaml --ignore-not-found=true

# Kind cluster management
kind-create:
	kind create cluster --name agent-platform

kind-delete:
	kind delete cluster --name agent-platform

# Test the application
test:
	@echo "Testing valid authorization..."
	@curl -s -H "Authorization: kagenti" http://localhost:8080/test
	@echo ""
	@echo "Testing invalid authorization..."
	@curl -s -H "Authorization: invalid" http://localhost:8080/test
	@echo ""
	@echo "Testing no authorization..."
	@curl -s http://localhost:8080/test
	@echo ""
