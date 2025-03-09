package playerstate

import (
	"encoding/json"
	"log"
	"sync"
)

// PlayerState represents the state of an individual player.
type PlayerState struct {
	ID        string  `json:"id"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Direction string  `json:"direction"` // e.g., "up", "down", "left", "right"
	// Add additional fields (score, lives, etc.) as needed.
}

// GameState holds the state of all players.
type GameState struct {
	Players map[string]PlayerState `json:"players"`
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
}

// GetGameStateJSON returns the current game state as a JSON byte slice.
func GetGameStateJSON() []byte {
	globalGameState.mu.RLock()
	defer globalGameState.mu.RUnlock()
	state, err := json.Marshal(globalGameState.Players)
	if err != nil {
		log.Printf("Error marshalling game state: %v", err)
		return nil
	}
	return state
}