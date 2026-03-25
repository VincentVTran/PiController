package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
	pb "github.com/vincentvtran/pi-controller/api/types"
	model "github.com/vincentvtran/pi-controller/pkg/model"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	port      = flag.Int("port", 5005, "WebSocket server port")
	stage     = flag.String("stage", "local", "Stage (local or prod)")
	agentAddr = flag.String("agent-addr", "", "Override pi-controller-agent address (host:port)")
	upgrader  = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	config    model.ApplicationConfig
	piClient  pb.PiAgentControllerClient
)

type CameraMessage struct {
	Operation string `json:"operation"`
	ClientID  string `json:"client_id"`
	Enable    bool   `json:"enable"`
}

type CameraResponse struct {
	StatusCode int32  `json:"status_code"`
	Output     string `json:"output"`
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

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	log.Println("WebSocket connection established")

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		var msg CameraMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("Invalid message format: %v", err)
			writeError(conn, "invalid message format")
			continue
		}

		var resp *pb.OperationResponse
		switch msg.Operation {
		case "configure_camera":
			resp, err = piClient.ConfigureCamera(context.Background(), &pb.CameraRequest{
				ClientId:   msg.ClientID,
				Parameters: &pb.CameraParameter{Enable: msg.Enable},
			})
		case "retrieve_camera_status":
			resp, err = piClient.RetrieveCameraStatus(context.Background(), &emptypb.Empty{})
		default:
			writeError(conn, fmt.Sprintf("unknown operation: %s", msg.Operation))
			continue
		}

		if err != nil {
			log.Printf("gRPC call failed: %v", err)
			writeError(conn, err.Error())
			continue
		}

		out, _ := json.Marshal(CameraResponse{StatusCode: resp.StatusCode, Output: resp.Output})
		if err := conn.WriteMessage(websocket.TextMessage, out); err != nil {
			log.Printf("WebSocket write error: %v", err)
			break
		}
	}
}

func writeError(conn *websocket.Conn, msg string) {
	out, _ := json.Marshal(CameraResponse{StatusCode: 500, Output: msg})
	conn.WriteMessage(websocket.TextMessage, out)
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

	http.HandleFunc("/ws", handleWebSocket)
	log.Printf("pi-controller-server listening on port %d", *port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
