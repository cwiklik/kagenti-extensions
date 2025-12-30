# AuthProxy quickstart

This document gives a step-by-step tutorial of getting started with the AuthProxy in a local Kind cluster. 

The final architecture deployed is as follows: `CURL command -> AuthProxy -> Demo App`

The Demo App is a demo application used for testing. The AuthProxy will validate bearer tokens in incoming requests and exchange them for the Demo App. 

The demo goes as follows:
1. Install Kagenti
1. Build and deploy the Demo App and AuthProxy
1. Configure Keycloak
1. Test the flow

## Step 1: Install Kagenti
First, we recommend to deploy Kagenti to a local Kind cluster with the Ansible installer. Instructions are available [here](https://github.com/kagenti/kagenti/blob/main/docs/install.md#ansible-based-installer-recommended). 

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

Finally, let's port-forward access to the AuthProxy. Do this in a separate terminal:

```bash
kubectl port-forward svc/auth-proxy-service 9090:8080
```

## Step 3: Configure Keycloak

Now that our services are deployed, we can configure Keycloak with two clients: `auth_proxy_caller` and `auth_proxy`. 
The `auth_proxy_caller` will be used by us on the command line to obtain an initial access token that is accepted by the `auth_proxy`. The `auth_proxy` will be used by the AuthProxy pod to exchange the token. 

Let's setup a light Python environment and run the script:

```bash
cd quickstart
python -m venv venv
source venv/bin/activate
pip install --upgrade pip
pip install -r requirements.txt
python setup_keycloak.py
```

This will spit out a command to export the `CLIENT_SECRET`. Run this command. 

## Step 4: Test the Flow

Using the exported `CLIENT_SECRET` environment variable, obtain an initial access token with the following command: 
```
export ACCESS_TOKEN=$(curl -sX POST -H "Content-Type: application/x-www-form-urlencoded" -d "client_secret=$CLIENT_SECRET" -d "grant_type=password" -d "client_id=application-caller" -d "username=test-user" -d "password=password" "http://keycloak.localtest.me:8080/realms/demo/protocol/openid-connect/token" | jq -r '.access_token')
```

Now we can access the application. Run:

**Valid request (will be forwarded):**
```bash
curl -H "Authorization: Bearer $ACCESS_TOKEN" http://localhost:9090/test
# Expected response: "authorized"
```

**Invalid request (will be rejected by proxy):** Use an invalid token, for example by setting `SOME_OTHER_TOKEN` to a random string, a malformed JWT, or a token issued for a different client/user.
```bash
export SOME_OTHER_TOKEN="invalid-or-expired-token-value"
curl -H "Authorization: Bearer $SOME_OTHER_TOKEN" http://localhost:9090/test
# Expected response: "Unauthorized - invalid token"
```

**No authorization header:**
```bash
curl http://localhost:9090/test
# Expected response: "Unauthorized - invalid token"
```

## Kubernetes Testing

When deployed to Kubernetes, you can test the services internally:

**Test demo app directly:**
```bash
kubectl run test-pod --image=curlimages/curl --rm -it --restart=Never -- curl -H "Authorization: Bearer $ACCESS_TOKEN" http://demo-app-service:8081/test
```

**View logs:**
```bash
# Auth proxy logs
kubectl logs deployment/auth-proxy

# Demo app logs
kubectl logs deployment/demo-app

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
kubectl describe deployment demo-app
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
