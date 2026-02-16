package api

import "testing"

func TestSanitizeEventMetadataDropsRawTextFields(t *testing.T) {
	input := map[string]any{
		"message": "secret prompt",
		"content": "raw draft text",
		"slug":    "growth-bot",
		"nested": map[string]any{
			"text":       "keep out",
			"room_id":    "room-1",
			"long_value": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		"items": []any{
			map[string]any{
				"body":       "hidden",
				"persona_id": "persona-1",
			},
			"ok",
		},
	}

	got := sanitizeEventMetadata(input)

	if _, exists := got["message"]; exists {
		t.Fatalf("message key must be removed")
	}
	if _, exists := got["content"]; exists {
		t.Fatalf("content key must be removed")
	}
	if got["slug"] != "growth-bot" {
		t.Fatalf("slug mismatch: %#v", got["slug"])
	}

	nested, ok := got["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested map missing")
	}
	if _, exists := nested["text"]; exists {
		t.Fatalf("nested text key must be removed")
	}
	if nested["room_id"] != "room-1" {
		t.Fatalf("nested room_id mismatch: %#v", nested["room_id"])
	}
	if len(nested["long_value"].(string)) != 180 {
		t.Fatalf("long value should be truncated to 180 chars")
	}

	items, ok := got["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items list mismatch: %#v", got["items"])
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("first item should be object")
	}
	if _, exists := first["body"]; exists {
		t.Fatalf("body key must be removed from list item")
	}
	if first["persona_id"] != "persona-1" {
		t.Fatalf("persona_id mismatch: %#v", first["persona_id"])
	}
}
