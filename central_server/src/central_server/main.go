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
	x   int
	y   int
	url string
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
	return err
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