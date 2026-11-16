package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type ProviderID string

const (
	ProviderClaude   ProviderID = "claude"
	ProviderCodex    ProviderID = "codex"
	ProviderGrok     ProviderID = "grok"
	ProviderCursor   ProviderID = "cursor"
	ProviderOpenAI   ProviderID = "openai"
	ProviderOpenCode ProviderID = "opencode"
)

var Providers = []ProviderID{ProviderClaude, ProviderCodex, ProviderGrok, ProviderCursor, ProviderOpenAI, ProviderOpenCode}

func IsProviderID(v string) bool {
	switch ProviderID(v) {
	case ProviderClaude, ProviderCodex, ProviderGrok, ProviderCursor, ProviderOpenAI, ProviderOpenCode:
		return true
	default:
		return false
	}
}

func IsOAuthProvider(v string) bool {
	switch ProviderID(v) {
	case ProviderClaude, ProviderCodex, ProviderGrok, ProviderCursor:
		return true
	default:
		return false
	}
}

type InboundFormat string

const (
	InboundOpenAIChat      InboundFormat = "openai.chat"
	InboundOpenAIResponses InboundFormat = "openai.responses"
	InboundClaudeMessages  InboundFormat = "claude.messages"
)

type ExecutorFormat string

const (
	FormatClaude ExecutorFormat = "claude"
	FormatCodex  ExecutorFormat = "codex"
	FormatGrok   ExecutorFormat = "grok"
	FormatCursor ExecutorFormat = "cursor"
	FormatOpenAI ExecutorFormat = "openai"
)

type LocalKey struct {
	ID        string
	Name      string
	Prefix    string
	Providers []ProviderID
	Models    []string
	DailyCap  *int64
	Revoked   bool
	CreatedAt int64
}

type CredentialTokens struct {
	AccessToken  string
	RefreshToken string
	AccountID    string
	Email        string
	Extra        map[string]any
}

type Credential struct {
	ID        string
	Provider  ProviderID
	Label     string
	Tokens    CredentialTokens
	ExpiresAt *int64
	CreatedAt int64
	UpdatedAt int64
}

type UsageEvent struct {
	TS               int64
	KeyID            string
	CredentialID     string
	Provider         ProviderID
	Model            string
	Inbound          InboundFormat
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	LatencyMs        int64
	Status           string
	USD              float64
	ErrorStatus      int    // for failures: the HTTP status the client got, 0 when a stream broke off
	ErrorMessage     string // for failures: what the client was told
}

type ModelPrice struct {
	Provider   string
	Model      string
	Input      *float64
	Output     *float64
	CacheRead  *float64
	CacheWrite *float64
	Tiers      []ModelCostTier // ascending by InputTokensAbove
}

// ModelCostTier holds the rates that replace a model's base rates once a
// request's total input exceeds InputTokensAbove tokens. A nil rate keeps the
// base rate.
type ModelCostTier struct {
	InputTokensAbove int      `json:"input_tokens_above"`
	Input            *float64 `json:"input,omitempty"`
	Output           *float64 `json:"output,omitempty"`
	CacheRead        *float64 `json:"cache_read,omitempty"`
	CacheWrite       *float64 `json:"cache_write,omitempty"`
}

// UnmarshalJSON reads both our own form and models.dev's, which nests the
// threshold as "tier":{"type":"context","size":N}. Tiers of any other type
// get a zero threshold, which ModelCostJSON drops. The size may be a float or
// a numeric string; anything else is an error, and ModelCostJSON skips the tier.
func (t *ModelCostTier) UnmarshalJSON(b []byte) error {
	type plain ModelCostTier // no methods, so no recursion
	var raw struct {
		plain
		Tier *struct {
			Type string          `json:"type"`
			Size json.RawMessage `json:"size"`
		} `json:"tier"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*t = ModelCostTier(raw.plain)
	if raw.Tier != nil {
		t.InputTokensAbove = 0
		if raw.Tier.Type == "context" {
			size, err := tierSize(raw.Tier.Size)
			if err != nil {
				return err
			}
			t.InputTokensAbove = size
		}
	}
	return nil
}

// tierSize reads a token threshold written as a number or a numeric string.
func tierSize(raw json.RawMessage) (int, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, fmt.Errorf("tier size %q is not a number", x)
		}
		f = n
	default:
		return 0, fmt.Errorf("tier size %s is not a number", raw)
	}
	if math.IsNaN(f) || f <= 0 || f > math.MaxInt32 {
		return 0, nil
	}
	return int(f), nil
}

type ModelCostJSON struct {
	Input      *float64        `json:"input,omitempty"`
	Output     *float64        `json:"output,omitempty"`
	CacheRead  *float64        `json:"cache_read,omitempty"`
	CacheWrite *float64        `json:"cache_write,omitempty"`
	Tiers      []ModelCostTier `json:"tiers,omitempty"`
}

// UnmarshalJSON keeps only context tiers with a threshold, smallest first.
// Tiers are optional extras on a price, so a tier that doesn't parse, or a
// "tiers" value that isn't a list, is skipped rather than failing the price.
func (c *ModelCostJSON) UnmarshalJSON(b []byte) error {
	type plain ModelCostJSON
	var raw struct {
		plain
		Tiers json.RawMessage `json:"tiers"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	p := raw.plain
	var entries []json.RawMessage
	_ = json.Unmarshal(raw.Tiers, &entries) // not a list: no tiers
	var kept []ModelCostTier
	for _, e := range entries {
		var t ModelCostTier
		if json.Unmarshal(e, &t) != nil {
			continue
		}
		if t.InputTokensAbove > 0 {
			kept = append(kept, t)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].InputTokensAbove < kept[j].InputTokensAbove })
	p.Tiers = kept
	if len(p.Tiers) == 0 {
		p.Tiers = nil
	}
	*c = ModelCostJSON(p)
	return nil
}

type ModelInfo struct {
	ID              string         `json:"id"`
	Object          string         `json:"object"`
	OwnedBy         string         `json:"owned_by"`
	Name            string         `json:"name,omitempty"`
	DisplayName     string         `json:"display_name,omitempty"`
	ContextLength   int            `json:"context_length,omitempty"`
	MaxOutputTokens int            `json:"max_output_tokens,omitempty"`
	Input           []string       `json:"input,omitempty"`
	InputModalities []string       `json:"input_modalities,omitempty"`
	Reasoning       bool           `json:"reasoning,omitempty"`
	Efforts         []string       `json:"efforts,omitempty"`
	Cost            *ModelCostJSON `json:"cost,omitempty"`
}
