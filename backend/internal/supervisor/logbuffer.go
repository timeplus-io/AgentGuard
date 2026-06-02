package supervisor

import (
	"bytes"
	"regexp"
	"sync"
)

// secretPatterns mask credential/license-looking substrings before any
// captured child output leaves the process (spec §5.6).
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|license[_-]?key|api[_-]?key)([=:]\s*|\s+)\S+`),
	regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}\b`),
}

func redact(s string) string {
	out := secretPatterns[0].ReplaceAllString(s, "$1$2***")
	for _, re := range secretPatterns[1:] {
		out = re.ReplaceAllString(out, "***")
	}
	return out
}

// LogBuffer is a concurrency-safe ring buffer of the last N redacted lines.
// It implements io.Writer so it can be wired into a child's stdout/stderr.
type LogBuffer struct {
	mu       sync.Mutex
	maxLines int
	lines    []string
	partial  []byte
}

func NewLogBuffer(maxLines int) *LogBuffer {
	if maxLines < 1 {
		maxLines = 1
	}
	return &LogBuffer{maxLines: maxLines}
}

func (lb *LogBuffer) Write(p []byte) (int, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.partial = append(lb.partial, p...)
	for {
		i := bytes.IndexByte(lb.partial, '\n')
		if i < 0 {
			break
		}
		line := string(lb.partial[:i])
		lb.partial = lb.partial[i+1:]
		lb.append(redact(line))
	}
	return len(p), nil
}

func (lb *LogBuffer) append(line string) {
	lb.lines = append(lb.lines, line)
	if len(lb.lines) > lb.maxLines {
		lb.lines = lb.lines[len(lb.lines)-lb.maxLines:]
	}
}

func (lb *LogBuffer) Lines() []string {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	out := make([]string, len(lb.lines))
	copy(out, lb.lines)
	return out
}

// Tail returns the most recent lines joined by "\n", capped to maxBytes
// (keeping the newest content).
func (lb *LogBuffer) Tail(maxBytes int) string {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	joined := ""
	for i := len(lb.lines) - 1; i >= 0; i-- {
		next := lb.lines[i]
		if joined != "" {
			next = next + "\n"
		}
		if len(next)+len(joined) > maxBytes {
			break
		}
		joined = next + joined
	}
	return joined
}
