# ✅ SPIRE + Keycloak Integration Guide

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

Edit `example_deployment_spiffe.yaml`:

Add your own container image by replacing placeholders:
    - `COMPONENT_NAME`
    - `IMAGE_REGISTRY`
    - `IMAGE_NAME`
    - `IMAGE_TAG`

Apply the deployment:

```bash
kubectl apply -f example_deployment_spiffe.yaml
```

### 8. Verify Client Registration in Keycloak

Wait for the client registration to complete.

```bash
kubectl wait --for=condition=available --timeout=120s deployment/COMPONENT_NAME
```
Log in to Keycloak and navigate to [Clients](http://keycloak.localtest.me:8080/admin/master/console/#/master/clients).

Confirm a new client has been created; `Client ID` should be a SPIFFE ID and `Name` should be `COMPONENT_NAME`.