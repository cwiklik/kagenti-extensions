# `client-registration`

`client-registration` is a container image designed to automatically register Kubernetes workloads as OAuth2/OpenID Connect clients in Keycloak. It may be used with SPIFFE/SPIRE to use the pod’s SPIFFE ID as the client identifier, simplifying secure service-to-service authentication and reducing manual configuration.

* [Usage with SPIRE](#spire-client-registration)

## SPIRE Client Registration

This guide walks you through installing SPIRE components, Gateway API, Keycloak, and deploying a SPIFFE-enabled workload.

### 1. Install SPIRE CRDs

```bash
helm upgrade --install spire-crds spire-crds -n spire-mgmt --repo https://spiffe.github.io/helm-charts-hardened/ --create-namespace --wait
```

### 2. Install SPIRE (Server + Agent)

Use your custom Helm values file:

```bash
helm upgrade --install spire spire \
  -n spire-mgmt \
  --repo https://spiffe.github.io/helm-charts-hardened/ \
  -f https://raw.githubusercontent.com/kagenti/kagenti/main/kagenti/installer/app/resources/spire-helm-values.yaml
```

### 3. Install Gateway API CRDs

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml
```

### 4. Create Keycloak Namespace

```bash
kubectl apply -f "https://raw.githubusercontent.com/kagenti/kagenti/refs/heads/main/kagenti/installer/app/resources/keycloak-namespace.yaml"
```

### 5. Deploy Keycloak with Postgres

```bash
kubectl apply -f "https://raw.githubusercontent.com/kagenti/kagenti/refs/heads/main/kagenti/installer/app/resources/keycloak.yaml" -n keycloak
```

### 6. Port Forward Keycloak

```bash
kubectl rollout status statefulset/keycloak -n keycloak --timeout=120s
kubectl port-forward service/keycloak -n keycloak 8080:8080
```

You can now access Keycloak: http://keycloak.localtest.me:8080

**Default credentials:**
```
Username: admin
Password: admin
```

### 7. Configure Your SPIFFE-Enabled Deployment

Apply the example deployment:

```bash
kubectl apply -f example_deployment_spiffe.yaml
```

The example deployment, name `my-app`, will run BusyBox along with everything needed for automated client registration with KeyCloak.

### 8. Verify Client Registration in Keycloak

Wait for the client registration to complete.

```bash
kubectl wait --for=condition=available --timeout=120s deployment/my-app
```
Log in to Keycloak and navigate to [Clients](http://keycloak.localtest.me:8080/admin/master/console/#/master/clients).

Confirm a new client has been created; `Client ID` should be a SPIFFE ID and `Name` should be `my-app`.

![`my-app` client](images/my-app_client.png)