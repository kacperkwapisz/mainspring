package domain

import (
	"encoding/json"
	"testing"
)

func TestCostReadsModelsDevTiersAndWritesOurs(t *testing.T) {
	var c ModelCostJSON
	in := `{"input":0.1,"output":0.5,"cache_read":0.01,"cache_write":0.125,"tiers":[
	  {"input":9,"tier":{"type":"time","size":5}},
	  {"input":0.5,"output":2.5,"cache_read":0.05,"cache_write":0.625,"tier":{"type":"context","size":100000}}]}`
	if err := json.Unmarshal([]byte(in), &c); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"input":0.1,"output":0.5,"cache_read":0.01,"cache_write":0.125,"tiers":[{"input_tokens_above":100000,"input":0.5,"output":2.5,"cache_read":0.05,"cache_write":0.625}]}`
	if string(out) != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
	// Our own output reads back the same.
	var back ModelCostJSON
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(back)
	if string(again) != want {
		t.Fatalf("round trip %s", again)
	}
}

func TestCostWithoutTiersOmitsThem(t *testing.T) {
	var c ModelCostJSON
	if err := json.Unmarshal([]byte(`{"input":1,"tiers":[{"input":2,"tier":{"type":"time","size":1}}]}`), &c); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(c)
	if string(out) != `{"input":1}` {
		t.Fatalf("got %s", out)
	}
}
