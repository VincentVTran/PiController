#!/usr/bin/env bash
set -euo pipefail

make_port_available() {
    PORT=$1
    PID=$(lsof -t -i:$PORT 2>/dev/null || true)
    if [ -n "$PID" ]; then
        echo "Killing process $PID on port $PORT" >&2
        kill -9 $PID
    fi
    echo $PORT
}

SERVER_PORT=$(make_port_available 5005)
AGENT_PORT=$(make_port_available 50051)

# Start pi-controller-agent in the background
go run cmd/pi-controller-agent/main.go --stage=local --port=$AGENT_PORT &
AGENT_PID=$!
echo "pi-controller-agent started (PID $AGENT_PID) on port $AGENT_PORT"

# Start pi-controller-server in the foreground
go run cmd/pi-controller-server/main.go --stage=local --port=$SERVER_PORT --agent-addr=localhost:$AGENT_PORT
