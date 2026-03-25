# [Dev] Work environment setup
build-local:
	go mod download

build-proto:
	protoc --go-grpc_out=. --go-grpc_opt=paths=source_relative --go_out=. --go_opt=paths=source_relative api/types/api.proto

# [Local w/o containers] Run both services locally
test-local:
	./script/start-server-local.sh

# [Local w/ containers] Build individual images
build-pi-controller-server:
	docker build -t pi-controller-server:latest -f cmd/pi-controller-server/Dockerfile .

build-pi-controller-agent:
	docker build -t pi-controller-agent:latest -f cmd/pi-controller-agent/Dockerfile .

build-all: build-pi-controller-server build-pi-controller-agent

# [Local w/ containers] Run individual containers
run-pi-controller-server:
	docker run --rm -p 5005:5005 pi-controller-server:latest

run-pi-controller-agent:
	docker run --rm -p 50051:50051 pi-controller-agent:latest

run-all:
	docker container prune -f; docker compose up --build --remove-orphans

# [Prod] Install and run the agent on Raspberry Pi
prod-install-agent:
	@echo "Installing raspivid-stream service..."
	bash ./script/install-pi-agent.sh
	@echo "Building and starting pi-controller-agent..."
	bash ./script/run-pi-controller.sh

# Stop all services
stop-all:
	docker compose down

# Clean up dangling Docker images
clean:
	docker system prune -af --volumes
