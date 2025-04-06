package playerstate

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
)

var CurrentChunkKey string

// ------------------ Player State ------------------

// Position holds the relative X and Y coordinates.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Location holds the location of the object within the 16 grids managed by each
// chunk server. Both can hold values between 0-3.
type Location struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Velocity holds the x and y velocity components.
type Velocity struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Dropper
type DropperState struct {
	ID            string   `json:"id"`
	Position      Position `json:"position"`
	Velocity      Velocity `json:"velocity"`
	LastPosition  Position `json:"lastPosition"`
	PelletCounter int      `json:"pelletCounter"`
}

type GlobalDropperState struct {
	Droppers map[string]DropperState
	mu       sync.RWMutex
}

var globalDropperState = GlobalDropperState{
	Droppers: make(map[string]DropperState),
}

func UpdateDropperState(ds DropperState) {
	globalDropperState.mu.Lock()
	defer globalDropperState.mu.Unlock()
	globalDropperState.Droppers[ds.ID] = ds
}

func RemoveDropper(dsID string) {
	globalDropperState.mu.Lock()
	defer globalDropperState.mu.Unlock()
	delete(globalDropperState.Droppers, dsID)
}

func GetDroppers() map[string]DropperState {
	globalDropperState.mu.RLock()
	defer globalDropperState.mu.RUnlock()
	copyMap := make(map[string]DropperState)
	for id, ds := range globalDropperState.Droppers {
		copyMap[id] = ds
	}
	return copyMap
}

// Ghosts
type GhostState struct {
	ID       string   `json:"id"`
	Position Position `json:"position"`
	Velocity Velocity `json:"velocity"`
}

type GlobalGhostState struct {
	Ghosts map[string]map[string]GhostState
	mu     sync.RWMutex
}

var globalGhostState = GlobalGhostState{
	Ghosts: make(map[string]map[string]GhostState),
}

func UpdateGhostState(chunkID string, gs GhostState) {
	globalGhostState.mu.Lock()
	defer globalGhostState.mu.Unlock()

	if globalGhostState.Ghosts[chunkID] == nil {
		globalGhostState.Ghosts[chunkID] = make(map[string]GhostState)
	}
	globalGhostState.Ghosts[chunkID][gs.ID] = gs
}

func RemoveGhost(chunkID, ghostID string) {
	globalGhostState.mu.Lock()
	defer globalGhostState.mu.Unlock()

	if chunkGhosts, ok := globalGhostState.Ghosts[chunkID]; ok {
		delete(chunkGhosts, ghostID)
	}
}

func GetGhosts(chunkID string) map[string]GhostState {
	globalGhostState.mu.RLock()
	defer globalGhostState.mu.RUnlock()

	// Return a copy of the chunk’s ghosts
	ghostsCopy := make(map[string]GhostState)
	if chunkGhosts, ok := globalGhostState.Ghosts[chunkID]; ok {
		for id, ghost := range chunkGhosts {
			ghostsCopy[id] = ghost
		}
	}
	return ghostsCopy
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

func GetPlayers() map[string]PlayerState {
	globalGameState.mu.RLock()
	defer globalGameState.mu.RUnlock()

	copy := make(map[string]PlayerState)
	for id, player := range globalGameState.Players {
		copy[id] = player
	}
	return copy
}

// Only return players in a specific map chunk
func GetPlayersInChunk(x, y int) map[string]PlayerState {
	globalGameState.mu.RLock()
	defer globalGameState.mu.RUnlock()

	result := make(map[string]PlayerState)
	for id, p := range globalGameState.Players {
		if p.Location.X == x && p.Location.Y == y {
			result[id] = p
		}
	}
	return result
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
	m  map[string]bool
}

// Not used to golang so just did what worked
var pelletMap = [16]eatenPellets{
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
	eatenPellets{m: make(map[string]bool)},
}

func mapLocation(X int, Y int) int {
	return X*4 + Y
}

// RemovePellet marks a pellet as eaten by its ID.
func RemovePellet(pelletID string, X int, Y int) {
	var loc = mapLocation(X, Y)
	pelletMap[loc].mu.Lock()
	defer pelletMap[loc].mu.Unlock()
	pelletMap[loc].m[pelletID] = true
}

// GetEatenPellets returns a slice of eaten pellet IDs and then clears the stored map.
// This acts as a "delta" so that each call returns only new removals.
func GetEatenPellets(X int, Y int) []string {
	var loc = mapLocation(X, Y)
	pelletMap[loc].mu.RLock()
	defer pelletMap[loc].mu.RUnlock()
	ids := make([]string, 0, len(pelletMap[loc].m))
	for id := range pelletMap[loc].m {
		ids = append(ids, id)
	}
	return ids
}

func GetEatenPelletsMap(loc int) []string {
	pelletMap[loc].mu.RLock()
	defer pelletMap[loc].mu.RUnlock()
	ids := make([]string, 0, len(pelletMap[loc].m))
	for id := range pelletMap[loc].m {
		ids = append(ids, id)
	}
	return ids
}

// RestoredPellet represents a pellet that has been restored by a dropper
type RestoredPellet struct {
	ID       string   `json:"id"`
	Position Position `json:"position"`
	MapIndex int      `json:"mapIndex"`
}

// GlobalRestoredPelletsState keeps track of pellets restored by droppers
type GlobalRestoredPelletsState struct {
	Pellets map[string]map[string]RestoredPellet
	mu      sync.RWMutex
}

var globalRestoredPelletsState = GlobalRestoredPelletsState{
	Pellets: make(map[string]map[string]RestoredPellet),
}

// PelletExists checks if a pellet with the same position and map already exists
func (rp *RestoredPellet) PelletExists(pellets map[string]RestoredPellet) bool {
	for _, pellet := range pellets {
		if pellet.MapIndex == rp.MapIndex &&
			math.Abs(pellet.Position.X-rp.Position.X) < 5 &&
			math.Abs(pellet.Position.Y-rp.Position.Y) < 5 {
			return true
		}
	}
	return false
}

// And update the AddRestoredPellet function:
func AddRestoredPellet(chunkID string, pellet RestoredPellet) {
	globalRestoredPelletsState.mu.Lock()
	defer globalRestoredPelletsState.mu.Unlock()

	if globalRestoredPelletsState.Pellets[chunkID] == nil {
		globalRestoredPelletsState.Pellets[chunkID] = make(map[string]RestoredPellet)
	}

	// Check if a similar pellet already exists before adding
	if !pellet.PelletExists(globalRestoredPelletsState.Pellets[chunkID]) {
		globalRestoredPelletsState.Pellets[chunkID][pellet.ID] = pellet
		log.Printf("Added new restored pellet at (%f,%f) for map %d",
			pellet.Position.X, pellet.Position.Y, pellet.MapIndex)
	} else {
		log.Printf("Skipped adding duplicate pellet at (%f,%f) for map %d",
			pellet.Position.X, pellet.Position.Y, pellet.MapIndex)
	}
}

// GetRestoredPellets returns all restored pellets for a specific chunk
func GetRestoredPellets(chunkID string) map[string]RestoredPellet {
	globalRestoredPelletsState.mu.RLock()
	defer globalRestoredPelletsState.mu.RUnlock()

	pelletsCopy := make(map[string]RestoredPellet)
	if chunkPellets, ok := globalRestoredPelletsState.Pellets[chunkID]; ok {
		for id, pellet := range chunkPellets {
			pelletsCopy[id] = pellet
		}

		globalRestoredPelletsState.Pellets[chunkID] = make(map[string]RestoredPellet)
	}

	return pelletsCopy
}

// IsEaten checks if a pellet has been eaten
func IsPelletEaten(pelletID string, loc int) bool {
	pelletMap[loc].mu.RLock()
	defer pelletMap[loc].mu.RUnlock()

	eatenPellets := GetEatenPelletsMap(loc)

	for _, id := range eatenPellets {
		if id == pelletID {
			log.Printf("Pellet %s is eaten", pelletID)
			return true
		}
	}

	isEaten := pelletMap[loc].m[pelletID]
	return isEaten
}

// UnmarkPellet removes a pellet from the eaten pellets list
func UnmarkPellet(pelletID string, loc int) {
	pelletMap[loc].mu.Lock()
	defer pelletMap[loc].mu.Unlock()
	delete(pelletMap[loc].m, pelletID)
}

// ---------------- Combined Game State ----------------

// combinedStatePayload wraps both players and the list of eaten pellet IDs.
type combinedStatePayload struct {
	Players         map[string]PlayerState    `json:"players"`
	EatenPellets    []string                  `json:"eatenPellets"`
	RestoredPellets map[string]RestoredPellet `json:"restoredPellets"`
	Ghosts          map[string]GhostState     `json:"ghosts"`
	Droppers        map[string]DropperState   `json:"droppers"`
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
	eaten := GetEatenPellets(X, Y)

	// Use the global chunk key if set.
	var ghosts map[string]GhostState
	var restoredPellets map[string]RestoredPellet

	chunkKey := CurrentChunkKey
	if chunkKey == "" {
		// Fallback if for some reason CurrentChunkKey isn't set.
		chunkKey = fmt.Sprintf("%d-%d", X, Y)
	}

	ghosts = GetGhosts(chunkKey)
	restoredPellets = GetRestoredPellets(chunkKey)
	droppers := GetDroppers()

	payload := combinedStatePayload{
		Players:         playersCopy,
		EatenPellets:    eaten,
		RestoredPellets: restoredPellets,
		Ghosts:          ghosts,
		Droppers:        droppers,
	}
	state, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling combined game state: %v", err)
		return nil
	}
	return state
}
