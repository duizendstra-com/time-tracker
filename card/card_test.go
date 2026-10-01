package card

import (
	"encoding/json"
	"testing"
)

// Calendar parses the body as RenderActions, so "action" must be the top-level
// key; a "renderActions" wrapper fails with "Cannot find field: renderActions".
func TestResponseIsBareRenderActions(t *testing.T) {
	for name, r := range map[string]Response{
		"push":   Push(Card{Header: Head("Time Tracker", "")}),
		"notify": Notify("Entry updated."),
		"with":   Update(Card{}).With("Saved."),
	} {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(b, &top); err != nil {
			t.Fatal(err)
		}
		if _, ok := top["action"]; !ok || len(top) != 1 {
			t.Errorf("%s: want only a top-level \"action\", got %s", name, b)
		}
	}
	if b, _ := json.Marshal(Ask("s")); string(b) != `{"requesting_google_scopes":{"scopes":["s"]}}` {
		t.Errorf("ask: got %s", b)
	}
}
