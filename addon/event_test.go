package addon

import (
	"encoding/json"
	"testing"
)

func TestDateShapes(t *testing.T) {
	const want = int64(1759276800000) // 2025-10-01 00:00 UTC
	for name, in := range map[string]string{
		"dateInput string":     `{"dateInput":{"msSinceEpoch":"1759276800000"}}`,
		"dateInput number":     `{"dateInput":{"msSinceEpoch":1759276800000}}`,
		"dateInput exponent":   `{"dateInput":{"msSinceEpoch":1.7592768E12}}`,
		"dateTimeInput string": `{"dateTimeInput":{"hasDate":true,"hasTime":false,"msSinceEpoch":"1759276800000"}}`,
	} {
		var ev Event
		if err := json.Unmarshal([]byte(`{"commonEventObject":{"formInputs":{"date":`+in+`}}}`), &ev); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, ok := ev.Date("date"); !ok || got != want {
			t.Errorf("%s: Date = %d, %v; want %d, true", name, got, ok, want)
		}
		if raw := ev.Raw("date"); raw != in {
			t.Errorf("%s: Raw = %s; want %s", name, raw, in)
		}
	}
	var ev Event
	if _, ok := ev.Date("date"); ok {
		t.Error("absent date: ok is true")
	}
	if raw := ev.Raw("date"); raw != "absent" {
		t.Errorf("absent date: Raw = %q", raw)
	}
}
