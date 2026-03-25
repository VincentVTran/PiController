FROM golang:1.23

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN go build -o pi-controller-server ./cmd/pi-controller-server/main.go

EXPOSE 5005

CMD ["./pi-controller-server"]
