package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"bytes"

	"github.com/Cormuckle/dist_systems_group_M/chunk_server/kafka"
	"github.com/gin-gonic/gin"
)

var (
	kafkaProducer  *kafka.Producer
	kafkaConsumer  *kafka.Consumer
	chunkID        string
	chunkTopic     string
	centralTopic   = "chunk_to_central"
	broadcastTopic = "central_to_chunk_broadcast"
)

// generateChunkID generates a unique chunk server ID based on hostname or timestamp
func generateChunkID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = fmt.Sprintf("chunk_server_%d", time.Now().Unix())
	}
	return hostname
}

// setupRouter sets up HTTP routes for the chunk server
func setupRouter() *gin.Engine {
	r := gin.Default()

	// Health check endpoint
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// Send a message to the central server
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

	return r
}

// consumeMessages listens for messages from Kafka topics
func consumeMessages() {
	topics := []string{broadcastTopic, chunkTopic}
	log.Printf("Chunk Server [%s] listening on topics: %v", chunkID, topics)

	for {
		message, topic, err := kafkaConsumer.ConsumeMultiTopic(topics)
		if err != nil {
			log.Printf("Error consuming from Kafka: %v\n", err)
			time.Sleep(2 * time.Second) // Retry after 2 seconds
			continue
		}
		log.Printf("Received message on [%s]: %s\n", topic, message)
	}
}

func notifyCentralServer() {
	centralServerURL := "http://central_server:8080/register_chunk"

	payload := fmt.Sprintf(`{"chunk_id": "%s"}`, chunkID)
	req, err := http.NewRequest("POST", centralServerURL, bytes.NewBuffer([]byte(payload)))
	if err != nil {
		log.Printf("Failed to create request to central server: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to notify central server: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Println("Chunk ID successfully registered with central server")
	} else {
		log.Printf("Failed to register chunk with central server, status: %d", resp.StatusCode)
	}
}

func main() {
	// Generate a unique chunk server ID and topic
	chunkID = generateChunkID()
	chunkTopic = fmt.Sprintf("central_to_chunk_%s", chunkID)

	// Kafka broker setup
	kafkaBroker := os.Getenv("KAFKA_BOOTSTRAP_SERVER")
	if kafkaBroker == "" {
		kafkaBroker = "kafka:9092" // Default Kafka broker address
	}

	// Initialize Kafka Producer
	producer, err := kafka.NewProducer(kafkaBroker)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka producer: %v", err)
	}
	kafkaProducer = producer

	// Create topic for this chunk server
	err = kafkaProducer.CreateTopic(chunkTopic)
	if err != nil {
		log.Fatalf("Failed to create Kafka topic: %v", err)
	}
	log.Printf("Kafka topic '%s' created", chunkTopic)

	// Initialize Kafka Consumer
	consumer, err := kafka.NewConsumer(kafkaBroker, chunkID)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka consumer: %v", err)
	}
	kafkaConsumer = consumer

	// Start consuming messages from the central server
	go consumeMessages()
    
	// notify central about working
	notifyCentralServer();

	// Graceful shutdown handling
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down Chunk Server...")

		// Cleanup Kafka connections
		producer.Close()
		consumer.Close()

		os.Exit(0)
	}()

	// Setup HTTP server
	r := setupRouter()
	r.Run(":8080")
}