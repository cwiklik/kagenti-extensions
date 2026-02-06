#!/bin/sh

set -e

PROXY_PORT="${PROXY_PORT:-15123}"
INBOUND_PROXY_PORT="${INBOUND_PROXY_PORT:-15124}"
PROXY_UID="${PROXY_UID:-1337}"
SSH_PORT="${SSH_PORT:-22}"
OUTBOUND_PORTS_EXCLUDE="${OUTBOUND_PORTS_EXCLUDE:-}"
INBOUND_PORTS_EXCLUDE="${INBOUND_PORTS_EXCLUDE:-}"

# Istio ztunnel defaults
ZTUNNEL_UID="${ZTUNNEL_UID:-1337}"
ZTUNNEL_INBOUND_PORT="${ZTUNNEL_INBOUND_PORT:-15008}"
ZTUNNEL_OUTBOUND_PORT="${ZTUNNEL_OUTBOUND_PORT:-15001}"
ZTUNNEL_TUNNEL_PORT="${ZTUNNEL_TUNNEL_PORT:-15006}"

echo "Setting up iptables rules for outbound traffic interception..."

# Create custom chains (ignore errors if they already exist)
iptables -t nat -N PROXY_OUTPUT 2>/dev/null || true
iptables -t nat -N PROXY_REDIRECT 2>/dev/null || true

# Flush any existing rules in our chains to ensure idempotency
iptables -t nat -F PROXY_OUTPUT 2>/dev/null || true
iptables -t nat -F PROXY_REDIRECT 2>/dev/null || true

# Redirect to proxy port
iptables -t nat -A PROXY_REDIRECT -p tcp -j REDIRECT --to-port "${PROXY_PORT}"

# Exclude traffic from proxy's own UID to prevent infinite loops
iptables -t nat -A PROXY_OUTPUT -m owner --uid-owner "${PROXY_UID}" -j RETURN

# Exclude traffic from ztunnel UID to prevent conflicts with Istio ambient mesh
iptables -t nat -A PROXY_OUTPUT -m owner --uid-owner "${ZTUNNEL_UID}" -j RETURN

# Exclude SSH traffic
iptables -t nat -A PROXY_OUTPUT -p tcp --dport "${SSH_PORT}" -j RETURN

# Exclude localhost traffic
iptables -t nat -A PROXY_OUTPUT -p tcp -d 127.0.0.1/32 -j RETURN

# Exclude Istio ztunnel ports to avoid interference
echo "Excluding Istio ztunnel ports: ${ZTUNNEL_INBOUND_PORT}, ${ZTUNNEL_OUTBOUND_PORT}, ${ZTUNNEL_TUNNEL_PORT}"
iptables -t nat -A PROXY_OUTPUT -p tcp --dport "${ZTUNNEL_INBOUND_PORT}" -j RETURN
iptables -t nat -A PROXY_OUTPUT -p tcp --dport "${ZTUNNEL_OUTBOUND_PORT}" -j RETURN
iptables -t nat -A PROXY_OUTPUT -p tcp --dport "${ZTUNNEL_TUNNEL_PORT}" -j RETURN

# Exclude specified outbound ports
if [ -n "${OUTBOUND_PORTS_EXCLUDE}" ]; then
  for port in $(echo "${OUTBOUND_PORTS_EXCLUDE}" | tr ',' ' '); do
    echo "Excluding outbound port ${port} from redirection"
    iptables -t nat -A PROXY_OUTPUT -p tcp --dport "${port}" -j RETURN
  done
fi

# Redirect all other TCP traffic
iptables -t nat -A PROXY_OUTPUT -p tcp -j PROXY_REDIRECT

# Insert rule at the beginning of OUTPUT chain with higher priority than Istio rules
# Check if rule already exists to avoid duplicates
if ! iptables -t nat -C OUTPUT -p tcp -j PROXY_OUTPUT 2>/dev/null; then
  iptables -t nat -I OUTPUT 1 -p tcp -j PROXY_OUTPUT
fi

echo "Outbound iptables rules configured successfully"
echo "Outbound traffic will be redirected to port ${PROXY_PORT}"

# === Inbound traffic interception ===
echo "Setting up iptables rules for inbound traffic interception..."

# Create custom chains for inbound (ignore errors if they already exist)
iptables -t nat -N PROXY_INBOUND 2>/dev/null || true
iptables -t nat -N PROXY_INBOUND_REDIRECT 2>/dev/null || true

# Flush any existing rules in our chains to ensure idempotency
iptables -t nat -F PROXY_INBOUND 2>/dev/null || true
iptables -t nat -F PROXY_INBOUND_REDIRECT 2>/dev/null || true

# Redirect inbound traffic to the inbound proxy port
iptables -t nat -A PROXY_INBOUND_REDIRECT -p tcp -j REDIRECT --to-port "${INBOUND_PROXY_PORT}"

# Exclude traffic destined to sidecar/infrastructure ports
iptables -t nat -A PROXY_INBOUND -p tcp --dport "${PROXY_PORT}" -j RETURN
iptables -t nat -A PROXY_INBOUND -p tcp --dport "${INBOUND_PROXY_PORT}" -j RETURN
iptables -t nat -A PROXY_INBOUND -p tcp --dport 9090 -j RETURN
iptables -t nat -A PROXY_INBOUND -p tcp --dport 9901 -j RETURN

# Exclude SSH traffic
iptables -t nat -A PROXY_INBOUND -p tcp --dport "${SSH_PORT}" -j RETURN

# Exclude Istio ztunnel ports
iptables -t nat -A PROXY_INBOUND -p tcp --dport "${ZTUNNEL_INBOUND_PORT}" -j RETURN
iptables -t nat -A PROXY_INBOUND -p tcp --dport "${ZTUNNEL_OUTBOUND_PORT}" -j RETURN
iptables -t nat -A PROXY_INBOUND -p tcp --dport "${ZTUNNEL_TUNNEL_PORT}" -j RETURN

# Exclude specified inbound ports
if [ -n "${INBOUND_PORTS_EXCLUDE}" ]; then
  for port in $(echo "${INBOUND_PORTS_EXCLUDE}" | tr ',' ' '); do
    echo "Excluding inbound port ${port} from redirection"
    iptables -t nat -A PROXY_INBOUND -p tcp --dport "${port}" -j RETURN
  done
fi

# Redirect all other inbound TCP traffic
iptables -t nat -A PROXY_INBOUND -p tcp -j PROXY_INBOUND_REDIRECT

# Insert rule at the beginning of PREROUTING chain
if ! iptables -t nat -C PREROUTING -p tcp -j PROXY_INBOUND 2>/dev/null; then
  iptables -t nat -I PREROUTING 1 -p tcp -j PROXY_INBOUND
fi

echo "Inbound iptables rules configured successfully"
echo "Inbound traffic will be redirected to port ${INBOUND_PROXY_PORT}"
echo "Istio ztunnel compatibility enabled"
