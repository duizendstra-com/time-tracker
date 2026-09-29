package entry

import (
	"reflect"
	"testing"
	"time"
)

func TestTitleAndDescription(t *testing.T) {
	e := Entry{Client: "Acme", Project: "Site", Task: "Design", Note: "Header"}
	if got, want := e.Title(), "Acme · Site · Design"; got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
	if got, want := e.Description(), "Header\n\n— Logged with Time Tracker"; got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
}

func TestRead(t *testing.T) {
	e, ok := Read(map[string]string{"client": "Acme", "project": "Site", "task": "Design"}, "Header"+Signature)
	if !ok || e.Note != "Header" || e.Title() != "Acme · Site · Design" {
		t.Errorf("Read = %+v, %v", e, ok)
	}
	if _, ok := Read(map[string]string{"project": "Site"}, ""); ok {
		t.Error("an event with no client tag read as an entry")
	}
	// A description someone edited keeps what they wrote.
	if e, _ := Read(map[string]string{"client": "A"}, "edited"); e.Note != "edited" {
		t.Errorf("Note = %q", e.Note)
	}
}

func TestMissing(t *testing.T) {
	if got := (Entry{Client: "A", Task: " "}).Missing(); !reflect.DeepEqual(got, []string{"project", "task"}) {
		t.Errorf("Missing() = %v", got)
	}
}

func TestListsOf(t *testing.T) {
	lists := ListsOf([]Entry{
		{Client: "beta", Project: "P", Task: "t2"},
		{Client: "Acme", Project: "Site", Task: "Design"},
		{Client: "beta", Project: "P", Task: "t1"},
		{Client: "beta", Project: "P", Task: "t1"},
	})
	want := Lists{
		{Name: "Acme", Projects: []Project{{Name: "Site", Tasks: []string{"Design"}}}},
		{Name: "beta", Projects: []Project{{Name: "P", Tasks: []string{"t1", "t2"}}}},
	}
	if !reflect.DeepEqual(lists, want) {
		t.Errorf("ListsOf = %+v, want %+v", lists, want)
	}
}

func TestZone(t *testing.T) {
	// Tirane keeps CET/CEST: 09:00 on 28 September 2026 is 07:00 UTC.
	at := time.Date(2026, 9, 28, 9, 0, 0, 0, Zone)
	if got := at.UTC().Format("15:04"); got != "07:00" {
		t.Errorf("09:00 Tirane = %s UTC", got)
	}
}

func TestBillable(t *testing.T) {
	if got := (Entry{Client: "A"}).Tags()["billable"]; got != "no" {
		t.Errorf("billable tag = %q, want no", got)
	}
	if got := (Entry{Client: "A", Billable: true}).Tags()["billable"]; got != "yes" {
		t.Errorf("billable tag = %q, want yes", got)
	}
	for tag, want := range map[string]bool{"yes": true, "no": false, "": false, "Yes": false} {
		tags := map[string]string{"client": "A"}
		if tag != "" {
			tags["billable"] = tag
		}
		if e, _ := Read(tags, ""); e.Billable != want {
			t.Errorf("billable %q read as %v", tag, e.Billable)
		}
	}
}

func TestRow(t *testing.T) {
	start := time.Date(2026, 9, 28, 9, 0, 0, 0, Zone)
	e := Entry{Client: "Acme", Project: "Site", Task: "Design", Note: "Header", Billable: true,
		Start: start, End: start.Add(100 * time.Minute), UID: "x@google.com"}
	want := []any{"2026-09-28", "09:00", "10:40", 1.67, "Acme", "Site", "Design", true, "Header", "x@google.com"}
	if got := e.Row(); !reflect.DeepEqual(got, want) {
		t.Errorf("Row() = %v, want %v", got, want)
	}
	if len(Columns) != len(want) {
		t.Errorf("%d columns, %d cells", len(Columns), len(want))
	}
}
