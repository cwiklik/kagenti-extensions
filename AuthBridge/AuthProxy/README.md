# Auth Proxy

A Go application that provides authentication proxy functionality with two services:

1. **Auth Proxy** (port 8080) - Receives HTTP traffic and checks for "kagenti" authorization
2. **Target Service** (port 8081) - Validates "kagenti_new" authorization and responds accordingly

## Deployment

**Deploy to kind cluster:**
```bash
# Create kind cluster named "agent-platform"
make kind-create

# Build and load Docker images
make build-images
make load-images

# Deploy to Kubernetes
make deploy

# Port forward to access the service
kubectl port-forward svc/auth-proxy-service 8080:8080
```

## Testing the Application

Assume the access token is stored in env var `ACCESS_TOKEN`, run:

**Valid request (will be forwarded):**
```bash
curl -H "Authorization: Bearer $ACCESS_TOKEN" http://localhost:8080/test
# Expected response: "authorized"
```

**Invalid request (will be rejected by proxy):**
```bash
curl -H "Authorization: Bearer $SOME_OTHER_TOKEN" http://localhost:8080/test
# Expected response: "Unauthorized - invalid token"
```

**No authorization header:**
```bash
curl http://localhost:8080/test
# Expected response: "Unauthorized - invalid token"
```

## Kubernetes Testing

When deployed to Kubernetes, you can test the services internally:

**Test target service directly:**
```bash
kubectl run test-pod --image=curlimages/curl --rm -it --restart=Never -- curl -H "Authorization: Bearer $ACCESS_TOKEN" http://auth-target-service:8081/test
```

**View logs:**
```bash
# Auth proxy logs
kubectl logs deployment/auth-proxy

# Target service logs
kubectl logs deployment/auth-target

# Follow logs in real-time
kubectl logs -f deployment/auth-proxy
```

**Check service status:**
```bash
# List pods
kubectl get pods

# List services
kubectl get svc

# Describe deployments
kubectl describe deployment auth-proxy
kubectl describe deployment auth-target
```

## Clean Up

**Remove Kubernetes deployment:**
```bash
make undeploy
```

**Delete kind cluster:**
```bash
make kind-delete
```

## How it works

1. Client sends request to auth proxy (port 8080) with `Authorization: kagenti`
2. Proxy validates the authorization header
3. If valid, proxy forwards the request to target service (port 8081) with `Authorization: kagenti_new`
4. Target service validates the new authorization header and responds with "authorized" or "unauthorized"
5. Proxy returns the target service's response to the client

## Architecture

```
Client → Auth Proxy (token) → Target Service (token) → Response
         Port 8080            Port 8081
```

In Kubernetes:
```
Client → auth-proxy-service:8080 → auth-target-service:8081 → Response
         (NodePort 30080)          (ClusterIP)
```
