#!/usr/bin/env bash
# Function to find an available port
make_port_available() {
    PORT=$1
    PID=$(lsof -t -i:$PORT)
    if [ -n "$PID" ]; then
        echo "Killing process $PID on port $PORT"
        kill -9 $PID
    else
        echo $PORT
    fi
}

PI_PORT=$(make_port_available 50051)

go build -o pi-controller-agent ./cmd/pi-controller-agent/main.go

sudo ./pi-controller-agent --stage=prod --port $PI_PORT

