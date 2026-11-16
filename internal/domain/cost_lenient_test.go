package domain

import (
	"encoding/json"
	"testing"
)

// One odd tier must not cost a model its whole price.
func TestCostSkipsTiersThatDoNotParse(t *testing.T) {
	tests := []struct {
		name  string
		tiers string
		want  []int // thresholds kept
	}{
		{"size as text with a unit", `[{"input":9,"tier":{"type":"context","size":"200k"}},{"input":4,"tier":{"type":"context","size":300000}}]`, []int{300000}},
		{"size as a float", `[{"input":4,"tier":{"type":"context","size":200000.5}}]`, []int{200000}},
		{"size as a numeric string", `[{"input":4,"tier":{"type":"context","size":"200000"}}]`, []int{200000}},
		{"tier as a string", `[{"input":4,"tier":"context"},{"input":5,"tier":{"type":"context","size":400000}}]`, []int{400000}},
		{"a rate that isn't a number", `[{"input":"four","tier":{"type":"context","size":200000}}]`, nil},
		{"tiers as an object", `{"input":4,"tier":{"type":"context","size":200000}}`, nil},
		{"tiers as null", `null`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c ModelCostJSON
			if err := json.Unmarshal([]byte(`{"input":2,"output":12,"tiers":`+tc.tiers+`}`), &c); err != nil {
				t.Fatalf("whole cost failed: %v", err)
			}
			if c.Input == nil || *c.Input != 2 || c.Output == nil || *c.Output != 12 {
				t.Fatalf("base rates lost: %+v", c)
			}
			var got []int
			for _, tier := range c.Tiers {
				got = append(got, tier.InputTokensAbove)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("tiers %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("tiers %v, want %v", got, tc.want)
				}
			}
		})
	}
}
