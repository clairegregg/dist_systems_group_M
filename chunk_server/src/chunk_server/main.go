package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Cormuckle/dist_systems_group_M/chunk_server/kafka"
	"github.com/Cormuckle/dist_systems_group_M/chunk_server/playerstate"
	"github.com/Cormuckle/dist_systems_group_M/chunk_server/websocket"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// LeaderboardEntry represents a player entry.
type LeaderboardEntry struct {
	UserName string `json:"userName"`
	Score    int    `json:"score"`
}

// Global variables.
var (
	kafkaProducer  *kafka.Producer
	kafkaConsumer  *kafka.Consumer // main consumer for other messages
	db             = make(map[string]string) // still used for /user and /admin endpoints
	chunkID        string
	chunkTopic     string
	centralTopic   = "chunk_to_central"
	broadcastTopic = "central_to_chunk_broadcast"
	localMap       [][]string // holds the 2D map in memory

	// Local sync data updated by broadcast messages.
	localLeaderboard   []LeaderboardEntry
	localActivePlayers []LeaderboardEntry
	localLeftPlayers   []LeaderboardEntry

	// We'll store the Kafka broker address in a global variable for use in requestMap.
	kafkaBroker string
)

func generateChunkID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = fmt.Sprintf("chunk_server_%d", time.Now().Unix())
	}
	return hostname
}

func setupRouter() *gin.Engine {
	r := gin.Default()
	r.Use(cors.Default())

	// Health check.
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// Sample user endpoint.
	r.GET("/user/:name", func(c *gin.Context) {
		user := c.Param("name")
		value, ok := db[user]
		if ok {
			c.JSON(http.StatusOK, gin.H{"user": user, "value": value})
		} else {
			c.JSON(http.StatusOK, gin.H{"user": user, "status": "no value"})
		}
	})

	authorized := r.Group("/", gin.BasicAuth(gin.Accounts{
		"foo":  "bar",
		"manu": "123",
	}))
	authorized.POST("admin", func(c *gin.Context) {
		user := c.MustGet(gin.AuthUserKey).(string)
		var jsonData struct {
			Value string `json:"value" binding:"required"`
		}
		if c.Bind(&jsonData) == nil {
			db[user] = jsonData.Value
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		}
	})

	// Send a message to the central server.
	r.POST("/send", func(c *gin.Context) {
		var req struct {
			Message string `json:"message" binding:"required"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}
		err := kafkaProducer.SendMessage(centralTopic, fmt.Sprintf("[%s]: %s", chunkID, req.Message))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "Failed to send"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "Message sent to central server"})
	})

	// WebSocket endpoint.
	r.GET("/ws", func(c *gin.Context) {
		websocket.WSHandler(c.Writer, c.Request)
	})

	// Expose the locally stored map.
	r.GET("/getMap", func(c *gin.Context) {
		if localMap == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "map not loaded"})
		} else {
			c.JSON(http.StatusOK, localMap)
		}
	})

	// New endpoints to expose sync data.

	r.GET("/leaderboard", func(c *gin.Context) {
		c.JSON(http.StatusOK, localLeaderboard)
	})
	r.GET("/scores/active", func(c *gin.Context) {
		c.JSON(http.StatusOK, localActivePlayers)
	})
	r.GET("/scores/left", func(c *gin.Context) {
		c.JSON(http.StatusOK, localLeftPlayers)
	})

	return r
}

// waitForCentralServer repeatedly pings the central server until it responds.
func waitForCentralServer(centralURL string) {
	for {
		resp, err := http.Get(centralURL + "/ping")
		if err == nil && resp.StatusCode == http.StatusOK {
			log.Printf("Central server is up at %s", centralURL)
			return
		}
		log.Printf("Central server not ready at %s. Retrying in 5 seconds...", centralURL)
		time.Sleep(5 * time.Second)
	}
}

// registerChunkID waits for the central server to be available and then sends a registration message via Kafka.
func registerChunkID() {
	centralURL := os.Getenv("CENTRAL_SERVER_URL")
	if centralURL == "" {
		centralURL = "http://central_server:8080" // adjust as needed
	}
	waitForCentralServer(centralURL)
	registrationMsg := fmt.Sprintf("REGISTER:%s", chunkID)
	err := kafkaProducer.SendMessage(centralTopic, registrationMsg)
	if err != nil {
		log.Printf("Failed to register chunk server via Kafka: %v", err)
	} else {
		log.Printf("Registered chunk server via Kafka with message: %s", registrationMsg)
	}
}

// requestMap uses a dedicated consumer to request and receive the map from the central server.
func requestMap() {
	centralURL := os.Getenv("CENTRAL_SERVER_URL")
	if centralURL == "" {
		centralURL = "http://central_server:8080"
	}
	waitForCentralServer(centralURL)
	mapConsumer, err := kafka.NewConsumer(kafkaBroker, chunkID+"_map")
	if err != nil {
		log.Fatalf("Failed to create map consumer: %v", err)
	}
	defer mapConsumer.Close()

	for {
		requestMsg := fmt.Sprintf("GET_MAP:%s", chunkID)
		err := kafkaProducer.SendMessage(centralTopic, requestMsg)
		if err != nil {
			log.Printf("Failed to send map request: %v", err)
		} else {
			log.Printf("Sent map request for chunk ID %s", chunkID)
		}
		msg, err := mapConsumer.ConsumeMessage(chunkTopic)
		if err != nil {
			log.Printf("Error consuming map response: %v", err)
		} else {
			if strings.HasPrefix(msg, "MAP_RESPONSE:") {
				mapJSON := strings.TrimPrefix(msg, "MAP_RESPONSE:")
				var receivedMap [][]string
				err = json.Unmarshal([]byte(mapJSON), &receivedMap)
				if err == nil {
					localMap = receivedMap
					log.Printf("Received map for chunk ID %s:", chunkID)
					for i, row := range localMap {
						log.Printf("Row %d: %v", i, row)
					}
					return
				} else {
					log.Printf("Error unmarshaling map: %v", err)
				}
			}
		}
		time.Sleep(5 * time.Second)
	}
}

func consumeMessages() {
	// We'll subscribe to two topics: our own chunk topic and the broadcast topic.
	topics := []string{chunkTopic, broadcastTopic}
	log.Printf("Chunk Server [%s] listening on topics: %v", chunkID, topics)
	for _, topic := range topics {
		go func(t string) {
			for {
				message, err := kafkaConsumer.ConsumeMessage(t)
				if err != nil {
					log.Printf("Error consuming from topic %s: %v", t, err)
					time.Sleep(2 * time.Second)
					continue
				}
				log.Printf("Received message on [%s]: %s", t, message)
				// If the message is from the broadcast topic, update our sync data.
				if t == broadcastTopic {
					var syncData struct {
						Leaderboard []LeaderboardEntry `json:"leaderboard"`
						Active      []LeaderboardEntry `json:"active"`
						Left        []LeaderboardEntry `json:"left"`
					}
					if err := json.Unmarshal([]byte(message), &syncData); err != nil {
						log.Printf("Error decoding sync message: %v", err)
					} else {
						localLeaderboard = syncData.Leaderboard
						localActivePlayers = syncData.Active
						localLeftPlayers = syncData.Left
						log.Printf("Updated local sync data from broadcast message")
					}
				}
			}
		}(topic)
	}
	select {} // Keep the goroutines running.
}

func main() {
	// Set kafkaBroker from environment variable.
	kafkaBroker = os.Getenv("KAFKA_BOOTSTRAP_SERVER")
	if kafkaBroker == "" {
		kafkaBroker = "kafka:9092"
	}

	// Generate the chunk server's unique ID.
	chunkID = generateChunkID()

	// Create our chunk topic using the unique chunkID.
	chunkTopic = fmt.Sprintf("central_to_chunk_%s", chunkID)
	log.Printf("Chunk ID: %s", chunkID)
	log.Printf("Chunk topic: %s", chunkTopic)

	// Initialize Kafka producer.
	producer, err := kafka.NewProducer(kafkaBroker)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka producer: %v", err)
	}
	kafkaProducer = producer

	// Create the Kafka topic for this chunk server.
	err = kafkaProducer.CreateTopic(chunkTopic)
	if err != nil {
		log.Fatalf("Failed to create Kafka topic: %v", err)
	}
	log.Printf("Kafka topic '%s' created", chunkTopic)

	// Initialize main Kafka consumer.
	consumer, err := kafka.NewConsumer(kafkaBroker, chunkID)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka consumer: %v", err)
	}
	kafkaConsumer = consumer

	// Register this chunk server with the central server via Kafka.
	registerChunkID()

	// Request the map from the central server.
	go requestMap()

	// Start consuming Kafka messages.
	go consumeMessages()

	// Start a ticker to broadcast game state to connected WebSocket clients.
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			websocket.BroadcastGameState()
		}
	}()

	// Periodically send player updates (as before).
	type PlayerUpdate struct {
		UserName string `json:"userName"`
		Score    int    `json:"score"`
		Status   string `json:"status"`
	}
	lastSentStates := make(map[string]PlayerUpdate)
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			players := playerstate.GetGameState()
			for _, ps := range players {
				update := PlayerUpdate{
					UserName: ps.ID,
					Score:    ps.Score,
					Status:   ps.Status,
				}
				last, ok := lastSentStates[ps.ID]
				if !ok || last.Score != update.Score || last.Status != update.Status {
					msgData, err := json.Marshal(update)
					if err != nil {
						log.Printf("Error marshaling player update: %v", err)
						continue
					}
					err = kafkaProducer.SendMessage(centralTopic, string(msgData))
					if err != nil {
						log.Printf("Error sending player update to central: %v", err)
						continue
					}
					lastSentStates[ps.ID] = update
					log.Printf("Sent update for player %s: %v", ps.ID, update)
				}
			}
		}
	}()

	// Graceful shutdown handling.
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down Chunk Server...")
		kafkaProducer.Close()
		kafkaConsumer.Close()
		os.Exit(0)
	}()

	r := setupRouter()
	r.Run(":8080")
}