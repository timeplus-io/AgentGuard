package sentry

import (
	"fmt"
	"strings"
	"time"

	"github.com/timeplus/agentguard/internal/config"
)

// SelfContext renders the "About you" block substituted for {self_context}
// in the system prompt (#229): the runtime facts Sentry needs to answer
// questions about itself — model, provider dialect, reasoning settings,
// turn budgets, deployment target, and today's date. Everything here is
// already resolved at turn time (ChatCfg / config.Config); WITHOUT this
// block the model truthfully claims it cannot know its own model.
//
// Security invariant: NEVER include cc.APIKey or cc.BaseURL — the endpoint
// and credentials must not be exposable through prompt-extraction.
// TestSelfContextNeverLeaksSecrets enforces this.
//
// Prompt-caching note (#212): this block lands inside the cached system
// prefix on the anthropic path, so it must stay stable within a day —
// static config facts only, plus a DATE-ONLY stamp (one cache
// invalidation per UTC day, never a per-turn timestamp).
func SelfContext(cc ChatCfg, cfg *config.Config, now time.Time) string {
	var b strings.Builder

	dialect := "an OpenAI-compatible Chat Completions endpoint"
	if cc.APIType == "anthropic" {
		dialect = "the Anthropic Messages API"
	}
	fmt.Fprintf(&b, "- You are running on the model %q via %s.\n", cc.Model, dialect)

	if cc.APIType == "anthropic" {
		if cc.Thinking == "adaptive" {
			b.WriteString("- Adaptive thinking is enabled; your reasoning summaries are visible to the operator.\n")
		}
		if cc.Effort != "" {
			fmt.Fprintf(&b, "- Reasoning effort is set to %q.\n", cc.Effort)
		}
	}

	fmt.Fprintf(&b, "- Per turn you may use up to %d tool iterations within a ~%d-token context budget; if you stop early, that budget is why.\n",
		cc.MaxIterations, cc.TurnTokenBudget)

	if cfg != nil && cfg.Timeplus.Host != "" {
		fmt.Fprintf(&b, "- You monitor the Timeplus Enterprise deployment at %q.\n", cfg.Timeplus.Host)
	}

	fmt.Fprintf(&b, "- Today's date (UTC): %s.\n", now.UTC().Format("2006-01-02"))
	b.WriteString("- When the operator asks about your own model or configuration, answer from this section; you have no tool that reads your own runtime config, and its credentials are deliberately not available to you.")
	return b.String()
}
