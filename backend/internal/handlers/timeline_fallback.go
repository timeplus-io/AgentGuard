package handlers

import (
	"encoding/json"
	"fmt"

	"github.com/timeplus/agentshield/internal/models"
)

// hookKind maps a hook name to a span kind for waterfall colouring.
func hookKind(hookName string) string {
	switch hookName {
	case "llm_input", "llm_output", "before_model_resolve", "before_prompt_build", "before_agent_reply":
		return "llm"
	case "before_tool_call", "after_tool_call", "tool_result_persist":
		return "tool"
	default:
		return "lifecycle"
	}
}

// analyzeFallback builds a flat waterfall from raw hook event rows using
// gap-fill timing: each event's duration is the gap to the next event.
// Used for unknown agent types and as a regression-safe fallback.
func analyzeFallback(runID string, rows []map[string]any) []models.TraceSpan {
	if len(rows) == 0 {
		return []models.TraceSpan{}
	}

	firstMs := parseTime(rows[0]["event_time"]).UnixMilli()
	lastMs := parseTime(rows[len(rows)-1]["event_time"]).UnixMilli()
	runDurationMs := lastMs - firstMs
	if runDurationMs <= 0 {
		runDurationMs = 1
	}

	spans := make([]models.TraceSpan, 0, len(rows)+1)

	// Root span covers the full run window.
	spans = append(spans, models.TraceSpan{
		SpanID:     runID,
		ParentID:   "",
		Kind:       "run",
		Name:       "run",
		StartMs:    firstMs,
		DurationMs: runDurationMs,
		Status:     "ok",
		Attributes: map[string]string{},
	})

	// One child span per hook event; duration = gap to the next event.
	for i, row := range rows {
		hookName := parseStr(row["hook_name"])
		evtMs := parseTime(row["event_time"]).UnixMilli()

		var durationMs int64
		if i+1 < len(rows) {
			nextMs := parseTime(rows[i+1]["event_time"]).UnixMilli()
			durationMs = nextMs - evtMs
		} else {
			durationMs = lastMs - evtMs
		}
		if durationMs < 0 {
			durationMs = 0
		}

		status := "ok"
		hookDecision := parseStr(row["hook_decision"])
		if hookDecision == "block" {
			status = "blocked"
		}

		attrs := map[string]string{}
		if tn := parseStr(row["tool_name"]); tn != "" {
			attrs["tool_name"] = tn
		}
		if m := parseStr(row["model"]); m != "" {
			attrs["model"] = m
		}
		if p := parseStr(row["provider"]); p != "" {
			attrs["provider"] = p
		}
		if hookDecision != "" && hookDecision != "observe" {
			attrs["hook_decision"] = hookDecision
		}
		if br := parseStr(row["block_reason"]); br != "" {
			attrs["block_reason"] = br
		}
		if ed := parseStr(row["event_data"]); ed != "" {
			var data map[string]interface{}
			if json.Unmarshal([]byte(ed), &data) == nil {
				if errMsg, ok := data["error"].(string); ok && errMsg != "" {
					status = "error"
					attrs["error"] = errMsg
				}
				if dur, ok := data["durationMs"].(float64); ok {
					attrs["duration_ms"] = fmt.Sprintf("%.0f", dur)
				}
				if tcid, ok := data["toolCallId"].(string); ok {
					attrs["tool_call_id"] = tcid
				}
			}
		}

		spans = append(spans, models.TraceSpan{
			SpanID:     fmt.Sprintf("evt-%d", i),
			ParentID:   runID,
			Kind:       hookKind(hookName),
			Name:       hookName,
			StartMs:    evtMs,
			DurationMs: durationMs,
			Status:     status,
			Attributes: attrs,
		})
	}

	return spans
}
