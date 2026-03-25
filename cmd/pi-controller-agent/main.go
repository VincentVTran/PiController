// Package main implements the pi-controller-agent gRPC server.
// It runs on the Raspberry Pi and exposes camera control operations.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os/exec"
	"strings"

	pb "github.com/vincentvtran/pi-controller/api/types"
	"github.com/vincentvtran/pi-controller/pkg/logging"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	port   = flag.Int("port", 50051, "The gRPC server port")
	stage  = flag.String("stage", "local", "Stage (local or prod)")
	logger *slog.Logger
)

type server struct {
	pb.UnimplementedPiAgentControllerServer
	version  string
	clientID string
}

func (s *server) ConfigureCamera(ctx context.Context, in *pb.CameraRequest) (*pb.OperationResponse, error) {
	logger.Info("ConfigureCamera request", "client_id", in.ClientId, "enable", in.Parameters.Enable)
	if in.Parameters.Enable {
		if err := enableCamera(); err != nil {
			logger.Error("Failed to enable camera", "error", err)
			return &pb.OperationResponse{ApiVersion: s.version, StatusCode: 500, Output: err.Error()}, nil
		}
	} else {
		if err := disableCamera(); err != nil {
			logger.Error("Failed to disable camera", "error", err)
			return &pb.OperationResponse{ApiVersion: s.version, StatusCode: 500, Output: err.Error()}, nil
		}
	}
	return &pb.OperationResponse{ApiVersion: s.version, StatusCode: 200, Output: "OK"}, nil
}

func (s *server) RetrieveCameraStatus(ctx context.Context, _ *emptypb.Empty) (*pb.OperationResponse, error) {
	logger.Info("RetrieveCameraStatus request")
	out, err := exec.Command("systemctl", "is-active", "raspivid-stream").Output()
	if err != nil {
		logger.Error("Failed to retrieve camera status", "error", err)
		return &pb.OperationResponse{ApiVersion: s.version, StatusCode: 500, Output: "unknown"}, nil
	}
	status := strings.TrimSpace(string(out))
	return &pb.OperationResponse{ApiVersion: s.version, StatusCode: 200, Output: status}, nil
}

func enableCamera() error {
	return exec.Command("systemctl", "start", "raspivid-stream").Run()
}

func disableCamera() error {
	return exec.Command("systemctl", "stop", "raspivid-stream").Run()
}

func main() {
	flag.Parse()

	shutdown, err := logging.InitTelemetry(context.Background())
	if err != nil {
		log.Fatalf("failed to init telemetry: %v", err)
	}
	defer shutdown(context.Background())

	clientID := "pi-controller-agent"
	logger = otelslog.NewLogger(clientID)

	switch *stage {
	case "local":
		logger.Info("Using local configuration")
	case "prod":
		logger.Info("Using prod configuration")
	default:
		logger.Warn("Unknown stage, defaulting to prod configuration", "stage", *stage)
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterPiAgentControllerServer(s, &server{version: "1.0.0", clientID: clientID})
	logger.Info("pi-controller-agent listening", "addr", lis.Addr())

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
