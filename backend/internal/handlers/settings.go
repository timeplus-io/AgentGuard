package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/mock"
	"github.com/timeplus/agentshield/internal/models"
)

// mutableSettings is a package-level copy that persists updates during the server session
var mutableSettings models.SettingsResponse

func init() {
	mutableSettings = mock.MockSettings
	// deep copy integrations and users slices
	mutableSettings.Integrations = make([]models.Integration, len(mock.MockSettings.Integrations))
	copy(mutableSettings.Integrations, mock.MockSettings.Integrations)
	mutableSettings.Users = make([]models.User, len(mock.MockSettings.Users))
	copy(mutableSettings.Users, mock.MockSettings.Users)
}

func GetSettings(c *gin.Context) {
	c.JSON(http.StatusOK, mutableSettings)
}

func UpdateSettings(c *gin.Context) {
	var patch struct {
		OrgName        *string               `json:"org_name"`
		OtelEndpoint   *string               `json:"otel_endpoint"`
		RetentionDays  *int                  `json:"retention_days"`
		ContentCapture *bool                 `json:"content_capture"`
		Integrations   []models.Integration  `json:"integrations"`
		Users          []models.User         `json:"users"`
	}

	if err := c.ShouldBindJSON(&patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if patch.OrgName != nil {
		mutableSettings.OrgName = *patch.OrgName
	}
	if patch.OtelEndpoint != nil {
		mutableSettings.OtelEndpoint = *patch.OtelEndpoint
	}
	if patch.RetentionDays != nil {
		mutableSettings.RetentionDays = *patch.RetentionDays
	}
	if patch.ContentCapture != nil {
		mutableSettings.ContentCapture = *patch.ContentCapture
	}
	if patch.Integrations != nil {
		mutableSettings.Integrations = patch.Integrations
	}
	if patch.Users != nil {
		mutableSettings.Users = patch.Users
	}

	c.JSON(http.StatusOK, mutableSettings)
}
