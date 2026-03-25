# PiController

A distributed system for controlling a Raspberry Pi via gRPC, exposed through a WebSocket server running on Kubernetes.

## Architecture

```
Client (WebSocket)
      │
      ▼
pi-controller-server   ← Kubernetes deployment (WebSocket, port 5005)
      │ gRPC
      ▼
pi-controller-agent    ← Raspberry Pi (gRPC server, port 50051)
      │ systemctl
      ▼
raspivid-stream.service
```

### Components

| Component | Location | Description |
|---|---|---|
| `pi-controller-agent` | Raspberry Pi | gRPC server that controls on-device hardware (camera) |
| `pi-controller-server` | Kubernetes | WebSocket server that proxies client requests to the agent via gRPC |

---

## Camera Control

The first supported operation is enabling and disabling the camera (via the `raspivid-stream` systemd service).

### gRPC API (`api/types/api.proto`)

| RPC | Request | Description |
|---|---|---|
| `configureCamera` | `CameraRequest` | Enable or disable the camera |
| `retrieveCameraStatus` | `Empty` | Get the current camera status (`active` / `inactive`) |

### WebSocket API (`pi-controller-server`)

Connect to `ws://<server>:5005/ws` and send JSON messages.

**Enable camera:**
```json
{"operation": "configure_camera", "client_id": "my-client", "enable": true}
```

**Disable camera:**
```json
{"operation": "configure_camera", "client_id": "my-client", "enable": false}
```

**Get status:**
```json
{"operation": "retrieve_camera_status"}
```

**Response:**
```json
{"status_code": 200, "output": "active"}
```

---

## Development

### Prerequisites

- Go 1.23+
- `protoc` with `protoc-gen-go` and `protoc-gen-go-grpc` plugins
- Docker (optional)

### Initial setup

```bash
# Download dependencies
make build-local

# Regenerate protobuf Go files after editing api/types/api.proto
make build-proto
```

### Running locally (without containers)

```bash
make test-local
```

### Running locally (with containers)

```bash
make run-all
```

---

## Deployment

### Installing the agent on Raspberry Pi

1. Copy the repo to the Pi.
2. Run the install script to register the `raspivid-stream` systemd service:

```bash
make prod-run-pi-controller
```

This installs `installable/raspivid-stream.service`, enables it, and starts the `pi-controller-agent` binary.

### Kubernetes (pi-controller-server)

Apply the deployment manifest:

```bash
kubectl apply -f deployment.yaml
```

Configure the Pi's DNS/IP in `config/application-config.json` under the `prod` key before building the image.
