package handlers

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/timeplus/agentguard/internal/models"
)

// spanNameMap translates hook_name values to human-readable span names.
var spanNameMap = map[string]string{
	"before_agent_start":  "Agent Start",
	"agent_end":           "Agent End",
	"llm_input":           "LLM Call",
	"llm_output":          "LLM Response",
	"before_tool_call":    "Tool Call",
	"after_tool_call":     "Tool Call",
	"tool_result_persist": "Persist Tool Result",
	"subagent_spawning":   "Subagent Start",
	"subagent_spawned":    "Subagent Start",
	"subagent_ended":      "Subagent End",
	"session_start":       "Session Start",
	"session_end":         "Session End",
}

func humanName(hookName string) string {
	if n, ok := spanNameMap[hookName]; ok {
		return n
	}
	return hookName
}

// analyzeOpenClaw builds a hierarchical trace from OpenClaw hook event rows.
// It produces: root run span → LLM call spans → tool call spans (children of LLM).
// Unpaired events are gap-filled and attached to the nearest parent.
func analyzeOpenClaw(runID string, rows []map[string]any) []models.TraceSpan {
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

	// Parse event_data JSON for every row once — reused across multiple steps.
	type parsedRow struct {
		raw      map[string]any
		evtMs    int64
		hookName string
		toolName string
		data     map[string]any // parsed event_data; nil if unparseable
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

	// ── Step 1: Run root span ────────────────────────────────────────────────
	runDurationMs := lastMs - firstMs
	for i := range prows {
		if prows[i].hookName == "agent_end" {
			if dur, ok := prows[i].data["durationMs"].(float64); ok && dur > 0 {
				runDurationMs = int64(dur)
			}
			// Mark agent_end consumed so it doesn't appear as a lifecycle span;
			// its only purpose is to carry the authoritative run duration.
			prows[i].consumed = true
			break
		}
	}
	if runDurationMs <= 0 {
		runDurationMs = 1
	}

	spans := []models.TraceSpan{{
		SpanID:     runID,
		ParentID:   "",
		Kind:       "run",
		Name:       "Agent Run",
		StartMs:    firstMs,
		DurationMs: runDurationMs,
		Status:     "ok",
		Attributes: map[string]string{},
	}}

	// ── Step 2 + 3 + 4: LLM spans and tool call spans ───────────────────────
	// Build a lookup of after_tool_call rows by toolCallId for O(1) pairing.
	afterToolByID := map[string]int{} // toolCallId → index in prows
	for i, pr := range prows {
		if pr.hookName == "after_tool_call" {
			if tcID, ok := pr.data["toolCallId"].(string); ok && tcID != "" {
				afterToolByID[tcID] = i
			}
		}
	}

	llmSpanCount := 0
	for i := range prows {
		if prows[i].hookName != "llm_input" {
			continue
		}
		prows[i].consumed = true
		llmStartMs := prows[i].evtMs
		llmSpanID := fmt.Sprintf("llm-%d", llmSpanCount)
		llmSpanCount++

		// Find matching llm_output (same runId, after this llm_input in time).
		llmEndMs := llmStartMs // zero-duration default
		llmRunID, _ := prows[i].data["runId"].(string)
		for j := i + 1; j < len(prows); j++ {
			if prows[j].hookName == "llm_output" {
				outRunID, _ := prows[j].data["runId"].(string)
				if llmRunID == "" || outRunID == "" || llmRunID == outRunID {
					llmEndMs = prows[j].evtMs
					prows[j].consumed = true
					break
				}
			}
		}

		model := parseStr(prows[i].raw["model"])
		provider := parseStr(prows[i].raw["provider"])
		llmAttrs := map[string]string{}
		if model != "" {
			llmAttrs["model"] = model
		}
		if provider != "" {
			llmAttrs["provider"] = provider
		}

		spans = append(spans, models.TraceSpan{
			SpanID:     llmSpanID,
			ParentID:   runID,
			Kind:       "llm",
			Name:       "LLM Call",
			StartMs:    llmStartMs,
			DurationMs: llmEndMs - llmStartMs,
			Status:     "ok",
			Attributes: llmAttrs,
		})

		// ── Step 3 + 4: Tool calls that occurred during this LLM window ─────
		toolCount := 0
		for j := i + 1; j < len(prows); j++ {
			// Stop at the next LLM turn regardless of whether llm_output was found.
			if prows[j].hookName == "llm_input" {
				break
			}
			if prows[j].evtMs >= llmEndMs && llmEndMs > llmStartMs {
				break // past the LLM window
			}
			if prows[j].hookName != "before_tool_call" || prows[j].consumed {
				continue
			}
			prows[j].consumed = true

			toolName := prows[j].toolName
			if toolName == "" {
				toolName = "Tool Call"
			}
			toolStartMs := prows[j].evtMs
			toolSpanID := fmt.Sprintf("%s-tool-%d", llmSpanID, toolCount)
			toolCount++

			var toolDurationMs int64
			status := "ok"
			if prows[j].decision == "block" {
				status = "blocked"
			}

			tcID, _ := prows[j].data["toolCallId"].(string)
			if afterIdx, ok := afterToolByID[tcID]; ok && !prows[afterIdx].consumed {
				prows[afterIdx].consumed = true
				afterData := prows[afterIdx].data
				if dur, ok := afterData["durationMs"].(float64); ok && dur > 0 {
					toolDurationMs = int64(dur)
				} else {
					toolDurationMs = prows[afterIdx].evtMs - toolStartMs
				}
			} else {
				// Unmatched (no toolCallId or no after_tool_call found): gap-fill duration.
				// Only mark blocked if hook_decision was already block; don't upgrade to blocked
				// just because event_data was missing toolCallId.
				if prows[j].decision == "block" {
					status = "blocked"
				}
				if j+1 < len(prows) {
					toolDurationMs = prows[j+1].evtMs - toolStartMs
				}
			}
			if toolDurationMs < 0 {
				toolDurationMs = 0
			}

			spanName := toolName
			if status == "blocked" {
				spanName = toolName + " (blocked)"
			}

			spans = append(spans, models.TraceSpan{
				SpanID:     toolSpanID,
				ParentID:   llmSpanID,
				Kind:       "tool",
				Name:       spanName,
				StartMs:    toolStartMs,
				DurationMs: toolDurationMs,
				Status:     status,
				Attributes: map[string]string{"tool_name": toolName},
			})

			// ── Step 5: tool_result_persist for this tool call ───────────
			for k := j + 1; k < len(prows); k++ {
				if prows[k].consumed {
					continue
				}
				if prows[k].hookName != "tool_result_persist" {
					continue
				}
				persistStartMs := prows[k].evtMs
				if persistStartMs < toolStartMs+toolDurationMs || toolDurationMs == 0 {
					prows[k].consumed = true
					var persistDuration int64
					if k+1 < len(prows) {
						persistDuration = prows[k+1].evtMs - persistStartMs
					}
					if persistDuration < 0 {
						persistDuration = 0
					}
					spans = append(spans, models.TraceSpan{
						SpanID:     fmt.Sprintf("%s-persist", toolSpanID),
						ParentID:   toolSpanID,
						Kind:       "tool",
						Name:       "Persist Tool Result",
						StartMs:    persistStartMs,
						DurationMs: persistDuration,
						Status:     "ok",
						Attributes: map[string]string{},
					})
					break
				}
			}
		}
	}

	// ── Step 6: Remaining lifecycle events ───────────────────────────────────
	for i, pr := range prows {
		if pr.consumed {
			continue
		}
		if pr.hookName == "after_tool_call" {
			continue
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

		kind := "lifecycle"
		if pr.hookName == "subagent_spawning" || pr.hookName == "subagent_spawned" || pr.hookName == "subagent_ended" {
			kind = "subagent"
		}

		spans = append(spans, models.TraceSpan{
			SpanID:     fmt.Sprintf("lifecycle-%d", i),
			ParentID:   runID,
			Kind:       kind,
			Name:       humanName(pr.hookName),
			StartMs:    pr.evtMs,
			DurationMs: durationMs,
			Status:     "ok",
			Attributes: map[string]string{},
		})
	}

	// Sort spans chronologically by start_ms so the waterfall renders top-to-bottom
	// in time order. StableSlice preserves the original insertion order for equal
	// start_ms values, which keeps the root span (appended first at firstMs) before
	// any lifecycle events that share the same timestamp.
	sort.SliceStable(spans, func(i, j int) bool {
		return spans[i].StartMs < spans[j].StartMs
	})

	return spans
}
