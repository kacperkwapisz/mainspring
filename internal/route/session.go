package route

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/kacperkwapisz/mainspring/internal/live"
	"regexp"
	"sort"
	"strings"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

var legacyClaudeSession = regexp.MustCompile(`_session_([0-9a-fA-F-]{36})`)

// SessionKey finds the conversation a request belongs to, so every request of
// one coding session lands on the same account and its prompt cache stays warm.
// It looks at what Claude Code, Codex and OpenAI-style clients send, and falls
// back to the LocalKey so a tool with no session id still sticks to one account.
func SessionKey(headers map[string]string, body map[string]any, localKeyID string) string {
	h := func(name string) string {
		for k, v := range headers {
			if strings.EqualFold(k, name) {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	if v := h("X-Claude-Code-Session-Id"); v != "" {
		return "claude:" + v
	}
	if md, ok := body["metadata"].(map[string]any); ok {
		if uid, _ := md["user_id"].(string); uid != "" {
			var parsed map[string]any
			if strings.HasPrefix(strings.TrimSpace(uid), "{") && json.Unmarshal([]byte(uid), &parsed) == nil {
				if sid, _ := parsed["session_id"].(string); sid != "" {
					return "claude:" + sid
				}
			}
			if m := legacyClaudeSession.FindStringSubmatch(uid); m != nil {
				return "claude:" + m[1]
			}
		}
	}
	// Codex subagents stay on their parent's account.
	if v := h("x-codex-parent-thread-id"); v != "" {
		return "codex:" + v
	}
	for _, name := range []string{"session-id", "session_id", "thread-id"} {
		if v := h(name); v != "" {
			return "codex:" + v
		}
	}
	if v := h("x-codex-turn-metadata"); v != "" {
		var md map[string]any
		if json.Unmarshal([]byte(v), &md) == nil {
			for _, k := range []string{"session_id", "thread_id"} {
				if s, _ := md[k].(string); s != "" {
					return "codex:" + s
				}
			}
		}
	}
	for _, name := range []string{"X-Session-ID", "X-Session-Affinity"} {
		if v := h(name); v != "" {
			return "sid:" + v
		}
	}
	if s, _ := body["session_id"].(string); s != "" {
		return "sid:" + s
	}
	if s, _ := body["prompt_cache_key"].(string); s != "" {
		return "pck:" + s
	}
	if s, _ := body["conversation_id"].(string); s != "" {
		return "conv:" + s
	}
	if conv, ok := body["conversation"].(map[string]any); ok {
		if s, _ := conv["id"].(string); s != "" {
			return "conv:" + s
		}
	}
	if localKeyID != "" {
		return "key:" + localKeyID
	}
	return ""
}

// ClientName guesses the tool from the request headers.
func ClientName(headers map[string]string) string {
	get := func(name string) string {
		for k, v := range headers {
			if strings.EqualFold(k, name) {
				return v
			}
		}
		return ""
	}
	ua := strings.ToLower(get("User-Agent"))
	orig := strings.ToLower(get("originator"))
	switch {
	case strings.Contains(ua, "claude-cli") || strings.Contains(ua, "claude-code") || get("X-Claude-Code-Session-Id") != "":
		return "Claude Code"
	case strings.Contains(orig, "codex") || strings.Contains(ua, "codex"):
		return "Codex"
	case strings.Contains(ua, "opencode"):
		return "OpenCode"
	case strings.Contains(ua, "cursor"):
		return "Cursor"
	case strings.Contains(ua, "t3"):
		return "T3 Code"
	case ua == "":
		return "Unknown"
	}
	name := get("User-Agent")
	if i := strings.IndexAny(name, "/ ("); i > 0 {
		name = name[:i]
	}
	if len(name) > 32 {
		name = name[:32]
	}
	return name
}

// Session is one live coding session as the Sessions screen shows it.
type Session struct {
	Key         string            `json:"key"`
	ShortID     string            `json:"id"`
	Provider    domain.ProviderID `json:"provider"`
	CredID      string            `json:"credential_id"`
	LocalKeyID  string            `json:"key_id"`
	Client      string            `json:"client"`
	Model       string            `json:"model"`
	Transport   string            `json:"transport"`
	StartedAt   int64             `json:"started_at"`
	LastAt      int64             `json:"last_at"`
	Requests    int64             `json:"requests"`
	InputTokens int64             `json:"input_tokens"`
	CacheRead   int64             `json:"cache_read_tokens"`
	MovedFrom   string            `json:"moved_from,omitempty"`
	MovedAt     int64             `json:"moved_at,omitempty"`
	MovedReason string            `json:"moved_reason,omitempty"`
}

// Touch records a request on a session after it was served.
type Touch struct {
	Provider    domain.ProviderID
	SessionKey  string
	CredID      string
	LocalKeyID  string
	Client      string
	Model       string
	Transport   string
	InputTokens int64
	CacheRead   int64
	MovedFrom   string
	MovedReason string
}

// Touch updates (or starts) the live session for t.
func (e *Engine) Touch(t Touch) {
	if t.SessionKey == "" {
		return
	}
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	k := bindKey(t.Provider, t.SessionKey)
	s := e.sessions[k]
	if s == nil || now-s.LastAt > e.ttl.Milliseconds() {
		s = &Session{Key: k, ShortID: shortID(t.SessionKey), Provider: t.Provider, StartedAt: now}
		e.sessions[k] = s
	}
	if t.MovedFrom != "" && t.MovedFrom != t.CredID {
		s.MovedFrom, s.MovedAt, s.MovedReason = t.MovedFrom, now, t.MovedReason
	}
	s.CredID, s.LocalKeyID, s.Client, s.Model, s.LastAt = t.CredID, t.LocalKeyID, t.Client, t.Model, now
	if t.Transport != "" {
		s.Transport = t.Transport
	}
	s.Requests++
	e.Live.Publish(live.Sessions, live.Pool)
	s.InputTokens += t.InputTokens
	s.CacheRead += t.CacheRead
	if len(e.sessions) > 4096 {
		for key, old := range e.sessions {
			if now-old.LastAt > e.ttl.Milliseconds() {
				delete(e.sessions, key)
			}
		}
	}
}

// LiveSessions returns sessions with a request inside the affinity window, newest first.
func (e *Engine) LiveSessions() []Session {
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []Session{}
	for _, s := range e.sessions {
		if now-s.LastAt <= e.ttl.Milliseconds() {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out
}

// SessionsOn counts the live sessions currently on credID.
func (e *Engine) SessionsOn(credID string) int {
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, s := range e.sessions {
		if s.CredID == credID && now-s.LastAt <= e.ttl.Milliseconds() {
			n++
		}
	}
	return n
}

// shortID is a short, stable label for a session. It comes from a hash, not
// a prefix: time-based ids (pi's, UUIDv7) share their first characters.
func shortID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:6]
}

// SeedSession adds a live session as-is (demo data).
func (e *Engine) SeedSession(s Session) {
	e.mu.Lock()
	e.sessions[s.Key] = &s
	e.mu.Unlock()
}
