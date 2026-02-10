package thingscloud

import (
	"encoding/json"
	"testing"
)

func TestNote_FullText(t *testing.T) {
	raw := `{"_t":"tx","t":1,"ch":0,"v":"Hello world"}`
	var n Note
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatal(err)
	}
	if n.Type != NoteTypeFullText {
		t.Errorf("expected type %d, got %d", NoteTypeFullText, n.Type)
	}
	if n.Value != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", n.Value)
	}
}

func TestNote_Delta(t *testing.T) {
	raw := `{"_t":"tx","t":2,"ps":[{"r":"inserted text","p":0,"l":0,"ch":12345}]}`
	var n Note
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatal(err)
	}
	if n.Type != NoteTypeDelta {
		t.Errorf("expected type %d, got %d", NoteTypeDelta, n.Type)
	}
	if len(n.Patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(n.Patches))
	}
	if n.Patches[0].Replacement != "inserted text" {
		t.Errorf("unexpected replacement: %s", n.Patches[0].Replacement)
	}
}

func TestNote_ApplyPatch(t *testing.T) {
	original := "Hello world"
	patch := NotePatch{Position: 5, Length: 6, Replacement: " Go"}
	result := ApplyPatches(original, []NotePatch{patch})
	if result != "Hello Go" {
		t.Errorf("expected 'Hello Go', got '%s'", result)
	}
}

func TestNote_ApplyMultiplePatches(t *testing.T) {
	original := "ABCDEF"
	patches := []NotePatch{
		{Position: 0, Length: 1, Replacement: "X"},
	}
	result := ApplyPatches(original, patches)
	if result != "XBCDEF" {
		t.Errorf("expected 'XBCDEF', got '%s'", result)
	}
}
