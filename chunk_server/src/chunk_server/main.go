package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Cormuckle/dist_systems_group_M/chunk_server/kafka"
	"github.com/Cormuckle/dist_systems_group_M/chunk_server/websocket"
	"github.com/gin-gonic/gin"
	"github.com/gin-contrib/cors"
)

// Global variables.
var (
	kafkaProducer  *kafka.Producer
	kafkaConsumer  *kafka.Consumer
	db             = make(map[string]string)
	chunkID        string
	chunkTopic     string
	centralTopic   = "chunk_to_central"
	broadcastTopic = "central_to_chunk_broadcast"
)

// generateChunkID generates a unique chunk server ID based on hostname or timestamp.
func generateChunkID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = fmt.Sprintf("chunk_server_%d", time.Now().Unix())
	}
	return hostname
}

// setupRouter sets up HTTP routes for the chunk server.
func setupRouter() *gin.Engine {
	r := gin.Default()
    r.Use(cors.Default())
	// Health check endpoint.
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// Get user value.
	r.GET("/user/:name", func(c *gin.Context) {
		user := c.Param("name")
		value, ok := db[user]
		if ok {
			c.JSON(http.StatusOK, gin.H{"user": user, "value": value})
		} else {
			c.JSON(http.StatusOK, gin.H{"user": user, "status": "no value"})
		}
	})

	// Authorized group (uses gin.BasicAuth() middleware).
	authorized := r.Group("/", gin.BasicAuth(gin.Accounts{
		"foo":  "bar", // user: foo, password: bar
		"manu": "123", // user: manu, password: 123
	}))

	authorized.POST("admin", func(c *gin.Context) {
		user := c.MustGet(gin.AuthUserKey).(string)
		var json struct {
			Value string `json:"value" binding:"required"`
		}
		if c.Bind(&json) == nil {
			db[user] = json.Value
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		}
	})

	// Endpoint to send a message to the central server.
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

	// New /getMap endpoint.
	// Need to load this map in from central server
	r.GET("/getMap", func(c *gin.Context) {
		// Define the map data as a 16x16 grid.
		mapData := [][]string{
			{"1", "1", "1", "1", "1", "1", "1", "0", "0", "1", "1", "1", "1", "1", "1", "1"},
			{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
			{"1", "0", "1", "1", "1", "0", "1", "1", "1", "1", "0", "1", "1", "1", "0", "1"},
			{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0"},
			{"1", "0", "1", "1", "0", "1", "1", "1", "1", "1", "1", "0", "1", "1", "0", "1"},
			{"1", "0", "1", "0", "0", "1", "0", "0", "0", "0", "1", "0", "0", "1", "0", "1"},
			{"1", "0", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "0", "1"},
			{"0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0"},
			{"0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0"},
			{"1", "0", "1", "1", "0", "1", "1", "1", "1", "1", "1", "0", "1", "1", "0", "1"},
			{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
			{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
			{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
			{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
			{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
			{"1", "1", "1", "1", "1", "1", "1", "0", "0", "1", "1", "1", "1", "1", "1", "1"},
		}
		c.JSON(http.StatusOK, mapData)
	})

	return r
}

// consumeMessages continuously polls Kafka for messages.
func consumeMessages() {
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
			}
		}(topic)
	}

	select {} // Keep the goroutine running.
}

// notifyCentralServer sends a registration request to the central server.
func notifyCentralServer() error {
	centralServerURL := "http://central_server:8080/register_chunk"
	payload := fmt.Sprintf(`{"chunk_id": "%s"}`, chunkID)
	req, err := http.NewRequest("POST", centralServerURL, bytes.NewBuffer([]byte(payload)))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to notify central server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		log.Println("Chunk ID successfully registered with central server")
		return nil
	}
	return fmt.Errorf("failed to register chunk with central server, status: %d", resp.StatusCode)
}

func main() {
	chunkID = generateChunkID()
	chunkTopic = fmt.Sprintf("central_to_chunk_%s", chunkID)
	log.Printf("Chunk ID: %s", chunkID)
	log.Printf("Chunk topic: %s", chunkTopic)

	kafkaBroker := os.Getenv("KAFKA_BOOTSTRAP_SERVER")
	if kafkaBroker == "" {
		kafkaBroker = "kafka:9092"
	}

	producer, err := kafka.NewProducer(kafkaBroker)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka producer: %v", err)
	}
	kafkaProducer = producer

	err = kafkaProducer.CreateTopic(chunkTopic)
	if err != nil {
		log.Fatalf("Failed to create Kafka topic: %v", err)
	}
	log.Printf("Kafka topic '%s' created", chunkTopic)

	consumer, err := kafka.NewConsumer(kafkaBroker, chunkID)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka consumer: %v", err)
	}
	kafkaConsumer = consumer

	if err := notifyCentralServer(); err != nil {
		log.Fatalf("Registration failed: %v", err)
	}

	go consumeMessages()

	// Graceful shutdown handling.
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down Chunk Server...")
		producer.Close()
		consumer.Close()
		os.Exit(0)
	}()

	r := setupRouter()
	r.Run(":8080")
}