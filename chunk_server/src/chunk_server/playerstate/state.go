package playerstate

import (
	"encoding/json"
	"log"
	"sync"
)

// ------------------ Player State ------------------

// Position holds the relative X and Y coordinates.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Location holds the location of the object within the 16 grids managed by each
// chunk server. Both can hold values between 0-3.
type Location struct {
	X int 	`json:"x"`
	Y int 	`json:"y"`
}

// Velocity holds the x and y velocity components.
type Velocity struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// PlayerState represents the state of an individual player.
// The Status field marks whether the player is "active" or "left".
type PlayerState struct {
	ID       string   `json:"id"`
	Position Position `json:"position"`
	Velocity Velocity `json:"velocity"`
	Score    int      `json:"score"`
	Status   string   `json:"status"` // "active" or "left"
	Location Location `json:"location"`
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

var globalGameState = GameState{
	Players: make(map[string]PlayerState),
}

// UpdatePlayerState updates the state for a specific player in a thread-safe manner.
func UpdatePlayerState(ps PlayerState) {
	globalGameState.mu.Lock()
	defer globalGameState.mu.Unlock()
	// Default status to "active" if not provided.
	if ps.Status == "" {
		ps.Status = "active"
	}
	globalGameState.Players[ps.ID] = ps
	log.Printf("Updated player state for %s, score: %d, status: %s", ps.ID, ps.Score, ps.Status)
}

// MarkPlayerLeft marks a player's state as "left" instead of removing it.
func MarkPlayerLeft(playerID string) {
	globalGameState.mu.Lock()
	defer globalGameState.mu.Unlock()
	if ps, ok := globalGameState.Players[playerID]; ok {
		ps.Status = "left"
		globalGameState.Players[playerID] = ps
		log.Printf("Marked player %s as left", playerID)
	}
}

// RemovePlayerState removes a player's state from the game.
func RemovePlayerState(playerID string) {
	globalGameState.mu.Lock()
	defer globalGameState.mu.Unlock()
	delete(globalGameState.Players, playerID)
	log.Printf("Removed player state for %s", playerID)
}

// GetGameStateJSON returns the current game state (players only) as JSON.
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

// GetGameState returns a copy of the current game state as a map.
func GetGameState() map[string]PlayerState {
	globalGameState.mu.RLock()
	defer globalGameState.mu.RUnlock()
	copyMap := make(map[string]PlayerState, len(globalGameState.Players))
	for k, v := range globalGameState.Players {
		copyMap[k] = v
	}
	return copyMap
}

// ---------------- Pellet Synchronization ----------------

// Instead of storing all pellet information (which may be huge), we only keep track
// of pellets that have been eaten. Clients initially load the full pellet map (e.g., from the map API),
// then apply these removals.
// We store the eaten pellet IDs in a thread-safe map.
// var eatenPellets = struct {
// 	mu sync.RWMutex
// 	m  map[string]bool
// }{
// 	m: make(map[string]bool),
// }

type eatenPellets = struct {
	mu sync.RWMutex
	m map[string]bool
}


// Not used to golang so just did what worked
var pelletMap = [16]eatenPellets{
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
	eatenPellets{m:make(map[string]bool)},
}

func mapLocation(X int, Y int) int {
	return X*4+Y
}

// RemovePellet marks a pellet as eaten by its ID.
func RemovePellet(pelletID string, X int, Y int) {
	var loc = mapLocation(X,Y)
	pelletMap[loc].mu.Lock()
	defer pelletMap[loc].mu.Unlock()
	pelletMap[loc].m[pelletID] = true
	log.Printf("Pellet %s marked as eaten", pelletID)
}

// GetEatenPellets returns a slice of eaten pellet IDs and then clears the stored map.
// This acts as a "delta" so that each call returns only new removals.
func GetEatenPellets(X int, Y int) []string {
	var loc = mapLocation(X,Y)
	pelletMap[loc].mu.RLock()
	defer pelletMap[loc].mu.RUnlock()
	ids := make([]string, 0, len(pelletMap[loc].m))
	for id := range pelletMap[loc].m {
		ids = append(ids, id)
	}
	return ids
}

// ---------------- Combined Game State ----------------

// combinedStatePayload wraps both players and the list of eaten pellet IDs.
type combinedStatePayload struct {
	Players      map[string]PlayerState `json:"players"`
	EatenPellets []string               `json:"eatenPellets"`
}

// GetCombinedGameStateJSON returns a combined JSON payload of players and eaten pellet IDs.
// It uses GetEatenPellets() to get the delta of pellet deletions.
func GetCombinedGameStateJSON(X int, Y int) []byte {
	// Copy players state.
	globalGameState.mu.RLock()
	playersCopy := make(map[string]PlayerState, len(globalGameState.Players))
	for k, v := range globalGameState.Players {
		playersCopy[k] = v
	}
	globalGameState.mu.RUnlock()

	// Get the eaten pellet IDs (delta).
	eaten := GetEatenPellets(X,Y)

	payload := combinedStatePayload{
		Players:      playersCopy,
		EatenPellets: eaten,
	}
	state, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling combined game state: %v", err)
		return nil
	}
	return state
}