# AuthProxy quickstart

This document gives a step-by-step tutorial of getting started with the AuthProxy in a local Kind cluster. 

The final architecture deployed is as follows: `CURL command -> AuthProxy -> Target`

The Target is a demo target application. The AuthProxy will validate bearer tokens in incoming requests and exchange them for the Target. 

The demo goes as follows:
1. Install Kagenti
1. Build and deploy the Target and AuthProxy
1. Configure Keycloak
1. Test the flow

## Step 1: Install Kagenti
First, we recommend to deploy Kagenti to a local Kind cluster with the Ansible installer. Instructions are available [here](https://github.com/kagenti/kagenti/blob/main/docs/install.md#ansible-based-installer-alternative). 

This should start a local Kind cluster named `kagenti`. 

The key component is Keycloak which has been deployed to the `keycloak` namespace and exposed as `keycloak-service`. 

## Step 2: Build and deploy the Target and AuthProxy

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

TODO add scripts

## Step 4: Test the Flow

First let's get the client secret:

First, let's obtain an initial access token using the following command: 

```
export ACCESS_TOKEN=$(curl -sX POST -H "Content-Type: application/x-www-form-urlencoded" -d "client_secret=q0NhCIf0ClXSuuEfbonQyZaLoXQ1EL1P" -d "grant_type=password" -d "client_id=auth-proxy-caller" -d "username=admin" -d "password=admin" "http://keycloak.localtest.me:8080/realms/master/protocol/openid-connect/token" | jq -r ‘.access_token')
```

Now we can access the application. Run:

**Valid request (will be forwarded):**
```bash
curl -H "Authorization: Bearer $ACCESS_TOKEN" http://localhost:9090/test
# Expected response: "authorized"
```

**Invalid request (will be rejected by proxy):** Consider using an expired token
```bash
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
