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
	"github.com/gin-gonic/gin"
)

// Global variables.
var (
	kafkaProducer  *kafka.Producer
	kafkaConsumer  *kafka.Consumer
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

	// Health check endpoint.
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
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

	return r
}

// consumeMessages continuously polls Kafka for messages on the broadcast and chunk topics.
func consumeMessages() {
	topics := []string{chunkTopic, broadcastTopic}
	log.Printf("Chunk Server [%s] is now listening on topics: %v", chunkID, topics)

	// Launch a separate goroutine for each topic.
	for _, topic := range topics {
		go func(t string) {
			for {
				// ConsumeMessage should be implemented to consume from a single topic.
				message, err := kafkaConsumer.ConsumeMessage(t)
				if err != nil {
					log.Printf("Error consuming from topic %s: %v", t, err)
					time.Sleep(2 * time.Second) // Retry after 2 seconds.
					continue
				}
				log.Printf("Received message on [%s]: %s", t, message)
			}
		}(topic)
	}

	// Prevent main goroutine from exiting.
	select {}
}

// notifyCentralServer sends a registration request to the central server.
// It returns an error if registration fails.
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
	// Generate a unique chunk server ID and topic.
	chunkID = generateChunkID()
	chunkTopic = fmt.Sprintf("central_to_chunk_%s", chunkID)
	log.Printf("Chunk ID: %s", chunkID)
	log.Printf("Chunk topic: %s", chunkTopic)

	// Kafka broker setup.
	kafkaBroker := os.Getenv("KAFKA_BOOTSTRAP_SERVER")
	if kafkaBroker == "" {
		kafkaBroker = "kafka:9092" // Default Kafka broker address.
	}

	// Initialize Kafka Producer.
	producer, err := kafka.NewProducer(kafkaBroker)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka producer: %v", err)
	}
	kafkaProducer = producer

	// Create topic for this chunk server.
	err = kafkaProducer.CreateTopic(chunkTopic)
	if err != nil {
		log.Fatalf("Failed to create Kafka topic: %v", err)
	}
	log.Printf("Kafka topic '%s' created", chunkTopic)

	// Initialize Kafka Consumer.
	consumer, err := kafka.NewConsumer(kafkaBroker, chunkID)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka consumer: %v", err)
	}
	kafkaConsumer = consumer

	// Notify the central server of registration.
	if err := notifyCentralServer(); err != nil {
		log.Fatalf("Registration failed: %v", err)
	}

	// Once successfully registered, start consuming messages.
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

	// Start the HTTP server.
	r := setupRouter()
	r.Run(":8080")
}