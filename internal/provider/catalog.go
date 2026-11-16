package provider

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/prices"
)

var providerCatalog = map[domain.ProviderID]string{
	domain.ProviderClaude: "anthropic",
	domain.ProviderCodex:  "openai",
	domain.ProviderGrok:   "xai",
	domain.ProviderCursor: "cursor",
}

type catalogLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type catalogModel struct {
	ID    string                `json:"id"`
	Name  string                `json:"name"`
	Limit *catalogLimit         `json:"limit"`
	Cost  *domain.ModelCostJSON `json:"cost"`
}

var (
	codexEfforts = []string{"none", "low", "medium", "high", "xhigh"}
	grokEfforts  = []string{"low", "medium", "high", "xhigh"}
)

func CatalogModels(provider domain.ProviderID) []domain.ModelInfo {
	rows, ok := catalogPack(providerCatalog[provider])
	if !ok {
		return nil
	}
	out := []domain.ModelInfo{}
	for _, m := range rows {
		if !keep(provider, m.ID) {
			continue
		}
		out = append(out, modelInfo(provider, m))
	}
	return out
}

func catalogPack(packID string) ([]catalogModel, bool) {
	raw, ok := packJSON(packID)
	if !ok {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, false
	}
	var out []catalogModel
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			break
		}
		// Read the raw value first so one model that doesn't fit our types is
		// skipped instead of ending the list.
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		var m catalogModel
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if m.ID == "" {
			m.ID, _ = key.(string)
		}
		out = append(out, m)
	}
	return out, true
}

func packJSON(packID string) (json.RawMessage, bool) {
	src := prices.API()
	switch packID {
	case "cursor":
		src = prices.CursorJSON
	case "opencode":
		src = prices.OpenCodeJSON
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(src, &top) != nil {
		return nil, false
	}
	packRaw, ok := top[packID]
	if !ok {
		return nil, false
	}
	var pack struct {
		Models json.RawMessage `json:"models"`
	}
	if json.Unmarshal(packRaw, &pack) != nil || len(pack.Models) == 0 {
		return nil, false
	}
	return pack.Models, true
}

func modelInfo(provider domain.ProviderID, m catalogModel) domain.ModelInfo {
	input := []string{"text", "image"}
	info := domain.ModelInfo{
		ID:              m.ID,
		Object:          "model",
		OwnedBy:         string(provider),
		Name:            m.Name,
		DisplayName:     m.Name,
		Input:           input,
		InputModalities: input,
		Efforts:         modelEfforts(provider, m.ID),
	}
	if m.Limit != nil {
		info.ContextLength = m.Limit.Context
		info.MaxOutputTokens = m.Limit.Output
	}
	if cost := nonemptyCost(m.Cost); cost != nil {
		info.Cost = cost
	}
	info.Reasoning = modelReasoning(provider, m.ID, info.Efforts)
	return info
}

func nonemptyCost(cost *domain.ModelCostJSON) *domain.ModelCostJSON {
	if cost == nil || (cost.Input == nil && cost.Output == nil && cost.CacheRead == nil && cost.CacheWrite == nil && len(cost.Tiers) == 0) {
		return nil
	}
	return cost
}

func modelEfforts(provider domain.ProviderID, id string) []string {
	switch provider {
	case domain.ProviderClaude:
		return claudeModelEfforts(id)
	case domain.ProviderCodex:
		return append([]string{}, codexEfforts...)
	case domain.ProviderGrok:
		return grokModelEfforts(id)
	default:
		return nil
	}
}

var (
	claudeFamilyFirstRe  = regexp.MustCompile(`^claude-(opus|sonnet|haiku|fable)-(\d+)(?:-(\d{1,2}))?(?:-|$)`)
	claudeVersionFirstRe = regexp.MustCompile(`^claude-(\d+)(?:-(\d{1,2}))?-(opus|sonnet|haiku|fable)(?:-|$)`)
)

// claudeModelEfforts guesses a Claude model's effort levels from its id, for
// when the account's own model list (which says exactly) can't be read. Effort
// came with Claude 4.6; xhigh with Opus 4.7, and Fable has it too. Models
// before 4.6 get none, so clients don't ask them for adaptive thinking.
func claudeModelEfforts(id string) []string {
	family, major, minor, ok := claudeVersion(id)
	if !ok || major < 4 || (major == 4 && minor < 6) {
		return nil
	}
	out := []string{"low", "medium", "high"}
	if (family == "opus" || family == "fable") && (major > 4 || minor >= 7) {
		out = append(out, "xhigh")
	}
	return append(out, "max")
}

// claudeVersion reads claude-<family>-<major>[-<minor>] and the older
// claude-<major>[-<minor>]-<family>. A dated suffix (-20251101) isn't a minor.
func claudeVersion(id string) (family string, major, minor int, ok bool) {
	low := strings.ToLower(id)
	if i := strings.LastIndex(low, "/"); i >= 0 {
		low = low[i+1:]
	}
	var majorS, minorS string
	if m := claudeFamilyFirstRe.FindStringSubmatch(low); m != nil {
		family, majorS, minorS = m[1], m[2], m[3]
	} else if m := claudeVersionFirstRe.FindStringSubmatch(low); m != nil {
		majorS, minorS, family = m[1], m[2], m[3]
	} else {
		return "", 0, 0, false
	}
	major, _ = strconv.Atoi(majorS)
	if minorS != "" {
		minor, _ = strconv.Atoi(minorS)
	}
	return family, major, minor, true
}

func grokModelEfforts(id string) []string {
	low := strings.ToLower(id)
	if i := strings.LastIndex(low, "/"); i >= 0 {
		low = low[i+1:]
	}
	if strings.Contains(low, "non-reasoning") {
		return nil
	}
	if strings.HasSuffix(low, "-reasoning") && !strings.Contains(low, "multi-agent") {
		return nil
	}
	if strings.HasPrefix(low, "grok-3-mini") || strings.HasPrefix(low, "grok-4.20-multi-agent") || strings.HasPrefix(low, "grok-4.3") || strings.HasPrefix(low, "grok-4.5") || strings.HasPrefix(low, "grok-4.6") {
		return append([]string{}, grokEfforts...)
	}
	return nil
}

func modelReasoning(provider domain.ProviderID, id string, efforts []string) bool {
	low := strings.ToLower(id)
	if strings.Contains(low, "non-reasoning") {
		return false
	}
	if len(efforts) > 0 {
		return true
	}
	if strings.Contains(low, "thinking") || strings.Contains(low, "reasoning") {
		return true
	}
	return provider == domain.ProviderClaude || provider == domain.ProviderCodex
}

// Limits returns snapshot context, output, cost, and modalities for an id.
// Native catalogs win; Cursor overlay fills cost for Cursor-only ids.
func Limits(id string) (contextLength, maxOutput int, cost *domain.ModelCostJSON, input []string, ok bool) {
	id = strings.TrimPrefix(id, "cursor/")
	if strings.HasPrefix(id, "cursor-") {
		if contextLength, maxOutput, cost, input, ok = limitsIn(domain.ProviderGrok, strings.TrimPrefix(id, "cursor-")); ok {
			return contextLength, maxOutput, cost, input, true
		}
	}
	for _, providerID := range []domain.ProviderID{domain.ProviderClaude, domain.ProviderCodex, domain.ProviderGrok} {
		if contextLength, maxOutput, cost, input, ok = limitsIn(providerID, id); ok {
			return contextLength, maxOutput, cost, input, true
		}
	}
	if contextLength, maxOutput, cost, input, ok = limitsIn(domain.ProviderCursor, id); ok {
		return contextLength, maxOutput, cost, input, true
	}
	return 0, 0, nil, nil, false
}

// CursorLimits is Limits for a Cursor id, looked up the way the meter prices
// it (store.PriceStore.Get): the exact id, then the id without -fast, then the
// vendor catalogues in domain.CursorPriceLookups order. Whatever an earlier
// match leaves out comes from the first later one.
func CursorLimits(id string) (ctx, maxOut int, cost *domain.ModelCostJSON, input []string, ok bool) {
	id = strings.TrimPrefix(id, "cursor/")
	ctx, maxOut, cost, input, ok = Limits(id)
	if base := strings.TrimSuffix(id, "-fast"); base != id {
		if bctx, bmax, bcost, binput, bok := Limits(base); bok {
			if !ok {
				input = binput
			}
			ctx, maxOut, cost = fillLimits(ctx, maxOut, cost, bctx, bmax, bcost)
			ok = true
		}
	}
	if ctx > 0 && maxOut > 0 && cost != nil {
		return
	}
	packs := map[string][]catalogModel{}
	for _, l := range domain.CursorPriceLookups(id) {
		rows, seen := packs[l.Catalog]
		if !seen {
			rows, _ = catalogPack(l.Catalog)
			packs[l.Catalog] = rows
		}
		m, found := findPrefix(rows, l.ID)
		if !found {
			continue
		}
		var mctx, mmax int
		if m.Limit != nil {
			mctx, mmax = m.Limit.Context, m.Limit.Output
		}
		ctx, maxOut, cost = fillLimits(ctx, maxOut, cost, mctx, mmax, nonemptyCost(m.Cost))
		if !ok {
			input = []string{"text", "image"}
		}
		return ctx, maxOut, cost, input, true
	}
	return
}

func fillLimits(ctx, maxOut int, cost *domain.ModelCostJSON, bctx, bmax int, bcost *domain.ModelCostJSON) (int, int, *domain.ModelCostJSON) {
	if ctx == 0 {
		ctx = bctx
	}
	if maxOut == 0 {
		maxOut = bmax
	}
	if cost == nil {
		cost = bcost
	}
	return ctx, maxOut, cost
}

// findPrefix matches an id exactly, else the longest catalogue id it starts
// with, as the meter's price lookup does (glm-5.2-high is glm-5.2).
func findPrefix(rows []catalogModel, id string) (catalogModel, bool) {
	best := -1
	for i, m := range rows {
		if m.ID == id {
			return m, true
		}
		if m.ID != "" && strings.HasPrefix(id, m.ID) && (best < 0 || len(m.ID) > len(rows[best].ID)) {
			best = i
		}
	}
	if best < 0 {
		return catalogModel{}, false
	}
	return rows[best], true
}

func limitsIn(provider domain.ProviderID, id string) (int, int, *domain.ModelCostJSON, []string, bool) {
	rows, ok := catalogPack(providerCatalog[provider])
	if !ok {
		return 0, 0, nil, nil, false
	}
	for _, m := range rows {
		if m.ID != id {
			continue
		}
		var ctx, maxOut int
		if m.Limit != nil {
			ctx, maxOut = m.Limit.Context, m.Limit.Output
		}
		return ctx, maxOut, nonemptyCost(m.Cost), []string{"text", "image"}, true
	}
	return 0, 0, nil, nil, false
}

// CatalogEntry is what the models.dev catalogue knows about one model.
type CatalogEntry struct {
	Context int
	Output  int
	Cost    *domain.ModelCostJSON
}

// CatalogIndex finds models.dev entries by id across every catalogue pack, for
// sources that list models without prices or limits. Build one per listing:
// reading a pack decodes it.
type CatalogIndex struct {
	byPack map[string]map[string]CatalogEntry
}

func NewCatalogIndex() *CatalogIndex {
	idx := &CatalogIndex{byPack: map[string]map[string]CatalogEntry{}}
	for _, pack := range domain.SourcePriceCatalogs {
		rows, ok := catalogPack(pack)
		if !ok {
			continue
		}
		byID := map[string]CatalogEntry{}
		for _, m := range rows {
			e := CatalogEntry{Cost: nonemptyCost(m.Cost)}
			if m.Limit != nil {
				e.Context, e.Output = m.Limit.Context, m.Limit.Output
			}
			if _, dup := byID[m.ID]; !dup {
				byID[m.ID] = e
			}
		}
		idx.byPack[pack] = byID
	}
	return idx
}

// Find looks the id up in domain.SourcePriceLookups order, the order the
// meter prices a source model in (store.PriceStore.EstimateSource).
func (idx *CatalogIndex) Find(id string) (CatalogEntry, bool) {
	for _, l := range domain.SourcePriceLookups(id) {
		if e, ok := idx.byPack[l.Catalog][l.ID]; ok {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

func PriceProvider(provider domain.ProviderID) string { return providerCatalog[provider] }

// ChatGPT-account Codex catalog. Matches pi-ai openai-codex.json.
var chatgptCodexModels = map[string]bool{
	"gpt-5.6-luna":        true,
	"gpt-5.6-terra":       true,
	"gpt-5.6-sol":         true,
	"gpt-5.5":             true,
	"gpt-5.4":             true,
	"gpt-5.4-mini":        true,
	"gpt-5.3-codex-spark": true,
}

func keepCodex(id string) bool {
	return chatgptCodexModels[id]
}

func keep(provider domain.ProviderID, id string) bool {
	switch provider {
	case domain.ProviderClaude:
		return strings.HasPrefix(id, "claude-")
	case domain.ProviderCodex:
		return keepCodex(id)
	case domain.ProviderGrok:
		return strings.HasPrefix(id, "grok-") && !strings.Contains(id, "imagine")
	case domain.ProviderCursor:
		return true
	default:
		return false
	}
}
