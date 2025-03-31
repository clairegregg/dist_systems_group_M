package main

import (
	"context"
	"encoding/json" // <-- new import for JSON decoding
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
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
	x   int
	y   int
	url string
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
			return
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
	authorized := r.Group("/", gin.BasicAuth(gin.Accounts{
		"foo":  "bar", // user:foo password:bar
		"manu": "123", // user:manu password:123
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

	// ------------------------------- API Endpoints for the Central Server

	// Create new user.
	r.POST("/users", func(c *gin.Context) {
		var req CreateUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if _, exists := db[req.UserName]; exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "username in use"})
			return
		}
		userDoc := gin.H{
			"userName":   req.UserName,
			"pacmanBody": req.PacmanBody,
			"createdAt":  time.Now(),
			"score":      0,
			"highScore":  0,
			"status":     "active", // default status
		}
		collection := client.Database("game").Collection("users")
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := collection.InsertOne(ctxTimeout, userDoc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to insert user into database"})
			return
		}
		db[req.UserName] = req.PacmanBody
		resp := CreateUserResponse{
			UserName:           req.UserName,
			ChunkServerAddress: "http://chunkserver.example.com", // Placeholder
		}
		resp.SpawnCoordinates.X = 100
		resp.SpawnCoordinates.Y = 200
		c.JSON(http.StatusCreated, resp)
	})

	// PUT endpoint to update a user's score.
	r.PUT("/scores", func(c *gin.Context) {
		var req UpdateScoreRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		collection := client.Database("game").Collection("users")
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		filter := bson.M{"userName": req.UserName}
		update := bson.M{
			"$max": bson.M{"highScore": req.Score},
			"$set": bson.M{"score": req.Score},
		}
		result, err := collection.UpdateOne(ctxTimeout, filter, update)
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

	// GET endpoint to retrieve the top 10 leaderboard entries.
	r.GET("/leaderboard", func(c *gin.Context) {
		collection := client.Database("game").Collection("users")
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		opts := options.Find().SetSort(bson.D{{Key: "highScore", Value: -1}}).SetLimit(10)
		cursor, err := collection.Find(ctxTimeout, bson.M{}, opts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error retrieving leaderboard"})
			return
		}
		defer cursor.Close(ctxTimeout)
		var leaderboard []LeaderboardEntry
		for cursor.Next(ctxTimeout) {
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

	// GET active users.
	r.GET("/scores/active", func(c *gin.Context) {
		collection := client.Database("game").Collection("users")
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cursor, err := collection.Find(ctxTimeout, bson.M{"status": "active"})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error retrieving active users"})
			return
		}
		defer cursor.Close(ctxTimeout)
		var activeUsers []LeaderboardEntry
		for cursor.Next(ctxTimeout) {
			var userDoc struct {
				UserName string `bson:"userName"`
				Score    int    `bson:"score"`
			}
			if err := cursor.Decode(&userDoc); err != nil {
				continue
			}
			activeUsers = append(activeUsers, LeaderboardEntry{
				UserName: userDoc.UserName,
				Score:    userDoc.Score,
			})
		}
		c.JSON(http.StatusOK, activeUsers)
	})

	// GET left users.
	r.GET("/scores/left", func(c *gin.Context) {
		collection := client.Database("game").Collection("users")
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cursor, err := collection.Find(ctxTimeout, bson.M{"status": "left"})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error retrieving left users"})
			return
		}
		defer cursor.Close(ctxTimeout)
		var leftUsers []LeaderboardEntry
		for cursor.Next(ctxTimeout) {
			var userDoc struct {
				UserName string `bson:"userName"`
				Score    int    `bson:"score"`
			}
			if err := cursor.Decode(&userDoc); err != nil {
				continue
			}
			leftUsers = append(leftUsers, LeaderboardEntry{
				UserName: userDoc.UserName,
				Score:    userDoc.Score,
			})
		}
		c.JSON(http.StatusOK, leftUsers)
	})

	// ------------------ New APIs for Map Information ------------------

	// GET map endpoint: returns the map for a given coordinate.
	// If not found, falls back to the default coordinate "0,0".
	r.GET("/map", func(c *gin.Context) {
		coordinate := c.Query("coordinate")
		if coordinate == "" {
			coordinate = "0,0"
		}
		collection := client.Database("game").Collection("maps")
		var result struct {
			Coordinate string      `bson:"coordinate"`
			Map        [][]string  `bson:"map"`
		}
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := collection.FindOne(ctxTimeout, bson.M{"coordinate": coordinate}).Decode(&result)
		if err != nil {
			if coordinate != "0,0" {
				err = collection.FindOne(ctxTimeout, bson.M{"coordinate": "0,0"}).Decode(&result)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "map not found"})
					return
				}
			} else {
				c.JSON(http.StatusNotFound, gin.H{"error": "map not found"})
				return
			}
		}
		c.JSON(http.StatusOK, result)
	})

	// POST map endpoint: adds or updates a map for a given coordinate.
	// POST map endpoint: adds or updates a map for a given coordinate.
r.POST("/map", func(c *gin.Context) {
	var payload struct {
		Coordinate string      `json:"coordinate" binding:"required"`
		Map        [][]string  `json:"map" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	collection := client.Database("game").Collection("maps")
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	update := bson.M{
		"$set": bson.M{
			"map":       payload.Map,
			"updatedAt": time.Now(),
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := collection.UpdateOne(ctxTimeout, bson.M{"coordinate": payload.Coordinate}, update, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update map"})
		return
	}
	// Automatically send the updated map via Kafka if the topic exists.
	// Build the topic name by replacing commas with underscores.
	topic := fmt.Sprintf("central_to_chunk_%s", strings.ReplaceAll(payload.Coordinate, ",", "_"))
	respData, _ := json.Marshal(payload.Map)
	responseMsg := "MAP_RESPONSE:" + string(respData)
	err = kafkaProducer.SendMessage(topic, responseMsg)
	if err != nil {
		log.Printf("Error sending updated map to topic %s: %v", topic, err)
	} else {
		log.Printf("Sent updated map to topic %s", topic)
	}

	c.JSON(http.StatusOK, gin.H{"status": "map updated"})
})

	// ------------------ End Map APIs ------------------

	// GET endpoint to find a chunk server at specific chunk coordinates.
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

	// Send a broadcast message to all chunk servers.
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

	// Send a message to a specific chunk server.
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

	// Register a new chunk server.
	r.POST("/register_chunk", func(c *gin.Context) {
		var req struct {
			ChunkID string `json:"chunk_id" binding:"required"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}
		// Check if chunk ID is already registered.
		for _, id := range chunkServers {
			if id == req.ChunkID {
				c.JSON(http.StatusConflict, gin.H{"error": "Chunk ID already registered"})
				return
			}
		}
		// Add chunk ID to list.
		chunkServers = append(chunkServers, req.ChunkID)
		log.Printf("Registered new chunk server: %s", req.ChunkID)
		c.JSON(http.StatusOK, gin.H{"status": "Chunk registered"})
	})

	// List all registered chunk servers.
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

		if strings.HasPrefix(message, "GET_MAP:") {
			coord := strings.TrimPrefix(message, "GET_MAP:")
			coord = strings.Trim(coord, "\"")
			log.Printf("Central server: Received GET_MAP request for coordinate: %s", coord)
		
			collection := client.Database("game").Collection("maps")
			ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			var result struct {
				Coordinate string     `bson:"coordinate"`
				Map        [][]string `bson:"map"`
			}
			err := collection.FindOne(ctxTimeout, bson.M{"coordinate": coord}).Decode(&result)
			if err != nil {
				if coord != "0,0" {
					err = collection.FindOne(ctxTimeout, bson.M{"coordinate": "0,0"}).Decode(&result)
					if err != nil {
						result.Map = [][]string{{"0"}}
					}
				} else {
					result.Map = [][]string{{"0"}}
				}
			}
			respData, _ := json.Marshal(result.Map)
			responseTopic := fmt.Sprintf("central_to_chunk_%s", strings.ReplaceAll(coord, ",", "_"))
			responseMsg := "MAP_RESPONSE:" + string(respData)
			log.Printf("Central server: Sending map response on topic %s", responseTopic)
			err = kafkaProducer.SendMessage(responseTopic, responseMsg)
			if err != nil {
				log.Printf("Error sending map response: %v", err)
			}
			continue
		}

		// Attempt to parse the message as a user state update.
		var userUpdate struct {
			UserName string `json:"userName"`
			Score    int    `json:"score"`
			Status   string `json:"status"`
		}
		if err := json.Unmarshal([]byte(message), &userUpdate); err == nil {
			collection := client.Database("game").Collection("users")
			ctxC, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			filter := bson.M{"userName": userUpdate.UserName}
			update := bson.M{
				"$set": bson.M{
					"score":     userUpdate.Score,
					"status":    userUpdate.Status,
					"updatedAt": time.Now(),
				},
				"$max": bson.M{"highScore": userUpdate.Score},
			}
			opts := options.Update().SetUpsert(true)
			_, err := collection.UpdateOne(ctxC, filter, update, opts)
			if err != nil {
				log.Printf("Failed to update user %s: %v", userUpdate.UserName, err)
			} else {
				log.Printf("Upserted user %s with score %d and status %s", userUpdate.UserName, userUpdate.Score, userUpdate.Status)
			}
		}
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
		url, err := k8s.NewChunkServer(ctx, clusterClients, x, y)
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
	ctxC, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	chunk := Chunk{
		x:   x,
		y:   y,
		url: url,
	}
	_, err := collection.InsertOne(ctxC, chunk)
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

	// Connect to MongoDB.
	var err error
	client, err = connectToDB(ctx)
	if err != nil {
		log.Fatalf("Error connecting to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	// Create k8s clients for chunk clusters.
	kubeconfigs := strings.Split(kubeconfigPaths, ",")
	clusterClients, err = k8s.KubeClients(kubeconfigs)

	// Kafka setup.
	kafkaBroker := os.Getenv("KAFKA_BOOTSTRAP_SERVER")
	if kafkaBroker == "" {
		kafkaBroker = "kafka:9092"
	}

	// Initialize Kafka Producer.
	kafkaProducer, err = kafka.NewProducer([]string{kafkaBroker})
	if err != nil {
		log.Fatalf("Failed to initialize Kafka producer: %v", err)
	}

	// Initialize Kafka Consumer.
	kafkaConsumer, err = kafka.NewConsumer([]string{kafkaBroker}, "central-server-group")
	if err != nil {
		log.Fatalf("Failed to initialize Kafka consumer: %v", err)
	}

	// Start consuming messages from chunk servers.
	go consumeChunkMessages(ctx)

	// Graceful shutdown handling.
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down Central Server...")
		kafkaProducer.Close()
		kafkaConsumer.Close()
		cancel()
		os.Exit(0)
	}()

	// Start HTTP server.
	r := setupRouter()
	r.Run(":8080")
}