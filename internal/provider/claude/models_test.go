package claude

import (
	"encoding/json"
	"slices"
	"testing"
)

// Shape of a real /v1/models answer to a subscription sign-in (trimmed).
const modelsAnswer = `{"data":[
 {"type":"model","id":"claude-fable-5-2","display_name":"Claude Fable 5.2","max_input_tokens":1000000,"max_tokens":128000,"lifecycle":"active",
  "capabilities":{"image_input":{"supported":true},"thinking":{"supported":true},
   "effort":{"supported":true,"low":{"supported":true},"medium":{"supported":true},"high":{"supported":true},"xhigh":{"supported":true},"max":{"supported":true}}}},
 {"type":"model","id":"claude-sonnet-4-6","display_name":"Claude Sonnet 4.6","max_input_tokens":1000000,"max_tokens":128000,"lifecycle":"active",
  "capabilities":{"image_input":{"supported":true},"thinking":{"supported":true},
   "effort":{"supported":true,"low":{"supported":true},"medium":{"supported":true},"high":{"supported":true},"xhigh":{"supported":false},"max":{"supported":true}}}},
 {"type":"model","id":"claude-haiku-4-5-20251001","display_name":"Claude Haiku 4.5","max_input_tokens":200000,"max_tokens":64000,"lifecycle":"active",
  "capabilities":{"image_input":{"supported":true},"thinking":{"supported":true},"effort":{"supported":false}}}
],"has_more":false}`

func TestParseModelsKeepsEachModelsOwnLimits(t *testing.T) {
	var body any
	_ = json.Unmarshal([]byte(modelsAnswer), &body)
	got := ParseModels(body)
	if len(got) != 3 || got[0].ID != "claude-fable-5-2" || got[0].Name != "Claude Fable 5.2" {
		t.Fatalf("%+v", got)
	}
	if got[0].ContextLength != 1000000 || got[0].MaxOutputTokens != 128000 || !slices.Equal(got[0].Efforts, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Fatalf("fable %+v", got[0])
	}
	if !slices.Equal(got[1].Efforts, []string{"low", "medium", "high", "max"}) {
		t.Fatalf("sonnet 4.6 has no xhigh: %v", got[1].Efforts)
	}
	if got[2].ContextLength != 200000 || len(got[2].Efforts) != 0 || !slices.Equal(got[2].Input, []string{"text", "image"}) {
		t.Fatalf("haiku %+v", got[2])
	}
}
