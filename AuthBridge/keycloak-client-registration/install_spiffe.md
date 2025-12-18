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
    - `{{ COMPONENT_NAME }}`
    - `{{ IMAGE_REGISTRY }}`
    - `{{ IMAGE_NAME }}`
    - `{{ IMAGE_TAG }}`

Apply the deployment:

```bash
kubectl apply -f example_deployment_spiffe.yaml
```

### 8. Verify Client Registration in Keycloak

- Log in to Keycloak.
- Navigate to **Clients**.
- Confirm:
  - **Client ID** = SPIFFE ID
  - **Name** = `COMPONENT_NAME`

### ✅ Architecture Diagram

```mermaid
flowchart LR
    subgraph Cluster[Kubernetes Cluster]
        subgraph SPIRE[ SPIRE Components ]
            A[SPIRE Server
(Helm in spire-mgmt)]
            B[SPIRE Agent
(DaemonSet on each node)]
        end

        subgraph Workload[Your SPIFFE-enabled Deployment]
            C[Main App Container]
            D[spiffe-helper Sidecar]
            E[kagenti-client-registration Sidecar]
        end

        A --> B
        B --> D
        D --> C
        D --> E
    end

    subgraph External[External Systems]
        F[Keycloak
(Helm or YAML)]
        G[Browser / Admin UI]
    end

    E --> F
    G --> F
```

#### ✅ Notes & Tips
- If your pod cannot reach Keycloak at `keycloak.localtest.me`, remember that `*.localtest.me` resolves to `127.0.0.1` (your laptop). Use:
  - A **Service DNS name** if Keycloak runs in Kubernetes (e.g., `http://keycloak.keycloak.svc:8080`).
  - Or `host.docker.internal` if Keycloak runs on your host and your cluster supports it.
- For production, store sensitive values (Keycloak admin password) in a **Secret**, not a ConfigMap.
