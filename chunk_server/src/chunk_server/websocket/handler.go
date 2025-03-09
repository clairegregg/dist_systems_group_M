package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
	"github.com/gorilla/websocket"
	"github.com/Cormuckle/dist_systems_group_M/chunk_server/playerstate"
)

// upgrader is used to upgrade HTTP connections to WebSocket.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// In production, you should restrict allowed origins.
		return true
	},
}

// WSHandler upgrades an HTTP connection to a WebSocket and processes incoming messages.
func WSHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Send the current game state to the new client.
	if state := playerstate.GetGameStateJSON(); state != nil {
		conn.WriteMessage(websocket.TextMessage, state)
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

		// Update the shared game state.
		playerstate.UpdatePlayerState(ps)
		log.Printf("Updated player state for %s", ps.ID)

		// Optionally, send the updated state back to this client.
		if state := playerstate.GetGameStateJSON(); state != nil {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, state); err != nil {
				log.Printf("WebSocket write error: %v", err)
				break
			}
		}
	}
}