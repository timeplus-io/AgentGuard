package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/models"
)

var availableTables = []models.TableInfo{
	{
		Name: "otel_traces",
		Type: "streaming",
		Columns: []string{"trace_id", "span_id", "parent_span_id", "name", "start_time", "end_time", "duration_ms", "status", "agent_id", "session_id"},
	},
	{
		Name: "otel_logs",
		Type: "streaming",
		Columns: []string{"timestamp", "severity", "body", "agent_id", "session_id", "trace_id", "span_id", "resource"},
	},
	{
		Name: "otel_metrics",
		Type: "streaming",
		Columns: []string{"timestamp", "metric_name", "value", "unit", "agent_id", "labels", "type"},
	},
	{
		Name: "hook_events",
		Type: "streaming",
		Columns: []string{
			"hook_name", "event_time", "session_id", "run_id", "agent_id",
			"deployment_id", "deployment_name", "session_key", "tool_name",
			"provider", "model", "hook_decision", "block_reason", "event_data",
		},
	},
}

func ListTables(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"tables": availableTables})
}

func RunQuery(c *gin.Context) {
	var body struct {
		Query string `json:"query"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Query) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query is required"})
		return
	}

	queryLower := strings.ToLower(body.Query)

	var result models.QueryResult

	switch {
	case strings.Contains(queryLower, "otel_traces"):
		result = mockTracesResult()
	case strings.Contains(queryLower, "otel_logs"):
		result = mockLogsResult()
	case strings.Contains(queryLower, "otel_metrics"):
		result = mockMetricsResult()
	case strings.Contains(queryLower, "hook_events"):
		result = mockHookEventsResult()
	default:
		result = mockGenericResult()
	}

	c.JSON(http.StatusOK, result)
}

func mockTracesResult() models.QueryResult {
	cols := []string{"trace_id", "span_id", "name", "start_time", "duration_ms", "status", "agent_id"}
	rows := []map[string]string{
		{"trace_id": "7f3a2b1c4d5e6f70", "span_id": "a1b2c3d4", "name": "llm.completion", "start_time": "2026-04-04T10:41:52Z", "duration_ms": "1243", "status": "OK", "agent_id": "AS-DELTA-9012"},
		{"trace_id": "8e4b3c2d5f6a7b81", "span_id": "b2c3d4e5", "name": "tool.call.bash", "start_time": "2026-04-04T10:41:44Z", "duration_ms": "87", "status": "ERROR", "agent_id": "AS-OMEGA-1102"},
		{"trace_id": "9d5c4d3e6a7b8c92", "span_id": "c3d4e5f6", "name": "memory.read", "start_time": "2026-04-04T10:41:38Z", "duration_ms": "12", "status": "OK", "agent_id": "AS-ETA-8834"},
		{"trace_id": "0e6d5e4f7b8c9d03", "span_id": "d4e5f6a7", "name": "llm.completion", "start_time": "2026-04-04T10:41:30Z", "duration_ms": "2108", "status": "OK", "agent_id": "AS-ALPHA-3030"},
		{"trace_id": "1f7e6f5a8c9d0e14", "span_id": "e5f6a7b8", "name": "tool.call.read_file", "start_time": "2026-04-04T10:41:22Z", "duration_ms": "34", "status": "OK", "agent_id": "AS-BETA-7721"},
		{"trace_id": "2a8f7a6b9d0e1f25", "span_id": "f6a7b8c9", "name": "hook.pre_llm", "start_time": "2026-04-04T10:41:15Z", "duration_ms": "5", "status": "OK", "agent_id": "AS-GAMMA-5503"},
		{"trace_id": "3b9a8b7c0e1f2a36", "span_id": "a7b8c9d0", "name": "llm.completion", "start_time": "2026-04-04T10:41:08Z", "duration_ms": "1891", "status": "OK", "agent_id": "AS-LAMBDA-4456"},
	}
	return models.QueryResult{Columns: cols, Rows: rows, Total: len(rows)}
}

func mockLogsResult() models.QueryResult {
	cols := []string{"timestamp", "severity", "body", "agent_id", "session_id"}
	rows := []map[string]string{
		{"timestamp": "2026-04-04T10:42:00Z", "severity": "ERROR", "body": "Prompt injection detected in user message payload", "agent_id": "AS-OMEGA-1102", "session_id": "sess-9f3a2"},
		{"timestamp": "2026-04-04T10:41:55Z", "severity": "WARN", "body": "Token budget threshold reached at 85%", "agent_id": "AS-ZETA-2201", "session_id": "sess-6e3b1"},
		{"timestamp": "2026-04-04T10:41:48Z", "severity": "INFO", "body": "Agent startup complete, telemetry initialized", "agent_id": "AS-DELTA-9012", "session_id": "sess-1a4f7"},
		{"timestamp": "2026-04-04T10:41:40Z", "severity": "ERROR", "body": "PII detected in LLM response: SSN pattern matched", "agent_id": "AS-IOTA-3309", "session_id": "sess-7b1c4"},
		{"timestamp": "2026-04-04T10:41:33Z", "severity": "INFO", "body": "Rule pack 'Prompt Injection Shield' evaluation: PASS", "agent_id": "AS-BETA-7721", "session_id": "sess-3b9c6"},
		{"timestamp": "2026-04-04T10:41:25Z", "severity": "WARN", "body": "Slow tool execution detected: bash took 4200ms", "agent_id": "AS-SIGMA-4421", "session_id": "sess-2d8e9"},
		{"timestamp": "2026-04-04T10:41:18Z", "severity": "INFO", "body": "Memory write completed: 3 new embeddings stored", "agent_id": "AS-ETA-8834", "session_id": "sess-4f7a3"},
		{"timestamp": "2026-04-04T10:41:10Z", "severity": "ERROR", "body": "Supply chain alert: suspicious package hash mismatch", "agent_id": "AS-GAMMA-5503", "session_id": "sess-8c2d5"},
	}
	return models.QueryResult{Columns: cols, Rows: rows, Total: len(rows)}
}

func mockMetricsResult() models.QueryResult {
	cols := []string{"timestamp", "metric_name", "value", "unit", "agent_id"}
	rows := []map[string]string{
		{"timestamp": "2026-04-04T10:42:00Z", "metric_name": "llm.tokens.input", "value": "1240", "unit": "tokens", "agent_id": "AS-DELTA-9012"},
		{"timestamp": "2026-04-04T10:42:00Z", "metric_name": "llm.tokens.output", "value": "420", "unit": "tokens", "agent_id": "AS-DELTA-9012"},
		{"timestamp": "2026-04-04T10:42:00Z", "metric_name": "agent.throughput", "value": "1240", "unit": "req/min", "agent_id": "AS-DELTA-9012"},
		{"timestamp": "2026-04-04T10:42:00Z", "metric_name": "agent.health_index", "value": "98", "unit": "score", "agent_id": "AS-DELTA-9012"},
		{"timestamp": "2026-04-04T10:42:00Z", "metric_name": "llm.latency.p99", "value": "2341", "unit": "ms", "agent_id": "AS-ALPHA-3030"},
		{"timestamp": "2026-04-04T10:42:00Z", "metric_name": "llm.tokens.input", "value": "3892", "unit": "tokens", "agent_id": "AS-ALPHA-3030"},
		{"timestamp": "2026-04-04T10:42:00Z", "metric_name": "agent.cost.usd", "value": "0.0842", "unit": "USD", "agent_id": "AS-SIGMA-4421"},
		{"timestamp": "2026-04-04T10:42:00Z", "metric_name": "hook.evaluation.latency", "value": "4.7", "unit": "ms", "agent_id": "AS-GAMMA-5503"},
		{"timestamp": "2026-04-04T10:42:00Z", "metric_name": "cache.hit_rate", "value": "0.67", "unit": "ratio", "agent_id": "AS-ETA-8834"},
	}
	return models.QueryResult{Columns: cols, Rows: rows, Total: len(rows)}
}

func mockHookEventsResult() models.QueryResult {
	cols := []string{"event_id", "timestamp", "hook_type", "agent_id", "action_taken", "severity"}
	rows := []map[string]string{
		{"event_id": "hev-001", "timestamp": "2026-04-04T10:42:00Z", "hook_type": "pre_llm", "agent_id": "AS-OMEGA-1102", "action_taken": "BLOCKED", "severity": "critical"},
		{"event_id": "hev-002", "timestamp": "2026-04-04T10:41:50Z", "hook_type": "post_llm", "agent_id": "AS-IOTA-3309", "action_taken": "BLOCKED", "severity": "critical"},
		{"event_id": "hev-003", "timestamp": "2026-04-04T10:41:40Z", "hook_type": "pre_tool", "agent_id": "AS-SIGMA-4421", "action_taken": "ALLOWED", "severity": "info"},
		{"event_id": "hev-004", "timestamp": "2026-04-04T10:41:30Z", "hook_type": "pre_llm", "agent_id": "AS-DELTA-9012", "action_taken": "MODIFIED", "severity": "warning"},
		{"event_id": "hev-005", "timestamp": "2026-04-04T10:41:20Z", "hook_type": "post_tool", "agent_id": "AS-GAMMA-5503", "action_taken": "BLOCKED", "severity": "warning"},
		{"event_id": "hev-006", "timestamp": "2026-04-04T10:41:10Z", "hook_type": "pre_llm", "agent_id": "AS-BETA-7721", "action_taken": "ALLOWED", "severity": "info"},
		{"event_id": "hev-007", "timestamp": "2026-04-04T10:41:00Z", "hook_type": "memory_write", "agent_id": "AS-ETA-8834", "action_taken": "ALLOWED", "severity": "info"},
		{"event_id": "hev-008", "timestamp": "2026-04-04T10:40:50Z", "hook_type": "pre_tool", "agent_id": "AS-ALPHA-3030", "action_taken": "ALLOWED", "severity": "info"},
		{"event_id": "hev-009", "timestamp": "2026-04-04T10:40:40Z", "hook_type": "post_llm", "agent_id": "AS-LAMBDA-4456", "action_taken": "MODIFIED", "severity": "warning"},
		{"event_id": "hev-010", "timestamp": "2026-04-04T10:40:30Z", "hook_type": "pre_llm", "agent_id": "AS-THETA-6612", "action_taken": "ALLOWED", "severity": "info"},
	}
	return models.QueryResult{Columns: cols, Rows: rows, Total: len(rows)}
}

func mockGenericResult() models.QueryResult {
	cols := []string{"id", "timestamp", "event", "source", "value"}
	rows := []map[string]string{
		{"id": "1", "timestamp": "2026-04-04T10:42:00Z", "event": "query_executed", "source": "agentshield", "value": "ok"},
		{"id": "2", "timestamp": "2026-04-04T10:41:55Z", "event": "rule_evaluated", "source": "agentshield", "value": "pass"},
		{"id": "3", "timestamp": "2026-04-04T10:41:50Z", "event": "threat_detected", "source": "agentshield", "value": "blocked"},
		{"id": "4", "timestamp": "2026-04-04T10:41:45Z", "event": "session_started", "source": "agentshield", "value": "ok"},
		{"id": "5", "timestamp": "2026-04-04T10:41:40Z", "event": "token_budget_check", "source": "agentshield", "value": "within_limit"},
	}
	return models.QueryResult{Columns: cols, Rows: rows, Total: len(rows)}
}
