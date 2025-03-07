package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/Cormuckle/dist_systems_group_M/central_server/kafka"
)

// SetupRoutes registers your HTTP endpoints.
func SetupRoutes(r *gin.Engine) {
	// Endpoint to produce a message to Kafka
	r.POST("/produce", func(c *gin.Context) {
		var req struct {
			Message string `json:"message" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Change broker address and topic as needed
		err := kafka.ProduceMessage("10.6.45.153:30092", "test-topic", req.Message)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "message produced"})
	})
}