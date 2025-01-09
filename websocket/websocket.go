package websocket

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var (
	// Separate client maps for different metric types
	cpuClients     = make(map[*websocket.Conn]bool)
	memoryClients  = make(map[*websocket.Conn]bool)
	networkClients = make(map[*websocket.Conn]bool)

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
)

// AddClient registers a new client WebSocket connection to the specified client map
func addClient(clientMap map[*websocket.Conn]bool, client *websocket.Conn, metricType string) {
	clientMap[client] = true
	log.Printf("New client connected to %s metrics stream", metricType)
}

// RemoveClient removes a WebSocket client from the specified client map
func removeClient(clientMap map[*websocket.Conn]bool, client *websocket.Conn, metricType string) {
	delete(clientMap, client)
	client.Close()
	log.Printf("Client disconnected from %s metrics stream", metricType)
}

// BroadcastCPUMetrics broadcasts CPU metrics to all connected clients
func BroadcastCPUMetrics(metrics string) {
	for client := range cpuClients {
		err := client.WriteMessage(websocket.TextMessage, []byte(metrics))
		if err != nil {
			log.Println("Error sending CPU metrics to client:", err)
			removeClient(cpuClients, client, "CPU")
		}
	}
}

// BroadcastMemoryMetrics broadcasts memory metrics to all connected clients
func BroadcastMemoryMetrics(metrics string) {
	for client := range memoryClients {
		err := client.WriteMessage(websocket.TextMessage, []byte(metrics))
		if err != nil {
			log.Println("Error sending memory metrics to client:", err)
			removeClient(memoryClients, client, "Memory")
		}
	}
}

// BroadcastNetworkMetrics broadcasts network metrics to all connected clients
func BroadcastNetworkMetrics(metrics string) {
	for client := range networkClients {
		err := client.WriteMessage(websocket.TextMessage, []byte(metrics))
		if err != nil {
			log.Println("Error sending network metrics to client:", err)
			removeClient(networkClients, client, "Network")
		}
	}
}

// createWebSocketHandler creates a WebSocket handler for a specific metric type
func createWebSocketHandler(clientMap map[*websocket.Conn]bool, metricType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Error upgrading to WebSocket for %s metrics: %v", metricType, err)
			return
		}
		defer conn.Close()

		// Register new client
		addClient(clientMap, conn, metricType)

		// Keep the connection open
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				log.Printf("Error reading message from %s metrics client: %v", metricType, err)
				break
			}
		}

		// Clean up when client disconnects
		removeClient(clientMap, conn, metricType)
	}
}

// StartWebSocketServer starts the WebSocket server with multiple routes
func StartWebSocketServer() {
	// Set up different routes for different metric types
	http.HandleFunc("/ws/cpu", createWebSocketHandler(cpuClients, "CPU"))
	http.HandleFunc("/ws/memory", createWebSocketHandler(memoryClients, "Memory"))
	http.HandleFunc("/ws/network", createWebSocketHandler(networkClients, "Network"))

	go func() {
		log.Println("WebSocket server started with the following endpoints:")
		log.Println("- CPU Metrics: ws://localhost:8080/ws/cpu")
		log.Println("- Memory Metrics: ws://localhost:8080/ws/memory")
		log.Println("- Network Metrics: ws://localhost:8080/ws/network")

		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatal("Error starting WebSocket server:", err)
		}
	}()
}
