"""
setup_keycloak.py - AuthBridge Demo Setup

This script configures Keycloak for the AuthBridge demo that combines:
1. Client Registration with SPIFFE ID (for the caller)
2. AuthProxy sidecar for token exchange
3. Auth Target (target server) that validates exchanged tokens

Architecture:
  Caller Pod (Caller + SPIFFE Helper + Client Registration + AuthProxy)
       |
       | Token with audience "authproxy" (or caller's SPIFFE ID)
       v
  AuthProxy (Envoy) exchanges token
       |
       | Token with audience "auth-target"
       v
  Auth Target (validates token)

Clients created:
- authproxy: Used by AuthProxy to exchange tokens for auth-target access
- auth-target: Target audience for token exchange (required by Keycloak)

Client Scopes created:
- authproxy-aud: Adds "authproxy" to token audience
- auth-target-aud: Adds "auth-target" to token audience

Note: The caller client is auto-registered by the client-registration init container
using the SPIFFE ID as the client ID.
"""

from keycloak import KeycloakAdmin, KeycloakPostError
import sys

KEYCLOAK_URL = "http://keycloak.localtest.me:8080"
KEYCLOAK_REALM = "demo"
KEYCLOAK_ADMIN_USERNAME = "admin"
KEYCLOAK_ADMIN_PASSWORD = "admin"


def get_or_create_realm(keycloak_admin, realm_name):
    """Create realm if it doesn't exist."""
    try:
        realms = keycloak_admin.get_realms()
        for realm in realms:
            if realm['realm'] == realm_name:
                print(f"Realm '{realm_name}' already exists.")
                return
        keycloak_admin.create_realm({
            "realm": realm_name,
            "enabled": True,
            "displayName": realm_name,
        })
        print(f"Created realm '{realm_name}'.")
    except Exception as e:
        print(f"Error checking/creating realm: {e}")


def get_or_create_client(keycloak_admin, client_payload):
    """Create client if doesn't exist, return internal client ID."""
    client_id = client_payload['clientId']
    existing_client_id = keycloak_admin.get_client_id(client_id)
    if existing_client_id:
        print(f"Client '{client_id}' already exists.")
        return existing_client_id
    internal_id = keycloak_admin.create_client(client_payload)
    print(f"Created client '{client_id}'.")
    return internal_id


def get_or_create_client_scope(keycloak_admin, scope_payload):
    """Create client scope if doesn't exist, return scope ID."""
    scope_name = scope_payload.get("name")
    scopes = keycloak_admin.get_client_scopes()
    for scope in scopes:
        if scope['name'] == scope_name:
            print(f"Client scope '{scope_name}' already exists with ID: {scope['id']}")
            return scope['id']

    try:
        scope_id = keycloak_admin.create_client_scope(scope_payload)
        print(f"Created client scope '{scope_name}': {scope_id}")
        return scope_id
    except KeycloakPostError as e:
        print(f"Could not create client scope '{scope_name}': {e}")
        raise


def add_audience_mapper(keycloak_admin, scope_id, mapper_name, audience):
    """Add audience protocol mapper to a client scope."""
    mapper_payload = {
        "name": mapper_name,
        "protocol": "openid-connect",
        "protocolMapper": "oidc-audience-mapper",
        "consentRequired": False,
        "config": {
            "included.custom.audience": audience,
            "id.token.claim": "false",
            "access.token.claim": "true",
            "userinfo.token.claim": "false"
        }
    }
    
    try:
        keycloak_admin.add_mapper_to_client_scope(scope_id, mapper_payload)
        print(f"Added audience mapper '{mapper_name}' for audience '{audience}'")
    except Exception as e:
        # Mapper might already exist
        print(f"Note: Could not add mapper '{mapper_name}' (might already exist): {e}")


def main():
    print("=" * 60)
    print("AuthBridge Demo - Keycloak Setup")
    print("=" * 60)
    
    # Connect to Keycloak master realm first
    print(f"\nConnecting to Keycloak at {KEYCLOAK_URL}...")
    try:
        master_admin = KeycloakAdmin(
            server_url=KEYCLOAK_URL,
            username=KEYCLOAK_ADMIN_USERNAME,
            password=KEYCLOAK_ADMIN_PASSWORD,
            realm_name="master",
            user_realm_name="master"
        )
    except Exception as e:
        print(f"Failed to connect to Keycloak: {e}")
        print("\nMake sure Keycloak is running and accessible at:")
        print(f"  {KEYCLOAK_URL}")
        print("\nIf using port-forward, run:")
        print("  kubectl port-forward service/keycloak-service -n keycloak 8080:8080")
        sys.exit(1)
    
    # Create demo realm if needed
    print(f"\n--- Setting up realm: {KEYCLOAK_REALM} ---")
    get_or_create_realm(master_admin, KEYCLOAK_REALM)
    
    # Switch to demo realm
    keycloak_admin = KeycloakAdmin(
        server_url=KEYCLOAK_URL,
        username=KEYCLOAK_ADMIN_USERNAME,
        password=KEYCLOAK_ADMIN_PASSWORD,
        realm_name=KEYCLOAK_REALM,
        user_realm_name="master"
    )
    
    # Create authproxy client (used by AuthProxy sidecar for token exchange)
    print("\n--- Creating authproxy client ---")
    print("This client is used by the AuthProxy sidecar to exchange tokens")
    authproxy_id = get_or_create_client(keycloak_admin, {
        "clientId": "authproxy",
        "name": "Auth Proxy",
        "enabled": True,
        "publicClient": False,
        "standardFlowEnabled": False,
        "serviceAccountsEnabled": True,
        "attributes": {
            "standard.token.exchange.enabled": "true"
        }
    })
    
    # Create auth-target client (required as token exchange audience target)
    print("\n--- Creating auth-target client ---")
    print("This client is required as the target audience for token exchange")
    auth_target_id = get_or_create_client(keycloak_admin, {
        "clientId": "auth-target",
        "name": "Auth Target",
        "enabled": True,
        "publicClient": False,
        "standardFlowEnabled": False,
        "serviceAccountsEnabled": True,
        "attributes": {
            "standard.token.exchange.enabled": "true"
        }
    })
    
    # Create client scopes
    print("\n--- Creating client scopes ---")
    
    # authproxy-aud scope - added to caller's tokens
    # This makes the caller's token valid for AuthProxy
    authproxy_scope_id = get_or_create_client_scope(keycloak_admin, {
        "name": "authproxy-aud",
        "protocol": "openid-connect",
        "attributes": {
            "include.in.token.scope": "true",
            "display.on.consent.screen": "true"
        }
    })
    add_audience_mapper(keycloak_admin, authproxy_scope_id, "authproxy-aud", "authproxy")
    
    # auth-target-aud scope - added to exchanged tokens
    # This makes the AuthProxy's exchanged token valid for auth-target
    auth_target_scope_id = get_or_create_client_scope(keycloak_admin, {
        "name": "auth-target-aud",
        "protocol": "openid-connect",
        "attributes": {
            "include.in.token.scope": "true",
            "display.on.consent.screen": "true"
        }
    })
    add_audience_mapper(keycloak_admin, auth_target_scope_id, "auth-target-aud", "auth-target")
    
    # Assign scopes
    print("\n--- Assigning scopes ---")
    
    # Add authproxy-aud as a realm default scope
    # This ensures all clients (including auto-registered ones) get this scope
    # So their tokens will have "authproxy" in the audience
    try:
        keycloak_admin.add_default_default_client_scope(authproxy_scope_id)
        print("Added 'authproxy-aud' as realm default scope (all clients will get it).")
    except Exception as e:
        print(f"Note: Could not add 'authproxy-aud' as default scope (might already exist): {e}")
    
    # authproxy gets auth-target-aud (so its exchanged tokens target auth-target)
    try:
        keycloak_admin.add_client_default_client_scope(authproxy_id, auth_target_scope_id, {})
        print("Assigned 'auth-target-aud' as default scope to 'authproxy'.")
    except Exception as e:
        print(f"Note: Could not assign 'auth-target-aud' scope (might already exist): {e}")
    
    # auth-target also gets auth-target-aud (so tokens for auth-target have correct audience)
    try:
        keycloak_admin.add_client_default_client_scope(auth_target_id, auth_target_scope_id, {})
        print("Assigned 'auth-target-aud' as default scope to 'auth-target'.")
    except Exception as e:
        print(f"Note: Could not assign 'auth-target-aud' scope to auth-target (might already exist): {e}")
    
    # Retrieve and display secrets
    print("\n" + "=" * 60)
    print("SETUP COMPLETE")
    print("=" * 60)
    
    try:
        authproxy_secret = keycloak_admin.get_client_secrets(authproxy_id)['value']
        
        print("\n--- authproxy client credentials ---")
        print(f"Client ID: authproxy")
        print(f"Client Secret: {authproxy_secret}")
        
        print("\n" + "=" * 60)
        print("NEXT STEPS")
        print("=" * 60)
        
        print("\n1. Update the auth-proxy-config secret with the authproxy client secret:")
        print(f"\n   kubectl patch secret auth-proxy-config -n authbridge -p '{{\"stringData\":{{\"CLIENT_SECRET\":\"{authproxy_secret}\"}}}}'\n")
        
        print("2. Deploy the AuthBridge demo:")
        print("\n   # With SPIFFE (requires SPIRE)")
        print("   kubectl apply -f k8s/authbridge-deployment.yaml")
        print("\n   # OR without SPIFFE")
        print("   kubectl apply -f k8s/authbridge-deployment-no-spiffe.yaml\n")
        
        print("3. Wait for pods to be ready:")
        print("\n   kubectl wait --for=condition=available --timeout=120s deployment/caller -n authbridge")
        print("   kubectl wait --for=condition=available --timeout=120s deployment/auth-target -n authbridge\n")
        
        print("4. Test from inside the caller pod:")
        print("""
   kubectl exec -it deployment/caller -n authbridge -c caller -- sh
   
   # Inside the container (credentials are auto-populated):
   CLIENT_ID=$(cat /shared/client-id.txt)
   CLIENT_SECRET=$(cat /shared/client-secret.txt)
   
   # Get a token
   TOKEN=$(curl -sX POST \\
     http://keycloak-service.keycloak.svc:8080/realms/demo/protocol/openid-connect/token \\
     -d 'grant_type=client_credentials' \\
     -d "client_id=$CLIENT_ID" \\
     -d "client_secret=$CLIENT_SECRET" | jq -r '.access_token')
   
   # Call auth-target (AuthProxy will exchange the token)
   curl -H "Authorization: Bearer $TOKEN" http://auth-target-service:8081/test
   # Expected: "authorized"
""")
        
        print("\nNote: The caller client is auto-registered by the client-registration")
        print("container. For SPIFFE version, it uses the SPIFFE ID as client ID.")
        print("The client ID and secret are saved to /shared/client-id.txt and /shared/client-secret.txt.")
        
    except Exception as e:
        print(f"Could not retrieve secrets: {e}")


if __name__ == "__main__":
    main()
