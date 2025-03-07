package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Cormuckle/dist_systems_group_M/central_server/handlers"
	"github.com/Cormuckle/dist_systems_group_M/central_server/kafka"
)

var client *mongo.Client
var mongoURI = os.Getenv("MONGO_URI")
var db = make(map[string]string)

func setupRouter() *gin.Engine {
	r := gin.Default()

	// Test MongoDB connection
	r.GET("/dbconn", func(c *gin.Context) {
		if err := client.Ping(context.Background(), nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to connect to database"})
			return
		}
		c.String(http.StatusOK, "Able to connect to DB")
	})

	// Ping test
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// Get user value (dummy in-memory store)
	r.GET("/user/:name", func(c *gin.Context) {
		user := c.Param("name")
		value, ok := db[user]
		if ok {
			c.JSON(http.StatusOK, gin.H{"user": user, "value": value})
		} else {
			c.JSON(http.StatusOK, gin.H{"user": user, "status": "no value"})
		}
	})

	// Authorized group using BasicAuth for admin updates
	authorized := r.Group("/", gin.BasicAuth(gin.Accounts{
		"foo":  "bar",
		"manu": "123",
	}))
	authorized.POST("/admin", func(c *gin.Context) {
		user := c.MustGet(gin.AuthUserKey).(string)
		var json struct {
			Value string `json:"value" binding:"required"`
		}
		if err := c.Bind(&json); err == nil {
			db[user] = json.Value
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		}
	})

	// Register Kafka routes (e.g. a produce endpoint)
	handlers.SetupRoutes(r)

	return r
}

func connectToDB(ctx context.Context) *mongo.Client {
	if mongoURI == "" {
		log.Println("MongoDB URI is not set.")
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("unable to connect to MongoDB: %v", err)
	}
	return client
}

func main() {
	ctx := context.Background()
	client = connectToDB(ctx)
	defer client.Disconnect(ctx)

	// Create Kafka topic "test-topic" using the Kafka admin function.
	if err := kafka.CreateTopicIfNotExists("10.6.45.153:30092", "test-topic", 1, 1); err != nil {
		log.Fatalf("failed to create topic: %v", err)
	}

	// Increase wait time to allow the topic to be fully registered.
	time.Sleep(10 * time.Second)

	// Start Kafka consumer in a separate goroutine with retry logic.
	go func() {
		for {
			log.Println("Starting Kafka consumer...")
			kafka.StartConsumer("10.6.45.153:30092", "test-topic", "go-gin-group")
			// If consumer exits, wait for 5 seconds before retrying.
			log.Println("Kafka consumer exited. Retrying in 5 seconds...")
			time.Sleep(10 * time.Second)
		}
	}()

	// Start the Gin HTTP server.
	r := setupRouter()
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}