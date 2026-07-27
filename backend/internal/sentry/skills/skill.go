package skills

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/timeplus/agentguard/internal/config"
)

type AccessClass int

const (
	ReadOnly    AccessClass = iota
	MemoryWrite             // chat-only, no approval (Phase 2)
	GatedWrite              // chat-only, approval required (Phase 4)
)

type RunContext struct {
	Cfg            *config.Config // per-request creds already injected
	ConversationID string
	User           string // conversation owner — attribution for gated writes (decided_by)
	SQLTimeoutMs   int
	SQLMaxRows     int
	ResultMaxBytes int
	Loaded         map[string]bool // per-conversation knowledge-load cache
}

type Skill interface {
	Name() string
	Description() string
	Params() map[string]any
	Access() AccessClass
	Execute(ctx context.Context, rc *RunContext, args json.RawMessage) (string, error)
}

type Def struct {
	Name        string
	Description string
	Params      map[string]any
}

type Registry struct {
	order  []string
	byName map[string]Skill
}

func NewRegistry(sk ...Skill) *Registry {
	r := &Registry{byName: map[string]Skill{}}
	for _, s := range sk {
		r.order = append(r.order, s.Name())
		r.byName[s.Name()] = s
	}
	return r
}

func (r *Registry) Get(name string) (Skill, bool) { s, ok := r.byName[name]; return s, ok }

// Defs returns tool definitions whose access class is <= maxAccess, in
// registration order (stable prompts help caching and tests).
func (r *Registry) Defs(maxAccess AccessClass) []Def {
	var out []Def
	for _, n := range r.order {
		s := r.byName[n]
		if s.Access() <= maxAccess {
			out = append(out, Def{Name: s.Name(), Description: s.Description(), Params: s.Params()})
		}
	}
	return out
}

const truncMarker = "... [truncated]"

// Truncate caps s at max bytes on a rune boundary, marking the cut.
func Truncate(s string, max int) (string, bool) {
	if max < 0 {
		max = 0
	}
	if len(s) <= max {
		return s, false
	}
	i := max
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i] + truncMarker, true
}
