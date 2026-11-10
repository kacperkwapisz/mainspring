package proxy

import (
	"github.com/kacperkwapisz/mainspring/internal/domain"
	"regexp"
	"sort"
	"strings"
)

// SettingDisabledModels holds the model IDs turned off in the panel, as a JSON
// array of IDs exactly as /v1/models lists them. A turned-off model is left out
// of /v1/models and refused, including its Cursor fallback.
const SettingDisabledModels = "models.disabled"

// DisabledModels returns the turned-off model IDs.
func DisabledModels(hub *Hub) map[string]bool {
	out := map[string]bool{}
	if hub == nil || hub.Settings == nil {
		return out
	}
	var ids []string
	_ = hub.Settings.GetJSON(SettingDisabledModels, &ids)
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// ServedModels is ListModels without the turned-off models: what /v1/models shows.
func ServedModels(hub *Hub) []domain.ModelInfo {
	off := DisabledModels(hub)
	all := ListModels(hub)
	if len(off) == 0 {
		return all
	}
	out := make([]domain.ModelInfo, 0, len(all))
	for _, m := range all {
		if !off[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// SetModelEnabled turns one model ID on or off.
func SetModelEnabled(hub *Hub, id string, on bool) error {
	off := DisabledModels(hub)
	if on {
		delete(off, id)
	} else {
		off[id] = true
	}
	ids := make([]string, 0, len(off))
	for k := range off {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	return hub.Settings.SetJSON(SettingDisabledModels, ids)
}

var (
	bracketSuffix = regexp.MustCompile(`\[[^\]]*\]$`) // claude-opus-4-7[1m]
	dateSuffix    = regexp.MustCompile(`-\d{8}$`)     // claude-sonnet-4-5-20250929
)

// modelDisabled reports whether a requested model is turned off. Clients add
// variants to listed IDs (a [1m] context tag, a release date), so those count
// as the listed ID.
func modelDisabled(off map[string]bool, model string) bool {
	if len(off) == 0 {
		return false
	}
	m := strings.TrimSpace(model)
	if off[m] {
		return true
	}
	m = bracketSuffix.ReplaceAllString(m, "")
	if off[m] {
		return true
	}
	return off[dateSuffix.ReplaceAllString(m, "")]
}
