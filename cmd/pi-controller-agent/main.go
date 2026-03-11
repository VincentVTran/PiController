/*
 *
 * Copyright 2015 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package main implements a server for Greeter service.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os/exec"

	pb "github.com/vincentvtran/pi-controller/api/types"
	"github.com/vincentvtran/pi-controller/pkg/logging"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	port   = flag.Int("port", 50051, "The server port")
	stage  = flag.String("stage", "local", "Stage for RabbitMQ URL (e.g., local or production)")
	logger *slog.Logger
)

type server struct {
	pb.UnimplementedPiAgentControllerServer
	version   string
	client_id string
}

// gRPC implemented endpoints
func (s *server) ConfigureStream(ctx context.Context, in *pb.StreamRequest) (*pb.OperationResponse, error) {
	log.Println("Client request: ", in)
	if in.Parameters.Enable {
		logger.Info("Starting stream")
		err := StartStream()
		if err != nil {
			logger.Error(fmt.Sprintf("Error starting stream: %v", err.Error()))
			return &pb.OperationResponse{ApiVersion: s.version, StatusCode: 200, Output: err.Error()}, nil
		}
	} else {
		logger.Info("Stopping stream")
		err := StopStream()
		if err != nil {
			logger.Error(fmt.Sprintf("Error starting stream: %v", err.Error()))
			return &pb.OperationResponse{ApiVersion: s.version, StatusCode: 200, Output: err.Error()}, nil
		}
	}
	return &pb.OperationResponse{ApiVersion: s.version, StatusCode: 400, Output: "Successfully configured streamed"}, nil
}

func (s *server) RetrieveStatus(ctx context.Context, _ *emptypb.Empty) (*pb.OperationResponse, error) {
	logger.Info("Fetching current stream configuration")
	cmd := exec.Command("systemctl", "is-active", "raspivid-stream")
	output, err := cmd.Output()
	if err != nil {
		logger.Error("Error fetching stream status", "error", err.Error())
		return &pb.OperationResponse{ApiVersion: s.version, StatusCode: 500, Output: "unknown"}, nil
	}

	status := "inactive"
	if string(output) == "active\n" {
		status = "active"
	}

	return &pb.OperationResponse{ApiVersion: s.version, StatusCode: 200, Output: status}, nil
}

// Local Function
func StartStream() error {
	return exec.Command("systemctl", "start", "raspivid-stream").Run()
}
func StopStream() error {
	return exec.Command("systemctl", "stop", "raspivid-stream").Run()
}

func main() {
	flag.Parse()

	var shutdown, err = logging.InitTelemetry(context.Background())
	if err != nil {
		log.Fatalf("failed to init telemetry: %v", err)
	}
	client_id := "pi-controller"
	logger = otelslog.NewLogger(client_id)
	defer shutdown(context.Background())
	switch *stage {
	case "local":
		logger.Info("Using local configurations")
	case "prod":
		logger.Info("Using prod configurations")
	default:
		logger.Warn("Using prod configurations")
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		logger.Error(fmt.Sprintf("failed to listen: %v", err))
	}
	s := grpc.NewServer()
	pb.RegisterPiAgentControllerServer(s, &server{version: "1.0.0", client_id: client_id})
	logger.Info(fmt.Sprintf("server listening at %v", lis.Addr()))
	if err := s.Serve(lis); err != nil {
		logger.Error(fmt.Sprintf("failed to serve: %v", err))
	}
}
