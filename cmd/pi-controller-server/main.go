package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	pb "github.com/vincentvtran/pi-controller/api/types"
	model "github.com/vincentvtran/pi-controller/pkg/model"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	port      = flag.Int("port", 5005, "REST server port")
	stage     = flag.String("stage", "local", "Stage (local or prod)")
	agentAddr = flag.String("agent-addr", "", "Override pi-controller-agent address (host:port)")
	config    model.ApplicationConfig
	piClient  pb.PiAgentControllerClient
	streams   = newStreamManager()
)

type CameraConfigRequest struct {
	ClientID string `json:"client_id"`
	Enable   bool   `json:"enable"`
}

type CameraResponse struct {
	StatusCode int32  `json:"status_code"`
	Output     string `json:"output"`
}

// InferenceResult is broadcast to SSE subscribers for each received frame.
// Extend this struct with ML model output once inference is integrated.
type InferenceResult struct {
	TimestampMs int64 `json:"timestamp_ms"`
	FrameSize   int   `json:"frame_size"`
}

// streamManager manages a single active frame stream from the agent
// and fans out InferenceResults to all connected SSE subscribers.
type streamManager struct {
	mu          sync.Mutex
	cancel      context.CancelFunc
	subscribers map[chan InferenceResult]struct{}
}

func newStreamManager() *streamManager {
	return &streamManager{
		subscribers: make(map[chan InferenceResult]struct{}),
	}
}

func (sm *streamManager) subscribe() chan InferenceResult {
	ch := make(chan InferenceResult, 10)
	sm.mu.Lock()
	sm.subscribers[ch] = struct{}{}
	sm.mu.Unlock()
	return ch
}

func (sm *streamManager) unsubscribe(ch chan InferenceResult) {
	sm.mu.Lock()
	delete(sm.subscribers, ch)
	sm.mu.Unlock()
}

func (sm *streamManager) broadcast(result InferenceResult) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for ch := range sm.subscribers {
		select {
		case ch <- result:
		default: // drop frame if subscriber is too slow
		}
	}
}

func (sm *streamManager) start(framerate int32) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.cancel != nil {
		return fmt.Errorf("stream already active")
	}

	ctx, cancel := context.WithCancel(context.Background())
	sm.cancel = cancel

	go func() {
		defer func() {
			sm.mu.Lock()
			sm.cancel = nil
			sm.mu.Unlock()
		}()

		stream, err := piClient.StreamFrames(ctx, &pb.StreamRequest{Framerate: framerate})
		if err != nil {
			log.Printf("StreamFrames error: %v", err)
			return
		}

		for {
			chunk, err := stream.Recv()
			if err != nil {
				log.Printf("Stream recv: %v", err)
				return
			}

			// TODO: run ML inference on chunk.Data (raw JPEG bytes)
			sm.broadcast(InferenceResult{
				TimestampMs: chunk.TimestampMs,
				FrameSize:   len(chunk.Data),
			})
		}
	}()

	return nil
}

func (sm *streamManager) stop() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.cancel != nil {
		sm.cancel()
		sm.cancel = nil
	}
}

func loadConfig() {
	file, err := os.Open("config/application-config.json")
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&config); err != nil {
		log.Fatalf("Failed to decode config file: %v", err)
	}
}

func agentAddress() string {
	if *agentAddr != "" {
		return *agentAddr
	}
	switch *stage {
	case "prod":
		return fmt.Sprintf("%s:%s", config.Prod.PiControllerDNS, config.Prod.PiControllerPort)
	default:
		return fmt.Sprintf("%s:%s", config.Local.PiControllerDNS, config.Local.PiControllerPort)
	}
}

func handleConfigureCamera(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CameraConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, CameraResponse{StatusCode: 400, Output: "invalid request body"})
		return
	}

	resp, err := piClient.ConfigureCamera(context.Background(), &pb.CameraRequest{
		ClientId:   req.ClientID,
		Parameters: &pb.CameraParameter{Enable: req.Enable},
	})
	if err != nil {
		log.Printf("gRPC ConfigureCamera failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, CameraResponse{StatusCode: 500, Output: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, CameraResponse{StatusCode: resp.StatusCode, Output: resp.Output})
}

func handleCameraStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp, err := piClient.RetrieveCameraStatus(context.Background(), &emptypb.Empty{})
	if err != nil {
		log.Printf("gRPC RetrieveCameraStatus failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, CameraResponse{StatusCode: 500, Output: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, CameraResponse{StatusCode: resp.StatusCode, Output: resp.Output})
}

func handleStreamStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := streams.start(5); err != nil {
		writeJSON(w, http.StatusConflict, CameraResponse{StatusCode: 409, Output: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, CameraResponse{StatusCode: 200, Output: "streaming started"})
}

func handleStreamStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	streams.stop()
	writeJSON(w, http.StatusOK, CameraResponse{StatusCode: 200, Output: "streaming stopped"})
}

// handleStreamResults is an SSE endpoint that pushes InferenceResults to the browser.
// Connect with: GET /camera/stream/results
// Each event: data: {"timestamp_ms":...,"frame_size":...}
func handleStreamResults(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := streams.subscribe()
	defer streams.unsubscribe(ch)

	for {
		select {
		case result := <-ch:
			data, _ := json.Marshal(result)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func main() {
	flag.Parse()
	loadConfig()

	addr := agentAddress()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to pi-controller-agent at %s: %v", addr, err)
	}
	defer conn.Close()

	piClient = pb.NewPiAgentControllerClient(conn)
	log.Printf("Connected to pi-controller-agent at %s", addr)

	http.HandleFunc("/camera", handleConfigureCamera)
	http.HandleFunc("/camera/status", handleCameraStatus)
	http.HandleFunc("/camera/stream/start", handleStreamStart)
	http.HandleFunc("/camera/stream/stop", handleStreamStop)
	http.HandleFunc("/camera/stream/results", handleStreamResults)

	log.Printf("pi-controller-server listening on port %d", *port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
