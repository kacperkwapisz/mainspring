package codex

import (
	"encoding/json"
	"slices"
	"testing"
)

// Shape of a real /backend-api/codex/models answer (trimmed).
const modelsAnswer = `{"models":[
 {"slug":"gpt-6-luna","display_name":"GPT-6 Luna","visibility":"list","context_window":272000,"input_modalities":["text","image"],
  "supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"},{"effort":"max"}]},
 {"slug":"codex-auto-review","display_name":"Auto review","visibility":"hide","context_window":272000,"supported_reasoning_levels":[{"effort":"low"}]},
 {"slug":"gpt-5.6-terra","display_name":"GPT-5.6 Terra","visibility":"list","context_window":272000,"input_modalities":["text","image"],
  "supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"},{"effort":"max"},{"effort":"ultra"}]}
]}`

func TestParseModelsSkipsInternalModels(t *testing.T) {
	var body any
	_ = json.Unmarshal([]byte(modelsAnswer), &body)
	got := ParseModels(body)
	if len(got) != 2 || got[0].ID != "gpt-6-luna" || got[1].ID != "gpt-5.6-terra" {
		t.Fatalf("%+v", got)
	}
	if got[0].ContextLength != 272000 || !got[0].Reasoning || !slices.Equal(got[1].Efforts, []string{"low", "medium", "high", "xhigh", "max", "ultra"}) {
		t.Fatalf("%+v", got)
	}
}
