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

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// In production, restrict allowed origins.
		return true
	},
}

var (
	clients   = make(map[*websocket.Conn]string) // maps connection pointer to playerID
	clientsMu sync.Mutex
)

// Exported broadcast function used by the ticker.
func BroadcastGameState() {
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

func WSHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	clientsMu.Lock()
	clients[conn] = "" // initially, no player ID is associated
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		playerID := clients[conn]
		delete(clients, conn)
		clientsMu.Unlock()

		if playerID != "" {
			playerstate.RemovePlayerState(playerID)
		}
		conn.Close()
	}()

	// Send the initial game state to the new client.
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

		var ps playerstate.PlayerState
		if err := json.Unmarshal(message, &ps); err != nil {
			log.Printf("Error parsing player state: %v", err)
			continue
		}

		clientsMu.Lock()
		if clients[conn] == "" && ps.ID != "" {
			clients[conn] = ps.ID
		}
		clientsMu.Unlock()

		// Update the shared game state.
		playerstate.UpdatePlayerState(ps)
		// Removed immediate broadcast – state will be broadcast periodically.
	}
}