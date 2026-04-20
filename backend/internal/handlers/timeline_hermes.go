package handlers

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/timeplus/agentguard/internal/models"
)

// hermesSpanName maps Hermes hook names to human-readable span names.
var hermesSpanName = map[string]string{
	"on_session_start":    "Session Start",
	"on_session_end":      "Session End",
	"on_session_finalize": "Session Finalize",
	"on_session_reset":    "Session Reset",
	"conversation_end":    "Conversation End",
	"pre_llm_call":        "LLM Turn",
	"post_llm_call":       "LLM Response",
	"pre_api_request":     "API Request",
	"post_api_request":    "API Response",
	"pre_tool_call":       "Tool Call",
	"post_tool_call":      "Tool Call",
}

func hermesHookName(hookName string) string {
	if n, ok := hermesSpanName[hookName]; ok {
		return n
	}
	return hookName
}

// analyzeHermes builds a hierarchical trace from Hermes plugin hook event rows.
// Structure: root run span → LLM turn spans → tool call spans (children of LLM turn).
// Token/latency attributes are sourced from post_api_request events.
func analyzeHermes(runID string, rows []map[string]any) []models.TraceSpan {
	if len(rows) == 0 {
		return []models.TraceSpan{{
			SpanID:     runID,
			ParentID:   "",
			Kind:       "run",
			Name:       "Agent Run",
			StartMs:    0,
			DurationMs: 0,
			Status:     "ok",
			Attributes: map[string]string{},
		}}
	}

	// Parse every row once.
	type parsedRow struct {
		raw      map[string]any
		evtMs    int64
		hookName string
		toolName string
		data     map[string]any
		decision string
		consumed bool
	}

	prows := make([]parsedRow, len(rows))
	for i, row := range rows {
		var data map[string]any
		if ed := parseStr(row["event_data"]); ed != "" {
			_ = json.Unmarshal([]byte(ed), &data)
		}
		prows[i] = parsedRow{
			raw:      row,
			evtMs:    parseTime(row["event_time"]).UnixMilli(),
			hookName: parseStr(row["hook_name"]),
			toolName: parseStr(row["tool_name"]),
			data:     data,
			decision: parseStr(row["hook_decision"]),
		}
	}

	firstMs := prows[0].evtMs
	lastMs := prows[len(prows)-1].evtMs

	// ── Root span ──────────────────────────────────────────────────────────────
	// Use first on_session_start → last on_session_end/on_session_finalize.
	rootStart, rootEnd := firstMs, lastMs
	for i := range prows {
		hn := prows[i].hookName
		if hn == "on_session_start" || hn == "on_session_reset" {
			rootStart = prows[i].evtMs
			prows[i].consumed = true
			break
		}
	}
	for i := len(prows) - 1; i >= 0; i-- {
		hn := prows[i].hookName
		if hn == "on_session_end" || hn == "on_session_finalize" {
			rootEnd = prows[i].evtMs
			prows[i].consumed = true
			break
		}
	}
	// Also consume any remaining session lifecycle events.
	for i := range prows {
		switch prows[i].hookName {
		case "on_session_start", "on_session_reset", "on_session_end", "on_session_finalize", "conversation_end":
			prows[i].consumed = true
		}
	}

	runDurationMs := rootEnd - rootStart
	if runDurationMs <= 0 {
		runDurationMs = 1
	}

	spans := []models.TraceSpan{{
		SpanID:     runID,
		ParentID:   "",
		Kind:       "run",
		Name:       "Agent Run",
		StartMs:    rootStart,
		DurationMs: runDurationMs,
		Status:     "ok",
		Attributes: map[string]string{},
	}}

	// ── Build post_tool_call lookup by tool_call_id ────────────────────────────
	postToolByID := map[string]int{} // tool_call_id → prows index
	for i, pr := range prows {
		if pr.hookName == "post_tool_call" {
			if tcID, ok := pr.data["tool_call_id"].(string); ok && tcID != "" {
				postToolByID[tcID] = i
			}
		}
	}

	// ── Build post_api_request lookup: list in order for index-based pairing ───
	// We associate them to LLM turns by falling within the pre→post_llm_call window.
	type apiEntry struct {
		idx  int
		data map[string]any
		raw  map[string]any
	}
	var postApiRows []apiEntry
	for i, pr := range prows {
		if pr.hookName == "post_api_request" {
			postApiRows = append(postApiRows, apiEntry{i, pr.data, pr.raw})
			prows[i].consumed = true
		}
		if pr.hookName == "pre_api_request" {
			prows[i].consumed = true
		}
	}

	// ── LLM turn spans ─────────────────────────────────────────────────────────
	llmSpanCount := 0
	for i := range prows {
		if prows[i].hookName != "pre_llm_call" {
			continue
		}
		prows[i].consumed = true
		llmStartMs := prows[i].evtMs
		llmSpanID := fmt.Sprintf("llm-%d", llmSpanCount)
		llmSpanCount++

		// Find the matching post_llm_call (next unconsumed one after this).
		llmEndMs := llmStartMs
		for j := i + 1; j < len(prows); j++ {
			if prows[j].hookName == "post_llm_call" && !prows[j].consumed {
				llmEndMs = prows[j].evtMs
				prows[j].consumed = true
				break
			}
		}

		// Gather attributes from the model/provider columns and from any
		// post_api_request that falls within this LLM window.
		model := parseStr(prows[i].raw["model"])
		provider := parseStr(prows[i].raw["provider"])
		llmAttrs := map[string]string{}
		if model != "" {
			llmAttrs["model"] = model
		}
		if provider != "" {
			llmAttrs["provider"] = provider
		}
		if msg, ok := prows[i].data["user_message"].(string); ok && msg != "" {
			if len(msg) > 200 {
				msg = msg[:200] + "…"
			}
			llmAttrs["user_message"] = msg
		}

		// Attach token metrics from post_api_request events in this window.
		for _, ae := range postApiRows {
			aeMs := prows[ae.idx].evtMs
			if aeMs < llmStartMs {
				continue
			}
			if llmEndMs > llmStartMs && aeMs > llmEndMs {
				continue
			}
			if m := parseStr(ae.raw["model"]); m != "" && llmAttrs["model"] == "" {
				llmAttrs["model"] = m
			}
			if p := parseStr(ae.raw["provider"]); p != "" && llmAttrs["provider"] == "" {
				llmAttrs["provider"] = p
			}
			if usage, ok := ae.data["usage"].(map[string]any); ok {
				if v, ok := usage["input_tokens"].(float64); ok {
					llmAttrs["input_tokens"] = fmt.Sprintf("%.0f", v)
				}
				if v, ok := usage["output_tokens"].(float64); ok {
					llmAttrs["output_tokens"] = fmt.Sprintf("%.0f", v)
				}
			}
			if dur, ok := ae.data["api_duration"].(float64); ok && dur > 0 {
				llmAttrs["latency_ms"] = fmt.Sprintf("%.0f", dur*1000)
			}
			if fr, ok := ae.data["finish_reason"].(string); ok && fr != "" {
				llmAttrs["finish_reason"] = fr
			}
			break // one post_api_request per turn is enough
		}

		status := "ok"
		if prows[i].decision == "block" {
			status = "blocked"
		}

		spans = append(spans, models.TraceSpan{
			SpanID:     llmSpanID,
			ParentID:   runID,
			Kind:       "llm",
			Name:       "LLM Turn",
			StartMs:    llmStartMs,
			DurationMs: llmEndMs - llmStartMs,
			Status:     status,
			Attributes: llmAttrs,
		})

		// ── Tool calls within this LLM window ─────────────────────────────────
		toolCount := 0
		for j := i + 1; j < len(prows); j++ {
			if prows[j].hookName == "pre_llm_call" {
				break // next LLM turn
			}
			if llmEndMs > llmStartMs && prows[j].evtMs > llmEndMs {
				break // past this turn's window
			}
			if prows[j].hookName != "pre_tool_call" || prows[j].consumed {
				continue
			}
			prows[j].consumed = true

			toolName := prows[j].toolName
			if toolName == "" {
				if tn, ok := prows[j].data["tool_name"].(string); ok {
					toolName = tn
				}
			}
			if toolName == "" {
				toolName = "Tool Call"
			}

			toolStartMs := prows[j].evtMs
			toolSpanID := fmt.Sprintf("%s-tool-%d", llmSpanID, toolCount)
			toolCount++

			var toolDurationMs int64
			toolStatus := "ok"
			if prows[j].decision == "block" {
				toolStatus = "blocked"
			}

			toolAttrs := map[string]string{"tool_name": toolName}
			if args, ok := prows[j].data["args"]; ok {
				if b, err := json.Marshal(args); err == nil {
					s := string(b)
					if len(s) > 300 {
						s = s[:300] + "…"
					}
					toolAttrs["args"] = s
				}
			}

			tcID, _ := prows[j].data["tool_call_id"].(string)
			if afterIdx, ok := postToolByID[tcID]; ok && !prows[afterIdx].consumed {
				prows[afterIdx].consumed = true
				toolDurationMs = prows[afterIdx].evtMs - toolStartMs
				if result, ok := prows[afterIdx].data["result"]; ok {
					if b, err := json.Marshal(result); err == nil {
						s := string(b)
						if len(s) > 300 {
							s = s[:300] + "…"
						}
						toolAttrs["result"] = s
					}
				}
			} else if j+1 < len(prows) {
				toolDurationMs = prows[j+1].evtMs - toolStartMs
			}
			if toolDurationMs < 0 {
				toolDurationMs = 0
			}

			spanName := toolName
			if toolStatus == "blocked" {
				spanName = toolName + " (blocked)"
			}

			spans = append(spans, models.TraceSpan{
				SpanID:     toolSpanID,
				ParentID:   llmSpanID,
				Kind:       "tool",
				Name:       spanName,
				StartMs:    toolStartMs,
				DurationMs: toolDurationMs,
				Status:     toolStatus,
				Attributes: toolAttrs,
			})
		}
	}

	// ── Remaining lifecycle events ─────────────────────────────────────────────
	lifecycleIdx := 0
	for i, pr := range prows {
		if pr.consumed {
			continue
		}
		if pr.hookName == "post_tool_call" || pr.hookName == "post_llm_call" {
			continue // orphaned post events — skip silently
		}

		var durationMs int64
		for k := i + 1; k < len(prows); k++ {
			if !prows[k].consumed {
				durationMs = prows[k].evtMs - pr.evtMs
				break
			}
		}
		if durationMs < 0 {
			durationMs = 0
		}

		spans = append(spans, models.TraceSpan{
			SpanID:     fmt.Sprintf("lifecycle-%d", lifecycleIdx),
			ParentID:   runID,
			Kind:       "lifecycle",
			Name:       hermesHookName(pr.hookName),
			StartMs:    pr.evtMs,
			DurationMs: durationMs,
			Status:     "ok",
			Attributes: map[string]string{"hook": pr.hookName},
		})
		lifecycleIdx++
	}

	sort.SliceStable(spans, func(i, j int) bool {
		return spans[i].StartMs < spans[j].StartMs
	})

	return spans
}
