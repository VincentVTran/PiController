package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	model "github.com/vincentvtran/pi-controller/pkg/model"
)

var (
	port           = flag.Int("port", 5005, "The gRPC server port")
	stage          = flag.String("stage", "local", "Stage for Dragonfly DB URL (e.g., local or production)")
	queueName      = flag.String("queue", "pi-websocket-queue", "Dragonfly DB queue name")
	upgrader       = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	dragonFlyDbURL string
	config         model.ApplicationConfig
	redisClient    *redis.Client
	// rabbitURL        string
	// rabbitExchange   string
	// rabbitRoutingKey string
)

func loadConfig() {
	file, err := os.Open("config/application-config.json")
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		log.Fatalf("Failed to decode config file: %v", err)
	}
}

// Determine Dragonfly DB URL based on stage
func determineDragonflyDBURL() {
	switch *stage {
	case "local":
		dragonFlyDbURL = config.Local.DragonflyDbURL
		log.Println("Using local Dragonfly DB URL")
	case "prod":
		dragonFlyDbURL = config.Prod.DragonflyDbURL
		log.Println("Using production Dragonfly DB URL")
	default:
		log.Fatalf("Dragonfly DB URL for stage '%s' not found in config", *stage)
	}
}

// Initialize Redis client for Dragonfly DB
func initRedisClient(url string) *redis.Client {
	opt, err := redis.ParseURL("redis://" + url)
	if err != nil {
		log.Fatalf("Failed to parse Redis URL: %v", err)
	}
	return redis.NewClient(opt)
}

// Publish message to Dragonfly DB queue
func publishToQueue(message []byte) error {
	// Create PiOperation from message
	var piOp model.PiOperation
	piOp.ClientID = "websocket-client" // Default client ID for websocket messages
	piOp.Operation = "websocket_message"
	piOp.Parameters = map[string]interface{}{
		"message":   string(message),
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(piOp)
	if err != nil {
		return fmt.Errorf("failed to marshal PiOperation to JSON: %v", err)
	}

	// Push to Redis list (queue)
	err = redisClient.LPush(context.Background(), *queueName, jsonData).Err()
	if err != nil {
		return fmt.Errorf("failed to publish message to queue %s: %v", *queueName, err)
	}

	log.Printf("Message published to Dragonfly DB queue: %s", *queueName)
	return nil
}

// WebSocket handler
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	log.Println("WebSocket connection established")

	for {
		// Read message from client
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Error reading message: %v", err)
			break
		}
		log.Printf("WebSocket received message: %s", message)

		// Publish message to queue
		err = publishToQueue(message)
		if err != nil {
			log.Printf("Failed to publish message to queue: %v", err)
		} else {
			log.Printf("Message published to queue")
		}

		// Store message in Dragonfly DB
		err = redisClient.Set(context.Background(), "websocket:"+fmt.Sprintf("%d", time.Now().UnixNano()), message, 0).Err()
		if err != nil {
			log.Printf("Failed to store message in Dragonfly DB: %v", err)
		} else {
			log.Printf("Message stored in Dragonfly DB")
		}

		// Echo the message back to the client
		err = conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			log.Printf("Error writing message: %v", err)
			break
		}
	}
}

func startWebSocketServer() {
	http.HandleFunc("/ws", handleWebSocket)
	log.Printf("WebSocket server listening on port %d", *port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	if err != nil {
		log.Fatalf("Failed to start WebSocket server: %v", err)
	}
}

func main() {
	flag.Parse()

	// Load configuration
	loadConfig()

	// Determine Dragonfly DB URL based on stage
	determineDragonflyDBURL()

	// Initialize Redis client
	redisClient = initRedisClient(dragonFlyDbURL)
	defer redisClient.Close()

	log.Printf("Connected to Dragonfly DB at %s", dragonFlyDbURL)

	// Start WebSocket server
	startWebSocketServer()
}
