#!/bin/sh

# Start the go-processor in the background
echo "Starting go-processor..."
/usr/local/bin/go-processor &
GO_PROCESSOR_PID=$!

# Give go-processor a moment to start
sleep 2

# Start Envoy in the foreground
echo "Starting Envoy..."
exec /usr/local/bin/envoy -c /etc/envoy/envoy.yaml --service-cluster auth-proxy --service-node auth-proxy --log-level debug
