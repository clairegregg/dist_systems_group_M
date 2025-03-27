package playerstate

import (
	"encoding/json"
	"log"
	"sync"
)

// Position holds the X and Y coordinates.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Velocity holds the x and y velocity components.
type Velocity struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// PlayerState represents the state of an individual player.
type PlayerState struct {
	ID       string   `json:"id"`
	Position Position `json:"position"`
	Velocity Velocity `json:"velocity"`
	// Add additional fields (score, lives, etc.) as needed.
}

// gameStatePayload is used to wrap the players map.
type gameStatePayload struct {
	Players map[string]PlayerState `json:"players"`
}

// GameState holds the state of all players.
type GameState struct {
	Players map[string]PlayerState
	mu      sync.RWMutex
}

// globalGameState is the shared game state.
var globalGameState = GameState{
	Players: make(map[string]PlayerState),
}

// UpdatePlayerState updates the state for a specific player in a thread-safe manner.
func UpdatePlayerState(ps PlayerState) {
	globalGameState.mu.Lock()
	defer globalGameState.mu.Unlock()
	globalGameState.Players[ps.ID] = ps
	log.Printf("Updated player state for %s", ps.ID)
}

// RemovePlayerState removes a player's state (for example, when the connection closes).
func RemovePlayerState(playerID string) {
	globalGameState.mu.Lock()
	defer globalGameState.mu.Unlock()
	delete(globalGameState.Players, playerID)
	log.Printf("Removed player state for %s", playerID)
}

// GetGameStateJSON returns the current game state wrapped in a "players" field as a JSON byte slice.
func GetGameStateJSON() []byte {
	globalGameState.mu.RLock()
	defer globalGameState.mu.RUnlock()
	payload := gameStatePayload{
		Players: globalGameState.Players,
	}
	state, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling game state: %v", err)
		return nil
	}
	return state
}