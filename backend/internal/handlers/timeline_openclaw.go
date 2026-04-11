package handlers

import "github.com/timeplus/agentshield/internal/models"

// analyzeOpenClaw is a temporary stub — replaced by Task 4.
func analyzeOpenClaw(runID string, rows []map[string]any) []models.TraceSpan {
	return analyzeFallback(runID, rows)
}
