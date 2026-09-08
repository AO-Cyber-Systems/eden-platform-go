package cms

import (
	"encoding/json"
	"reflect"
	"testing"
)

// decodeExtra is a test helper that mirrors how a caller actually obtains
// the map[string]any ParseBlocks expects: decode arbitrary JSON into
// interface{} the same way encoding/json (or a JSONB driver) does.
func decodeExtra(t *testing.T, raw string) map[string]any {
	t.Helper()
	var extra map[string]any
	if err := json.Unmarshal([]byte(raw), &extra); err != nil {
		t.Fatalf("decodeExtra: %v", err)
	}
	return extra
}

func TestParseBlocks_NilExtra(t *testing.T) {
	if got := ParseBlocks(nil); got != nil {
		t.Errorf("ParseBlocks(nil) = %#v, want nil", got)
	}
}

func TestParseBlocks_AbsentBlocksKey(t *testing.T) {
	extra := decodeExtra(t, `{"title": "About Us"}`)
	got := ParseBlocks(extra)
	if got != nil {
		t.Errorf("ParseBlocks(no blocks key) = %#v, want nil", got)
	}
}

func TestParseBlocks_NonArrayBlocksValue(t *testing.T) {
	cases := []string{
		`{"blocks": "not-an-array"}`,
		`{"blocks": {"type": "banner"}}`,
		`{"blocks": 42}`,
		`{"blocks": null}`,
		`{"blocks": true}`,
	}
	for _, raw := range cases {
		extra := decodeExtra(t, raw)
		got := ParseBlocks(extra)
		if got != nil {
			t.Errorf("ParseBlocks(%s) = %#v, want nil", raw, got)
		}
	}
}

func TestParseBlocks_EmptyArray(t *testing.T) {
	extra := decodeExtra(t, `{"blocks": []}`)
	got := ParseBlocks(extra)
	if len(got) != 0 {
		t.Errorf("ParseBlocks(empty array) = %#v, want empty", got)
	}
}

func TestParseBlocks_NonObjectElementSkipped(t *testing.T) {
	extra := decodeExtra(t, `{"blocks": ["a string", 42, null, true, {"id": "1", "type": "banner", "data": {}}]}`)
	got := ParseBlocks(extra)
	want := []Block{{ID: "1", Type: "banner", Data: map[string]any{}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseBlocks = %#v, want %#v", got, want)
	}
}

func TestParseBlocks_MissingTypeSkipped(t *testing.T) {
	extra := decodeExtra(t, `{"blocks": [
		{"id": "1", "data": {"headline": "no type here"}},
		{"id": "2", "type": "", "data": {}},
		{"id": "3", "type": "banner", "data": {"headline": "kept"}}
	]}`)
	got := ParseBlocks(extra)
	want := []Block{{ID: "3", Type: "banner", Data: map[string]any{"headline": "kept"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseBlocks = %#v, want %#v", got, want)
	}
}

func TestParseBlocks_NonStringTypeSkipped(t *testing.T) {
	extra := decodeExtra(t, `{"blocks": [{"id": "1", "type": 7, "data": {}}]}`)
	got := ParseBlocks(extra)
	if len(got) != 0 {
		t.Errorf("ParseBlocks(non-string type) = %#v, want empty", got)
	}
}

func TestParseBlocks_NullDataYieldsNilMap(t *testing.T) {
	extra := decodeExtra(t, `{"blocks": [{"id": "1", "type": "banner", "data": null}]}`)
	got := ParseBlocks(extra)
	if len(got) != 1 {
		t.Fatalf("ParseBlocks() len = %d, want 1", len(got))
	}
	if got[0].Data != nil {
		t.Errorf("Data = %#v, want nil", got[0].Data)
	}
}

func TestParseBlocks_MissingDataYieldsNilMap(t *testing.T) {
	extra := decodeExtra(t, `{"blocks": [{"id": "1", "type": "banner"}]}`)
	got := ParseBlocks(extra)
	if len(got) != 1 {
		t.Fatalf("ParseBlocks() len = %d, want 1", len(got))
	}
	if got[0].Data != nil {
		t.Errorf("Data = %#v, want nil", got[0].Data)
	}
}

func TestParseBlocks_DuplicateTypesDistinctIDsAllSurvive(t *testing.T) {
	extra := decodeExtra(t, `{"blocks": [
		{"id": "a", "type": "callout", "data": {"n": 1}},
		{"id": "b", "type": "callout", "data": {"n": 2}},
		{"id": "c", "type": "callout", "data": {"n": 3}}
	]}`)
	got := ParseBlocks(extra)
	if len(got) != 3 {
		t.Fatalf("ParseBlocks() len = %d, want 3", len(got))
	}
	seen := map[string]bool{}
	for _, b := range got {
		if b.Type != "callout" {
			t.Errorf("Type = %q, want callout", b.Type)
		}
		if seen[b.ID] {
			t.Errorf("duplicate id %q survived only once", b.ID)
		}
		seen[b.ID] = true
	}
	if !seen["a"] || !seen["b"] || !seen["c"] {
		t.Errorf("not all distinct ids survived: %#v", seen)
	}
}

func TestParseBlocks_OrderPreservedExactly(t *testing.T) {
	extra := decodeExtra(t, `{"blocks": [
		{"id": "1", "type": "banner", "data": {}},
		{"id": "2", "type": "markdown", "data": {}},
		{"id": "3", "type": "banner", "data": {}},
		{"id": "4", "type": "gallery", "data": {}}
	]}`)
	got := ParseBlocks(extra)
	wantOrder := []string{"1", "2", "3", "4"}
	if len(got) != len(wantOrder) {
		t.Fatalf("ParseBlocks() len = %d, want %d", len(got), len(wantOrder))
	}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Errorf("position %d: ID = %q, want %q", i, got[i].ID, id)
		}
	}
}

func TestParseBlocks_OneMalformedBlockDoesNotFailSiblings(t *testing.T) {
	extra := decodeExtra(t, `{"blocks": [
		{"id": "1", "type": "banner", "data": {"headline": "ok"}},
		"garbage",
		{"id": "2", "data": {"no": "type"}},
		{"id": "3", "type": "markdown", "data": {"source": "ok"}}
	]}`)
	got := ParseBlocks(extra)
	want := []Block{
		{ID: "1", Type: "banner", Data: map[string]any{"headline": "ok"}},
		{ID: "3", Type: "markdown", Data: map[string]any{"source": "ok"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseBlocks = %#v, want %#v", got, want)
	}
}

func TestParseBlocks_MissingIDDefaultsToEmptyString(t *testing.T) {
	extra := decodeExtra(t, `{"blocks": [{"type": "banner", "data": {}}]}`)
	got := ParseBlocks(extra)
	if len(got) != 1 {
		t.Fatalf("ParseBlocks() len = %d, want 1", len(got))
	}
	if got[0].ID != "" {
		t.Errorf("ID = %q, want empty string", got[0].ID)
	}
}
