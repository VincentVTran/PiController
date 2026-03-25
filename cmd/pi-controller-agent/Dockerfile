FROM golang:1.23

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN go build -o pi-controller-agent ./cmd/pi-controller-agent/main.go

EXPOSE 50051

CMD ["./pi-controller-agent"]
