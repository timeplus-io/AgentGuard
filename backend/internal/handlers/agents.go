package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/mock"
	"github.com/timeplus/agentshield/internal/models"
)

func ListAgents(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "15")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 15
	}

	agents := mock.Agents
	total := len(agents)

	start := (page - 1) * limit
	if start >= total {
		c.JSON(http.StatusOK, models.AgentListResponse{
			Agents: []models.Agent{},
			Total:  total,
			Page:   page,
			Limit:  limit,
		})
		return
	}
	end := start + limit
	if end > total {
		end = total
	}

	c.JSON(http.StatusOK, models.AgentListResponse{
		Agents: agents[start:end],
		Total:  total,
		Page:   page,
		Limit:  limit,
	})
}

func GetAgent(c *gin.Context) {
	id := c.Param("id")
	for _, a := range mock.Agents {
		if a.ID == id {
			c.JSON(http.StatusOK, a)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
}
