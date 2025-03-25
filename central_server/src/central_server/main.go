package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Cormuckle/dist_systems_group_M/central_server/k8s"
	"github.com/Cormuckle/dist_systems_group_M/central_server/kafka"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	client          *mongo.Client
	mongoURI        = os.Getenv("MONGO_URI")
	kubeconfigPaths = os.Getenv("KUBECONFIGS")
	kubeconfigs     []string
	db              = make(map[string]string)
	kafkaProducer   *kafka.Producer
	kafkaConsumer   *kafka.Consumer
	broadcastTopic  = "central_to_chunk_broadcast"
	chunkToCentral  = "chunk_to_central"
	chunkServers    []string // Stores registered chunk server IDs
	clusterClients  []*k8s.ClusterClient
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

// MongoDB Data Types
type Chunk struct {
	x   int    `bson:"x"`
	y   int    `bson:"y"`
	url string `bson:"url"`
}

// Request/Response models
type CreateUserRequest struct {
	UserName   string `json:"userName" binding:"required"`
	PacmanBody string `json:"pacmanBody"`
}

type CreateUserResponse struct {
	UserName           string `json:"userName"`
	ChunkServerAddress string `json:"chunkServerAddress"`
	SpawnCoordinates   struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"spawnCoordinates"`
}

type GetRandomChunkServerRequest struct {
	UserName string `json:"userName" binding:"required"`
}

type GetRandomChunkServerResponse struct {
	ChunkServerAddress string `json:"chunkServerAddress"`
	TileCoordinates    struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"tileCoordinates"`
}

type NewChunkServerRequest struct {
	ChunkCoordinates struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"chunkCoordinates"`
}

type NewChunkServerResponse struct {
	ChunkServerAddress string `json:"chunkServerAddress"`
}

type UpdateScoreRequest struct {
	UserName string `json:"userName" binding:"required"`
	Score    int    `json:"score" binding:"required"`
}

type LeaderboardEntry struct {
	UserName string `json:"userName"`
	Score    int    `json:"score"`
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

	// ------------------------------- API Endpoints for the Central Server

	// create new user
	r.POST("/users", func(c *gin.Context) {
		var req CreateUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if username exists in the in-memory map as a simple cache/duplicate check
		if _, exists := db[req.UserName]; exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "username in use"})
			return
		}

		userDoc := gin.H{
			"userName":   req.UserName,
			"pacmanBody": req.PacmanBody,
			"createdAt":  time.Now(),
			"score":      0,
		}

		// Insert the document into the "users" collection
		collection := client.Database("game").Collection("users")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := collection.InsertOne(ctx, userDoc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to insert user into database"})
			return
		}

		db[req.UserName] = req.PacmanBody

		// Build the response object
		resp := CreateUserResponse{
			UserName:           req.UserName,
			ChunkServerAddress: "http://chunkserver.example.com", // Placeholder address
		}
		resp.SpawnCoordinates.X = 100.0
		resp.SpawnCoordinates.Y = 200.0

		c.JSON(http.StatusCreated, resp)
	})

	// PUT endpoint to update a user's score
	r.PUT("/scores", func(c *gin.Context) {
		var req UpdateScoreRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		collection := client.Database("game").Collection("users")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		filter := bson.M{"userName": req.UserName}
		update := bson.M{"$max": bson.M{"highScore": req.Score}}

		result, err := collection.UpdateOne(ctx, filter, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update score"})
			return
		}
		if result.MatchedCount == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "score updated"})
	})

	// GET endpoint to retrieve the top 10 leaderboard entries
	r.GET("/leaderboard", func(c *gin.Context) {
		collection := client.Database("game").Collection("users")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Set up find options to sort by highScore descending and limit to 10 results.
		opts := options.Find().SetSort(bson.D{{Key: "highScore", Value: -1}}).SetLimit(10)
		cursor, err := collection.Find(ctx, bson.M{}, opts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error retrieving leaderboard"})
			return
		}
		defer cursor.Close(ctx)

		var leaderboard []LeaderboardEntry
		for cursor.Next(ctx) {
			var userDoc struct {
				UserName  string `bson:"userName"`
				HighScore int    `bson:"highScore"`
			}
			if err := cursor.Decode(&userDoc); err != nil {
				continue
			}
			leaderboard = append(leaderboard, LeaderboardEntry{
				UserName: userDoc.UserName,
				Score:    userDoc.HighScore,
			})
		}
		c.JSON(http.StatusOK, leaderboard)
	})

	// GET endpoint to find a chunk server at specific chunk coordinates
	// May require bringing up a new chunk server
	r.GET("/get-chunk-server", func(c *gin.Context) {
		var req NewChunkServerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fmt.Print(err.Error())
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		url, err := getChunk(c, req.ChunkCoordinates.X, req.ChunkCoordinates.Y)
		if err != nil {
			fmt.Print(err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Errorf("error, failed to find chunk server %v, %v: %w", req.ChunkCoordinates.X, req.ChunkCoordinates.Y, err).Error()})
			return
		}

		c.JSON(http.StatusOK, NewChunkServerResponse{
			ChunkServerAddress: url,
		})
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

func getChunk(ctx context.Context, x, y int) (string, error) {
	collection := client.Database("game").Collection("chunks")
	ctxC, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cursor, err := collection.Find(ctxC, bson.D{{Key: "x", Value: x}, {Key: "y", Value: y}})
	if err != nil {
		return "", err
	}
	var results []Chunk
	if err = cursor.All(ctxC, &results); err != nil {
		return "", err
	}
	defer cursor.Close(ctx)

	if len(results) == 0 {
		if len(clusterClients) == 0 {
			log.Printf("No clusterclients currently - kubeconfigs are %v", kubeconfigs)
			clusterClients, err = k8s.KubeClients(kubeconfigs)
			if err != nil {
				return "", err
			}
		}
		url, err := k8s.NewChunkServer(ctx, clusterClients)
		if err != nil {
			return "", err
		}
		err = addChunkToDB(ctx, x, y, url)
		if err != nil {
			return "", err
		}
		return url, nil
	}
	chunk := results[0]
	return chunk.url, nil
}

func addChunkToDB(ctx context.Context, x, y int, url string) error {
	collection := client.Database("game").Collection("chunks")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Add chunk to DB
	chunk := Chunk{
		x:   x,
		y:   y,
		url: url,
	}
	_, err := collection.InsertOne(ctx, chunk)
	if err != nil {
		return err
	}
	return nil
}

func initialChunkServers(ctx context.Context) error {
	urls, err := k8s.GetCurrentChunkServerUrls(ctx, clusterClients)
	if err != nil {
		return err
	}

	n := 1
	i := 0
	var coords [][2]int
	for i < len(urls) {
		for m := 1; m <= n; m++ {
			if m == n {
				coords = append(coords, [][2]int{
					{m, m}, {-m, m}, {m, -m}, {-m, -m},
				}...)
				i += 4
			} else {
				coords = append(coords, [][2]int{
					{n, m}, {-n, m}, {n, -m}, {-n, -m},
					{m, n}, {-m, n}, {m, -n}, {-m, -n},
				}...)
				i += 8
			}
		}
	}

	for i, url := range urls {
		err := addChunkToDB(ctx, coords[i][0], coords[i][1], url)
		if err != nil {
			fmt.Printf("Failed to write chunk %s to DB: %v", url, err)
		}
	}
	return nil
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

	// Create k8s clients for chunk clusters
	kubeconfigs = strings.Split(kubeconfigPaths, ",")
	clusterClients, err = k8s.KubeClients(kubeconfigs)
	if err != nil {
		log.Fatalf("Error connecting to cluster clients: %v", err)
	}

	// Initialise chunk servers with coordinates
	initialChunkServers(ctx)

	/*
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
	*/

	// Start HTTP server
	r := setupRouter()
	r.Run(":8080")
}
