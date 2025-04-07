package main

import (
	"context"
	"encoding/json"
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
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Global variables and configurations.
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

// MongoDB Data Types.
type Chunk struct {
	X   int    `bson:"x"`
	Y   int    `bson:"y"`
	Url string `bson:"url"`
}

// Request/Response models (omitted for brevity; keep your existing models).
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

type DeleteChunkServerRequest struct {
	ChunkServerAddress string `json:"chunkServerAddress"`
	ChunkCoordinates   struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"chunkCoordinates"`
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
	r.Use(cors.Default())

	r.GET("/dbconn", func(c *gin.Context) {
		err := client.Ping(context.Background(), nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to connect to database"})
			return
		}
		c.String(http.StatusOK, "Able to connect to DB")
	})

	// Health check.
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

	// --- API Endpoints for the Central Server (users, scores, map, etc.)
	// (Omitted for brevity; keep your existing endpoints.)

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

		// Get random chunk to connect to
		collection = client.Database("game").Collection("chunks")
		ctxC, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		cursor, err := collection.Find(ctxC, bson.D{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to find chunk to assign to, error: %w", err)})
			return
		}
		var results []Chunk
		if err = cursor.All(ctxC, &results); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to find chunk to assign to, error: %w", err)})
			return
		}
		defer cursor.Close(ctx)

		if len(results) == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("No chunks available")})
			return
		}

		// Build the response object
		resp := CreateUserResponse{
			UserName:           req.UserName,
			ChunkServerAddress: results[0].Url, 
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

	// POST endpoint to find a chunk server at specific chunk coordinates
	// May require bringing up a new chunk server
	r.POST("/get-chunk-server", func(c *gin.Context) {
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

	// Temporary DELETE request to bring down a chunk server.
	// Should be replaced with communication from Kafka when a chunk should be brought down.
	r.DELETE("/delete-chunk-server", func(c *gin.Context) {
		var req DeleteChunkServerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fmt.Print(err.Error())
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		err := deleteChunk(c, req.ChunkCoordinates.X, req.ChunkCoordinates.Y, req.ChunkServerAddress)
		if err != nil {
			fmt.Print(err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Errorf("error, failed to delete chunk server %v, %v: %w", req.ChunkCoordinates.X, req.ChunkCoordinates.Y, err).Error()})
			return
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
	// List all registered chunk servers.
	r.GET("/list_chunks", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"chunk_servers": chunkServers})
	})

	return r
}

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

// broadcastSync queries the database for the leaderboard, active, and left players
// and then broadcasts a sync message to chunk servers every second.
func broadcastSync(ctx context.Context) {
	collection := client.Database("game").Collection("users")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C

		// Query leaderboard (top 10 by highScore).
		ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
		opts := options.Find().SetSort(bson.D{{Key: "highScore", Value: -1}}).SetLimit(10)
		cursor, err := collection.Find(ctxTimeout, bson.M{}, opts)
		var leaderboard []LeaderboardEntry
		if err == nil {
			for cursor.Next(ctxTimeout) {
				var doc struct {
					UserName  string `bson:"userName"`
					HighScore int    `bson:"highScore"`
				}
				if err := cursor.Decode(&doc); err != nil {
					continue
				}
				leaderboard = append(leaderboard, LeaderboardEntry{
					UserName: doc.UserName,
					Score:    doc.HighScore,
				})
			}
			cursor.Close(ctxTimeout)
		}
		cancel()

		// Query active players.
		ctxTimeout, cancel = context.WithTimeout(ctx, 5*time.Second)
		cursor, err = collection.Find(ctxTimeout, bson.M{"status": "active"})
		var activePlayers []LeaderboardEntry
		if err == nil {
			for cursor.Next(ctxTimeout) {
				var doc struct {
					UserName string `bson:"userName"`
					Score    int    `bson:"score"`
				}
				if err := cursor.Decode(&doc); err != nil {
					continue
				}
				activePlayers = append(activePlayers, LeaderboardEntry{
					UserName: doc.UserName,
					Score:    doc.Score,
				})
			}
			cursor.Close(ctxTimeout)
		}
		cancel()

		// Query left players.
		ctxTimeout, cancel = context.WithTimeout(ctx, 5*time.Second)
		cursor, err = collection.Find(ctxTimeout, bson.M{"status": "left"})
		var leftPlayers []LeaderboardEntry
		if err == nil {
			for cursor.Next(ctxTimeout) {
				var doc struct {
					UserName string `bson:"userName"`
					Score    int    `bson:"score"`
				}
				if err := cursor.Decode(&doc); err != nil {
					continue
				}
				leftPlayers = append(leftPlayers, LeaderboardEntry{
					UserName: doc.UserName,
					Score:    doc.Score,
				})
			}
			cursor.Close(ctxTimeout)
		}
		cancel()

		// Construct the sync message.
		syncMessage := map[string]interface{}{
			"leaderboard": leaderboard,
			"active":      activePlayers,
			"left":        leftPlayers,
		}
		msgData, err := json.Marshal(syncMessage)
		if err != nil {
			log.Printf("Error marshaling sync message: %v", err)
			continue
		}

		// Send the sync message to the broadcast topic.
		err = kafkaProducer.SendMessage(broadcastTopic, string(msgData))
		if err != nil {
			log.Printf("Error sending sync message: %v", err)
		} else {
			log.Printf("Broadcasted sync message: %s", string(msgData))
		}
	}
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

		// Check for registration messages.
		if strings.HasPrefix(message, "REGISTER:") {
			regID := strings.TrimPrefix(message, "REGISTER:")
			// Avoid duplicate registrations.
			alreadyRegistered := false
			for _, id := range chunkServers {
				if id == regID {
					alreadyRegistered = true
					break
				}
			}
			if !alreadyRegistered {
				chunkServers = append(chunkServers, regID)
				log.Printf("Registered new chunk server via Kafka: %s", regID)
			} else {
				log.Printf("Chunk server %s already registered", regID)
			}
			continue
		}

		// Check for map requests.
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

func deleteChunk(ctx context.Context, x, y int, url string) error {
	collection := client.Database("game").Collection("chunks")
	ctxC, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Check if the chunk server actually exists
	cursor, err := collection.Find(ctxC, bson.D{{Key: "x", Value: x}, {Key: "y", Value: y}, {Key: "url", Value: url}})
	if err != nil {
		return err
	}
	var results []Chunk
	if err = cursor.All(ctxC, &results); err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("a chunk server does not exist with coordinates (%v,%v) and url %v, so it cannot be deleted.", x, y, url)
	}

	// Delete the chunk server from records
	_, err = collection.DeleteOne(ctxC, bson.D{{Key: "x", Value: x}, {Key: "y", Value: y}, {Key: "url", Value: url}})
	if err != nil {
		return err
	}

	// Turn the chunk server down
	return k8s.DeleteChunkServer(ctx, clusterClients, x, y, url)
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
	log.Printf("Chunk results from DB are %v", results)

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
	return chunk.Url, nil
}

func addChunkToDB(ctx context.Context, x, y int, url string) error {
	collection := client.Database("game").Collection("chunks")
	ctxC, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	chunk := Chunk{
		X:   x,
		Y:   y,
		Url: url,
	}
	_, err := collection.InsertOne(ctxC, chunk)
	return err
}

func clearChunkServers(ctx context.Context) error {
	collection := client.Database("game").Collection("chunks")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := collection.DeleteMany(ctx, bson.D{})
	return err
}

func initialChunkServers(ctx context.Context) error {
	err := clearChunkServers(ctx)
	if err != nil {
		return err
	}

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
		n++
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
	if err != nil {
		log.Fatalf("Error connecting to cluster clients: %v", err)
	}
	log.Println("Created clients for all clusters")

	// Initialise chunk servers with coordinates
	initialChunkServers(ctx)
	log.Println("Initialised chunk servers")

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

	// Start a goroutine to broadcast leaderboard, active, and left player sync messages every second.
	go broadcastSync(ctx)

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