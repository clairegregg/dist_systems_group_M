package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Cormuckle/dist_systems_group_M/central_server/kafka"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	client        *mongo.Client
	mongoURI      = os.Getenv("MONGO_URI")
	db            = make(map[string]string)
	kafkaProducer *kafka.Producer
	kafkaConsumer *kafka.Consumer
	broadcastTopic = "central_to_chunk_broadcast"
	chunkToCentral = "chunk_to_central"
	chunkServers   []string // Stores registered chunk server IDs
)

// connectToDB initializes a MongoDB connection.
func connectToDB(ctx context.Context) (*mongo.Client, error) {
	if mongoURI == "" {
		log.Println("Warning: MONGO_URI is not set, using default localhost.")
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("unable to connect to MongoDB: %v", err)
	}

	log.Println("Connected to MongoDB")
	return client, nil
}

// setupRouter initializes HTTP routes.
func setupRouter() *gin.Engine {
	r := gin.Default()
    
	r.GET("/dbconn", func(c *gin.Context) {
		// Ping the database
		err := client.Ping(context.Background(), nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to connect to database"})
		}

		c.String(http.StatusOK, "Able to connect to DB")
	})
	// Health check
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	r.GET("/user/:name", func(c *gin.Context) {
		user := c.Params.ByName("name")
		value, ok := db[user]
		if ok {
			c.JSON(http.StatusOK, gin.H{"user": user, "value": value})
		} else {
			c.JSON(http.StatusOK, gin.H{"user": user, "status": "no value"})
		}
	})
    
	// Authorized group (uses gin.BasicAuth() middleware)
	// Same than:
	// authorized := r.Group("/")
	// authorized.Use(gin.BasicAuth(gin.Credentials{
	//	  "foo":  "bar",
	//	  "manu": "123",
	//}))
	authorized := r.Group("/", gin.BasicAuth(gin.Accounts{
		"foo":  "bar", // user:foo password:bar
		"manu": "123", // user:manu password:123
	}))

	authorized.POST("admin", func(c *gin.Context) {
		user := c.MustGet(gin.AuthUserKey).(string)

		// Parse JSON
		var json struct {
			Value string `json:"value" binding:"required"`
		}

		if c.Bind(&json) == nil {
			db[user] = json.Value
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		}
	})

	// Send a broadcast message to all chunk servers
	// This is just for tesing purpose but we can use this function iternally to communicate
	/* 
	
	curl -X POST http://<central_server_host>:8080/broadcast \
     -H "Content-Type: application/json" \
     -d '{"message": "Your broadcast message"}'
	
	*/
	r.POST("/broadcast", func(c *gin.Context) {
		var req struct {
			Message string `json:"message" binding:"required"`
		}

		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		err := kafkaProducer.SendMessage(broadcastTopic, req.Message)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send broadcast"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "Broadcast message sent"})
	})

	// Send a message to a specific chunk server
	/* 
	curl -X POST http://<central_server_host>:8080/send/<chunk_id> \
     -H "Content-Type: application/json" \
     -d '{"message": "Your message"}'
	
	*/
	r.POST("/send/:chunk_id", func(c *gin.Context) {
		chunkID := c.Param("chunk_id")
		chunkTopic := fmt.Sprintf("central_to_chunk_%s", chunkID)

		var req struct {
			Message string `json:"message" binding:"required"`
		}

		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		err := kafkaProducer.SendMessage(chunkTopic, req.Message)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send message"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "Message sent to chunk server"})
	})

	// Register a new chunk server
	/* 
	curl -X POST http://<central_server_host>:8080/register_chunk \
     -H "Content-Type: application/json" \
     -d '{"chunk_id": "chunk_server_123"}'
	
	*/
	r.POST("/register_chunk", func(c *gin.Context) {
		var req struct {
			ChunkID string `json:"chunk_id" binding:"required"`
		}

		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		// Check if chunk ID is already registered
		for _, id := range chunkServers {
			if id == req.ChunkID {
				c.JSON(http.StatusConflict, gin.H{"error": "Chunk ID already registered"})
				return
			}
		}

		// Add chunk ID to list
		chunkServers = append(chunkServers, req.ChunkID)
		log.Printf("Registered new chunk server: %s", req.ChunkID)

		c.JSON(http.StatusOK, gin.H{"status": "Chunk registered"})
	})
 
	// List all registered chunk servers
	/*
	curl -X GET http://<central_server_host>:8080/list_chunks
	*/
	r.GET("/list_chunks", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"chunk_servers": chunkServers})
	})

	return r
}

// consumeChunkMessages listens for messages from chunk servers.
func consumeChunkMessages(ctx context.Context) {
	for {
		message, err := kafkaConsumer.ConsumeMessage(chunkToCentral)
		if err != nil {
			log.Printf("Failed to consume message: %v\n", err)
			continue
		}
		log.Printf("Received message from chunk server: %s\n", message)
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to MongoDB
	var err error
	client, err = connectToDB(ctx)
	if err != nil {
		log.Fatalf("Error connecting to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	// Kafka setup
	kafkaBroker := os.Getenv("KAFKA_BOOTSTRAP_SERVER")
	if kafkaBroker == "" {
		kafkaBroker = "kafka:9092"
	}

	// Initialize Kafka Producer
	kafkaProducer, err = kafka.NewProducer([]string{kafkaBroker})
	if err != nil {
		log.Fatalf("Failed to initialize Kafka producer: %v", err)
	}

	// Initialize Kafka Consumer
	kafkaConsumer, err = kafka.NewConsumer([]string{kafkaBroker}, "central-server-group")
	if err != nil {
		log.Fatalf("Failed to initialize Kafka consumer: %v", err)
	}

	// Start consuming messages from chunk servers
	go consumeChunkMessages(ctx)

	// Graceful shutdown handling
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down Central Server...")

		// Cleanup Kafka connections
		kafkaProducer.Close()
		kafkaConsumer.Close()

		cancel()
		os.Exit(0)
	}()

	// Start HTTP server
	r := setupRouter()
	r.Run(":8080")
}