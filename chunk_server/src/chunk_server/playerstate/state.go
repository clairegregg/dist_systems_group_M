package playerstate

import (
	"encoding/json"
	"log"
	"sync"
)

// Position holds the relative X and Y coordinates.
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
// Note: Position here is expected to be relative to the grid’s top-left corner.
type PlayerState struct {
	ID       string   `json:"id"`
	Position Position `json:"position"`
	Velocity Velocity `json:"velocity"`
	Score    int      `json:"score"`
}

// gameStatePayload wraps the players map.
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
	log.Printf("Updated player state for %s, score: %d", ps.ID, ps.Score)
}

// RemovePlayerState removes a player's state (for example, when the connection closes).
func RemovePlayerState(playerID string) {
	globalGameState.mu.Lock()
	defer globalGameState.mu.Unlock()
	delete(globalGameState.Players, playerID)
	log.Printf("Removed player state for %s", playerID)
}

// GetGameStateJSON returns the current game state wrapped in a "players" field as JSON.
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

// ---------------- Pellet Synchronization ----------------

// Pellet represents a pellet's state.
type Pellet struct {
	ID       string   `json:"id"`
	Position Position `json:"position"`
}

// pelletStatePayload wraps the pellet slice.
type pelletStatePayload struct {
	Pellets []Pellet `json:"pellets"`
}

// GlobalPelletState holds the state of all pellets.
type GlobalPelletState struct {
	Pellets []Pellet
	mu      sync.RWMutex
}

var globalPelletState = GlobalPelletState{
	Pellets: []Pellet{},
}

// UpdatePellets updates the global pellet state.
func UpdatePellets(newPellets []Pellet) {
	globalPelletState.mu.Lock()
	defer globalPelletState.mu.Unlock()
	globalPelletState.Pellets = newPellets
	log.Printf("Updated pellets, count: %d", len(newPellets))
}

// RemovePellet removes a pellet by its ID.
func RemovePellet(pelletID string) {
	globalPelletState.mu.Lock()
	defer globalPelletState.mu.Unlock()
	newPellets := make([]Pellet, 0, len(globalPelletState.Pellets))
	for _, p := range globalPelletState.Pellets {
		if p.ID != pelletID {
			newPellets = append(newPellets, p)
		}
	}
	globalPelletState.Pellets = newPellets
	log.Printf("Removed pellet %s, remaining: %d", pelletID, len(newPellets))
}

// GetPelletsJSON returns the current pellet state as JSON.
func GetPelletsJSON() []byte {
	globalPelletState.mu.RLock()
	defer globalPelletState.mu.RUnlock()
	payload := pelletStatePayload{
		Pellets: globalPelletState.Pellets,
	}
	state, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling pellets: %v", err)
		return nil
	}
	return state
}