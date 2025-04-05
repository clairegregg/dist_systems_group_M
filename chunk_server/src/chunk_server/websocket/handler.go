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

// MessageEnvelope wraps incoming messages with a type and raw data.
type MessageEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// BroadcastGameState sends the current combined game state (players and eaten pellet IDs)
// to all connected clients.

// TODO: not sure if the game state is reference or new object when returned
func BroadcastGameState() {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	for conn := range clients {
		gameState := playerstate.GetGameState()
		var player = gameState[clients[conn]]
		state := playerstate.GetCombinedGameStateJSON(player.Location.X, player.Location.Y)
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, state); err != nil {
			log.Printf("Broadcast write error: %v", err)
		}
	}
}

// WSHandler upgrades HTTP requests to WebSocket connections and listens for updates.
func WSHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	clientsMu.Lock()
	clients[conn] = "" // initially, no player ID is associated
	clientsMu.Unlock()

	// When the connection closes, mark the player as left and update all clients.
	defer func() {
		clientsMu.Lock()
		playerID := clients[conn]
		delete(clients, conn)
		clientsMu.Unlock()

		if playerID != "" {
			playerstate.MarkPlayerLeft(playerID)
			BroadcastGameState()
		}
		conn.Close()
	}()

	// Send the initial game state to the new client.
	if state := playerstate.GetCombinedGameStateJSON(1, 1); state != nil {
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, state); err != nil {
			log.Printf("Error writing initial game state: %v", err)
			return
		}
	}

	// Listen for messages from the client.
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		var envelope MessageEnvelope
		if err := json.Unmarshal(message, &envelope); err != nil {
			log.Printf("Error parsing message envelope: %v", err)
			continue
		}

		switch envelope.Type {
		case "player":
			var ps playerstate.PlayerState
			if err := json.Unmarshal(envelope.Data, &ps); err != nil {
				log.Printf("Error parsing player state: %v", err)
				continue
			}
			clientsMu.Lock()
			if clients[conn] == "" && ps.ID != "" {
				clients[conn] = ps.ID
			}
			clientsMu.Unlock()
			playerstate.UpdatePlayerState(ps)
		case "pellet":
			// Expect pellet update messages to include a pelletId.
			var pelletUpdate struct {
				PelletID string `json:"pelletId"`
				// Optionally include PlayerID and Score if needed.
				PlayerID string               `json:"id"`
				Score    int                  `json:"score"`
				Location playerstate.Location `json:"location"`
			}
			if err := json.Unmarshal(envelope.Data, &pelletUpdate); err != nil {
				log.Printf("Error parsing pellet update: %v", err)
				continue
			}
			// Mark the pellet as eaten globally.
			playerstate.RemovePellet(pelletUpdate.PelletID, pelletUpdate.Location.X, pelletUpdate.Location.Y)
			// Broadcast the updated game state immediately.
			BroadcastGameState()
		case "ghost_collision":
			var collisionUpdate struct {
				PlayerID string               `json:"id"`
				Score    int                  `json:"score"`
				Location playerstate.Location `json:"location"`
			}
			if err := json.Unmarshal(envelope.Data, &collisionUpdate); err != nil {
				log.Printf("Error parsing ghost collision update: %v", err)
				continue
			}

			// Update the player state with the new score after collision
			ps := playerstate.PlayerState{
				ID:       collisionUpdate.PlayerID,
				Score:    collisionUpdate.Score,
				Status:   "active",
				Location: collisionUpdate.Location,
			}
			playerstate.UpdatePlayerState(ps)

			// Log the collision event
			log.Printf("Player %s was caught by a ghost! New score: %d", collisionUpdate.PlayerID, collisionUpdate.Score)

			// Broadcast the updated game state immediately
			BroadcastGameState()
		default:
			log.Printf("Unknown message type: %s", envelope.Type)
		}
	}
}
