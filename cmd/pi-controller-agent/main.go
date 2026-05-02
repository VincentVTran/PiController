// Package main implements the pi-controller-agent gRPC server.
// It runs on the Raspberry Pi and exposes camera control operations.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"time"

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

func (s *server) StreamFrames(req *pb.StreamRequest, stream pb.PiAgentController_StreamFramesServer) error {
	framerate := req.Framerate
	if framerate <= 0 {
		framerate = 5
	}
	logger.Info("StreamFrames started", "framerate", framerate)

	ctx := stream.Context()

	raspivid := exec.CommandContext(ctx,
		"raspivid", "-o", "-", "-t", "0",
		"-w", "640", "-h", "360", "-fps", "25", "--rotation", "270",
	)
	ffmpeg := exec.CommandContext(ctx,
		"ffmpeg", "-loglevel", "quiet",
		"-i", "pipe:0",
		"-vf", fmt.Sprintf("fps=%d", framerate),
		"-f", "image2pipe", "-vcodec", "mjpeg", "pipe:1",
	)

	pipe, err := raspivid.StdoutPipe()
	if err != nil {
		return fmt.Errorf("raspivid stdout pipe: %w", err)
	}
	ffmpeg.Stdin = pipe

	ffmpegOut, err := ffmpeg.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}

	if err := raspivid.Start(); err != nil {
		return fmt.Errorf("start raspivid: %w", err)
	}
	if err := ffmpeg.Start(); err != nil {
		raspivid.Process.Kill()
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	defer func() {
		raspivid.Process.Kill()
		ffmpeg.Process.Kill()
	}()

	return parseMJPEGStream(ffmpegOut, func(frame []byte) error {
		return stream.Send(&pb.FrameChunk{
			Data:        frame,
			TimestampMs: time.Now().UnixMilli(),
		})
	}, ctx)
}

// parseMJPEGStream reads an MJPEG byte stream and calls send for each complete JPEG frame.
//
// Parameters:
//   - r: the raw byte stream from ffmpeg's stdout (continuous bytes, no built-in framing)
//   - send: a callback that receives one complete JPEG frame at a time — in our case,
//     this is stream.Send() which forwards the frame over the gRPC connection to the server
//   - ctx: the gRPC stream's context — when the server closes the stream or the connection
//     drops, ctx.Done() fires and we stop reading
//
// How MJPEG works:
// An MJPEG stream is just JPEG images concatenated one after another with no envelope.
// The only way to find where one image ends and the next begins is by looking for the
// JPEG End-Of-Image marker: two bytes 0xFF 0xD9.
//
// How the buffering works:
// We can't assume each r.Read() call gives us exactly one frame — it might give us
// half a frame, or two frames at once. So we accumulate bytes in `buf` and scan for
// the EOI marker in a loop. Each time we find one, we extract everything up to and
// including it as a complete frame and call send(). Whatever bytes remain after the
// marker stay in `buf` for the next iteration.
func parseMJPEGStream(r io.Reader, send func([]byte) error, ctx context.Context) error {
	var buf bytes.Buffer
	chunk := make([]byte, 65536)
	eoi := []byte{0xFF, 0xD9}

	for {
		// Check if the gRPC stream has been cancelled (e.g. server disconnected).
		// This runs before every read so we don't block on a dead stream.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Read the next batch of raw bytes from ffmpeg.
		// n is how many bytes were actually read this iteration.
		n, err := r.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n]) // append new bytes to our accumulation buffer
			data := buf.Bytes()

			// Extract all complete frames that fit in the buffer so far.
			for {
				idx := bytes.Index(data, eoi)
				if idx < 0 {
					break // no complete frame yet, wait for more bytes
				}

				// idx+2 because the EOI marker itself is 2 bytes (0xFF, 0xD9)
				frame := make([]byte, idx+2)
				copy(frame, data[:idx+2])

				// send() calls stream.Send() — this is what transmits the frame
				// over gRPC to the pi-controller-server.
				if sendErr := send(frame); sendErr != nil {
					return sendErr
				}

				// Discard the frame we just sent and keep the remainder.
				data = data[idx+2:]
				buf.Reset()
				buf.Write(data)
				data = buf.Bytes()
			}
		}
		if err == io.EOF {
			return nil // ffmpeg exited cleanly
		}
		if err != nil {
			return err
		}
	}
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
