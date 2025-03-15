package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Cormuckle/dist_systems_group_M/chunk_server/playerstate"
	"github.com/gorilla/websocket"
)

// upgrader upgrades HTTP connections to WebSocket.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// In production, restrict allowed origins.
		return true
	},
}

// clients stores all active WebSocket connections.
var (
	clients   = make(map[*websocket.Conn]string) // maps connection pointer to playerID
	clientsMu sync.Mutex
)

// broadcastGameState sends the current game state to all connected clients.
func broadcastGameState() {
	state := playerstate.GetGameStateJSON()
	if state == nil {
		return
	}

	clientsMu.Lock()
	defer clientsMu.Unlock()
	for conn := range clients {
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, state); err != nil {
			log.Printf("Broadcast write error: %v", err)
		}
	}
}

// WSHandler upgrades an HTTP connection to a WebSocket and processes incoming messages.
func WSHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Add this connection to the global clients map.
	clientsMu.Lock()
	clients[conn] = "" // initially, no player ID is associated
	clientsMu.Unlock()

	// Ensure cleanup when the connection is closed.
	defer func() {
		clientsMu.Lock()
		playerID := clients[conn]
		delete(clients, conn)
		clientsMu.Unlock()

		// Remove player's state if a player ID was set.
		if playerID != "" {
			playerstate.RemovePlayerState(playerID)
			broadcastGameState()
		}
		conn.Close()
	}()

	// Send the current game state to the new client.
	if state := playerstate.GetGameStateJSON(); state != nil {
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, state); err != nil {
			log.Printf("Error writing initial game state: %v", err)
			return
		}
	}

	// Listen for messages from the client.
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}
		if messageType != websocket.TextMessage {
			continue // Only process text messages.
		}

		// Parse the received JSON into a PlayerState.
		var ps playerstate.PlayerState
		if err := json.Unmarshal(message, &ps); err != nil {
			log.Printf("Error parsing player state: %v", err)
			continue
		}

		// Associate the connection with the player's ID if not already set.
		clientsMu.Lock()
		if clients[conn] == "" && ps.ID != "" {
			clients[conn] = ps.ID
		}
		clientsMu.Unlock()

		// Update the shared game state.
		playerstate.UpdatePlayerState(ps)
		// Broadcast the updated state to all clients.
		broadcastGameState()
	}
}