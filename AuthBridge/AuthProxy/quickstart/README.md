# AuthProxy quickstart

This document gives a step-by-step tutorial of getting started with the AuthProxy in a local Kind cluster.

The final architecture deployed is as follows:

```
Caller  ──►  AuthProxy Pod  ──►  Demo App
              (inbound: JWT validation)
              (outbound: token exchange)
```

The AuthProxy pod intercepts traffic in both directions:
- **Inbound**: Validates JWT tokens on incoming requests (returns 401 if invalid)
- **Outbound**: Exchanges tokens for the correct audience before forwarding to the Demo App

The demo goes as follows:
1. Install Kagenti
1. Build and deploy the Demo App and AuthProxy
1. Configure Keycloak
1. Create the auth-proxy-config secret
1. Test the flow

## Step 1: Install Kagenti
First, we recommend to deploy Kagenti to a local Kind cluster with the Ansible installer as service urls used below are derived from that installation. Instructions are available [here](https://github.com/kagenti/kagenti/blob/main/docs/install.md#ansible-based-installer-recommended).

This should start a local Kind cluster named `kagenti`.

The key component is Keycloak which has been deployed to the `keycloak` namespace and exposed as `keycloak-service`.

## Step 2: Build and deploy the Demo App and AuthProxy

Let's clone the assets locally:

```bash
git clone git@github.com:kagenti/kagenti-extensions.git
cd kagent-extensions/AuthBridge/AuthProxy
```

We can use the following `make` commands to build and load the images to the Kind cluster:

```bash
make build-images
make load-images
```

If the above gives error `ERROR: no nodes found...` set the `KIND_CLUSTER_NAME` environment variable to the name of the kind cluster you are using.

Then we can create two deployments in Kubernetes:

```bash
make deploy
```

## Step 3: Configure Keycloak

Port-forward Keycloak to access it locally (in a separate terminal):

```bash
kubectl port-forward service/keycloak-service -n keycloak 8080:8080
```

Now set up a Python environment and run the setup script:

```bash
cd quickstart
python -m venv venv
source venv/bin/activate
pip install --upgrade pip
pip install -r requirements.txt
python setup_keycloak.py
```

The script creates:
- `application-caller` client - for obtaining initial tokens (password grant)
- `authproxy` client - used by the AuthProxy sidecar for token exchange
- `demoapp` client - target audience for token exchange
- `authproxy-aud` scope - adds `authproxy` to token audience
- `demoapp-aud` scope - adds `demoapp` to exchanged token audience
- `test-user` / `password` - demo user for testing

## Step 4: Create the auth-proxy-config Secret

The AuthProxy sidecar needs credentials for token exchange. Get the `authproxy` client secret and create the Kubernetes secret:

```bash
# Get admin token
ADMIN_TOKEN=$(curl -s -X POST "http://keycloak.localtest.me:8080/realms/master/protocol/openid-connect/token" \
  -d "client_id=admin-cli" -d "grant_type=password" -d "username=admin" -d "password=admin" | jq -r '.access_token')

# Get authproxy client secret
AUTHPROXY_SECRET=$(curl -s -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://keycloak.localtest.me:8080/admin/realms/demo/clients?clientId=authproxy" | jq -r '.[0].secret')

# Create the secret
kubectl create secret generic auth-proxy-config \
  --from-literal=TOKEN_URL="http://keycloak-service.keycloak.svc.cluster.local:8080/realms/demo/protocol/openid-connect/token" \
  --from-literal=ISSUER="http://keycloak.localtest.me:8080/realms/demo" \
  --from-literal=EXPECTED_AUDIENCE="authproxy" \
  --from-literal=CLIENT_ID="authproxy" \
  --from-literal=CLIENT_SECRET="$AUTHPROXY_SECRET" \
  --from-literal=TARGET_AUDIENCE="demoapp" \
  --from-literal=TARGET_SCOPES="openid demoapp-aud"
```

Then restart the auth-proxy deployment to pick up the secret:

```bash
kubectl rollout restart deployment auth-proxy
kubectl rollout status deployment auth-proxy --timeout=120s
```

## Step 5: Test the Flow

Port-forward the AuthProxy service. Use port 9080 since Keycloak is already using 8080:

```bash
kubectl port-forward svc/auth-proxy-service 9080:8080
```

Wait for the ext proc to initialize (it takes up to 60 seconds to load credentials on first startup), then get a token and test:

```bash
# Get application-caller client secret
ADMIN_TOKEN=$(curl -s -X POST "http://keycloak.localtest.me:8080/realms/master/protocol/openid-connect/token" \
  -d "client_id=admin-cli" -d "grant_type=password" -d "username=admin" -d "password=admin" | jq -r '.access_token')

APP_SECRET=$(curl -s -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://keycloak.localtest.me:8080/admin/realms/demo/clients?clientId=application-caller" | jq -r '.[0].secret')

# Get an access token (password grant with test-user)
export ACCESS_TOKEN=$(curl -s -X POST \
  "http://keycloak.localtest.me:8080/realms/demo/protocol/openid-connect/token" \
  -d "grant_type=password" \
  -d "client_id=application-caller" \
  -d "client_secret=$APP_SECRET" \
  -d "username=test-user" \
  -d "password=password" \
  -d "scope=openid authproxy-aud" | jq -r '.access_token')
```

**Valid request (inbound validation passes, token exchange, forwarded to demo-app):**
```bash
curl -H "Authorization: Bearer $ACCESS_TOKEN" http://localhost:9080/test
# Expected response: "authorized"
```

**Invalid token (rejected by inbound validation):**
```bash
curl -H "Authorization: Bearer invalid-token" http://localhost:9080/test
# Expected response: "unauthorized"
```

**No authorization header (rejected by inbound validation):**
```bash
curl http://localhost:9080/test
# Expected response: "unauthorized: missing Authorization header"
```

## Kubernetes Testing

When deployed to Kubernetes, you can test the services internally:

**Test demo app directly:**
```bash
kubectl run test-pod --image=curlimages/curl --rm -it --restart=Never -- curl -H "Authorization: Bearer $ACCESS_TOKEN" http://demo-app-service:8081/test
```

**View logs:**
```bash
# Auth proxy logs (pass-through proxy)
kubectl logs deployment/auth-proxy -c auth-proxy

# Envoy proxy + ext proc logs (inbound validation and outbound token exchange)
kubectl logs deployment/auth-proxy -c envoy-proxy

# Demo app logs
kubectl logs deployment/demo-app

# Follow logs in real-time
kubectl logs -f deployment/auth-proxy -c envoy-proxy
```

**Check service status:**
```bash
# List pods
kubectl get pods

# List services
kubectl get svc

# Describe deployments
kubectl describe deployment auth-proxy
kubectl describe deployment demo-app
```

## Clean Up

**Remove Kubernetes deployment:**
```bash
make undeploy
kubectl delete secret auth-proxy-config --ignore-not-found=true
```

**Delete kind cluster:**
```bash
make kind-delete
```
