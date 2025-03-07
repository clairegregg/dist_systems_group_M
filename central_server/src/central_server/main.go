package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var client *mongo.Client
var mongoURI = os.Getenv("MONGO_URI")
var db = make(map[string]string)

// Request/Response models
type CreateUserRequest struct {
	UserName   string `json:"userName" binding:"required"`
	PacmanBody string `json:"pacmanBody"`
}

type CreateUserResponse struct {
	UserName           string `json:"userName"`
	ChunkServerAddress string `json:"chunkServerAddress"`
	SpawnCoordinates   struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"spawnCoordinates"`
}

type GetChunkServerRequest struct {
	UserName string `json:"userName" binding:"required"`
}

type GetChunkServerResponse struct {
	ChunkServerAddress string `json:"chunkServerAddress"`
	TileCoordinates    struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"tileCoordinates"`
}

type NewChunkServerRequest struct {
	Quadrant string `json:"quadrant" binding:"required"`
}

type NewChunkServerResponse struct {
	ChunkServerAddress string `json:"chunkServerAddress"`
	Status             string `json:"status"`
}

type UpdateScoreRequest struct {
	UserName string `json:"userName" binding:"required"`
	Score    int    `json:"score" binding:"required"`
}

type LeaderboardEntry struct {
	UserName string `json:"userName"`
	Score    int    `json:"score"`
}

func setupRouter() *gin.Engine {
	// Disable Console Color
	// gin.DisableConsoleColor()
	r := gin.Default()

	// Test mongodb connection
	r.GET("/dbconn", func(c *gin.Context) {
		// Ping the database
		err := client.Ping(context.Background(), nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to connect to database"})
		}

		c.String(http.StatusOK, "Able to connect to DB")
	})

	// Ping test
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// Get user value
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

	/* example curl for /admin with basicauth header
	   Zm9vOmJhcg== is base64("foo:bar")

		curl -X POST \
	  	http://localhost:8080/admin \
	  	-H 'authorization: Basic Zm9vOmJhcg==' \
	  	-H 'content-type: application/json' \
	  	-d '{"value":"bar"}'
	*/
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

	return r
}

func connectToDB(ctx context.Context) *mongo.Client {
	if mongoURI == "" {
		log.Println("MongoDB URI is not set.")
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))

	if err != nil {
		log.Println(fmt.Errorf("unable to connect to MongoDB: %v", err))
	}

	return client
}

func main() {
	ctx := context.Background()
	client = connectToDB(ctx)
	defer client.Disconnect(ctx)
	r := setupRouter()
	// Listen and Server in 0.0.0.0:8080
	r.Run(":8080")
}
