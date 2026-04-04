package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/mock"
	"github.com/timeplus/agentshield/internal/models"
)

func ListThreats(c *gin.Context) {
	severityFilter := c.Query("severity")
	categoryFilter := c.Query("category")
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "12")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 12
	}

	filtered := []models.Threat{}
	for _, t := range mock.Threats {
		if severityFilter != "" && t.Severity != severityFilter {
			continue
		}
		if categoryFilter != "" && t.Category != categoryFilter {
			continue
		}
		filtered = append(filtered, t)
	}

	total := len(filtered)
	start := (page - 1) * limit
	if start >= total {
		c.JSON(http.StatusOK, models.ThreatListResponse{
			Threats: []models.Threat{},
			Total:   total,
			Page:    page,
			Limit:   limit,
		})
		return
	}
	end := start + limit
	if end > total {
		end = total
	}

	c.JSON(http.StatusOK, models.ThreatListResponse{
		Threats: filtered[start:end],
		Total:   total,
		Page:    page,
		Limit:   limit,
	})
}
