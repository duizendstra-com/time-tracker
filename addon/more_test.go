package addon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/duizendstra-com/time-tracker/card"
	"github.com/duizendstra-com/time-tracker/entry"
	"github.com/duizendstra-com/time-tracker/remote"
)

type fakeSheets struct {
	header []string
	rows   [][]any
	made   bool
	err    error
}

func (f *fakeSheets) Write(ctx context.Context, header []string, rows [][]any) (string, bool, error) {
	f.header, f.rows = header, rows
	return "sheet1", f.made, f.err
}

func withSheets(s *Service, f *fakeSheets) *Service {
	s.Sheets = func(token string) Sheets { return f }
	return s
}

// scoped is an event object whose grant lists scopes.
func scoped(do string, scopes []string) string {
	b, _ := json.Marshal(map[string]any{
		"commonEventObject":        map[string]any{"hostApp": "CALENDAR", "parameters": map[string]string{"do": do}},
		"authorizationEventObject": map[string]any{"userOAuthToken": "tok", "authorizedScopes": scopes},
	})
	return string(b)
}

const execute = "https://www.googleapis.com/auth/calendar.addons.execute"

func TestAsksForUngrantedScopes(t *testing.T) {
	s := withSheets(service(newFake(), nil), &fakeSheets{})
	// Calendar unticked: the homepage asks for it, and draws nothing.
	res := post(t, s, scoped("", []string{execute}))
	if res.Scopes == nil || strings.Join(res.Scopes.Scopes, ",") != scopeCalendar || res.RenderActions != nil {
		t.Errorf("homepage without calendar = %+v", res)
	}
	// Calendar granted, drive.file not: Sync asks for drive.file only.
	res = post(t, s, scoped("sync", []string{execute, scopeCalendar}))
	if res.Scopes == nil || strings.Join(res.Scopes.Scopes, ",") != scopeDriveFile {
		t.Errorf("sync without drive.file = %+v", res)
	}
	// Both granted: Sync runs.
	if res = post(t, s, scoped("sync", []string{execute, scopeCalendar, scopeDriveFile})); res.Scopes != nil {
		t.Errorf("sync with both = %+v", res)
	}
	// The wire form is Workspace's own.
	b, _ := json.Marshal(card.Ask(scopeDriveFile))
	if string(b) != `{"requesting_google_scopes":{"scopes":["`+scopeDriveFile+`"]}}` {
		t.Errorf("ask = %s", b)
	}
}

func TestSyncFailsOnGoogleScopeRefusalWithAsk(t *testing.T) {
	f := &fakeSheets{err: &remote.APIError{Status: 403, Reason: "insufficientPermissions", Message: "no"}}
	res := post(t, withSheets(service(newFake(), nil), f), event("sync", nil, nil))
	if res.Scopes == nil || res.Scopes.Scopes[0] != scopeDriveFile {
		t.Errorf("sync refused = %+v", res)
	}
}

func TestBillable(t *testing.T) {
	store := newFake()
	s := service(store, nil)
	c := shown(t, post(t, s, event("", nil, nil)))
	if in := dropdown(c, "billable"); in == nil || in.Type != "CHECK_BOX" || chosen(in) != "" {
		t.Fatalf("billable box = %+v", in)
	}
	post(t, s, event("create", nil, map[string]any{
		"client": "A", "project": "B", "task": "C", "billable": "yes", "date": day(2026, 9, 28), "start": "09:00",
	}))
	e := store.entries["ev1"]
	if !e.Billable || e.Title() != "A · B · C" {
		t.Fatalf("made %+v", e)
	}
	at := map[string]string{"calendarId": "cal1", "eventId": "ev1"}
	if c := shown(t, post(t, s, event("edit", at, nil))); chosen(dropdown(c, "billable")) != "yes" {
		t.Errorf("edit form lost billable: %s", text(c))
	}
	// Unticked, the box sends nothing.
	post(t, s, event("update", at, map[string]any{"client": "A", "project": "B", "task": "C", "date": day(2026, 9, 28), "start": "09:00"}))
	if store.entries["ev1"].Billable {
		t.Error("unticking kept billable")
	}
}

func TestSyncWritesEveryEntrySorted(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 9, 28, h, 0, 0, 0, entry.Zone) }
	store := newFake(
		entry.Entry{Client: "B", Project: "P", Task: "T", Start: at(14), End: at(15), UID: "b@google.com"},
		entry.Entry{Client: "A", Project: "P", Task: "T", Billable: true, Start: at(9), End: at(11), UID: "a@google.com"},
	)
	f := &fakeSheets{made: true}
	c := shown(t, post(t, withSheets(service(store, nil), f), event("sync", nil, nil)))
	if strings.Join(f.header, ",") != "Date,Start,End,Hours,Client,Project,Task,Billable,Description,Event id" {
		t.Errorf("header = %v", f.header)
	}
	if len(f.rows) != 2 || f.rows[0][9] != "a@google.com" || f.rows[0][7] != true || f.rows[0][3] != 2.0 {
		t.Errorf("rows = %v", f.rows)
	}
	if !strings.Contains(text(c), "2 entries synced.") || !strings.Contains(text(c), "https://docs.google.com/spreadsheets/d/sheet1/edit") ||
		!strings.Contains(text(c), "Sync made a spreadsheet") {
		t.Errorf("synced card = %s", text(c))
	}
	// The homepage offers Sync and Rename.
	if h := text(shown(t, post(t, withSheets(service(store, nil), f), event("", nil, nil)))); !strings.Contains(h, "Sync to sheet") || !strings.Contains(h, "Rename…") {
		t.Errorf("homepage = %s", h)
	}
}

func renameStore() *fakeStore {
	return newFake(
		entry.Entry{Client: "Acme", Project: "Site", Task: "Design"},
		entry.Entry{Client: "Acme", Project: "Site", Task: "Build"},
		entry.Entry{Client: "Acme", Project: "App", Task: "Design"},
		entry.Entry{Client: "Beta", Project: "Site", Task: "Design"},
	)
}

func titles(f *fakeStore) string {
	var out []string
	for _, id := range []string{"ev1", "ev2", "ev3", "ev4"} {
		out = append(out, f.entries[id].Title())
	}
	return strings.Join(out, " | ")
}

func TestRenameAsksThenRewrites(t *testing.T) {
	store := renameStore()
	s := service(store, nil)
	c := shown(t, post(t, s, event("manage", nil, nil)))
	if chosen(dropdown(c, "renClient")) != "Acme" || chosen(dropdown(c, "renProject")) != "" || dropdown(c, "renTask") != nil {
		t.Fatalf("manage = %s", text(c))
	}
	// Acme's project Site, to Website: two entries; Beta's Site stays.
	c = shown(t, post(t, s, event("askRename", nil, map[string]any{"renClient": "Acme", "renProject": "Site", "renTo": "Website"})))
	if c.Header.Title != "Rename this project?" || c.Header.Subtitle != "Site → Website" || !strings.Contains(text(c), "2 entries change") {
		t.Fatalf("confirm = %s", text(c))
	}
	if titles(store) != "Acme · Site · Design | Acme · Site · Build | Acme · App · Design | Beta · Site · Design" {
		t.Fatal("asking wrote entries")
	}
	res := post(t, s, event("rename", map[string]string{"client": "Acme", "project": "Site", "to": "Website"}, nil))
	if got := titles(store); got != "Acme · Website · Design | Acme · Website · Build | Acme · App · Design | Beta · Site · Design" {
		t.Errorf("after = %s", got)
	}
	if n := res.RenderActions.Action.Notification; n == nil || n.Text != "Renamed in 2 entries." {
		t.Errorf("notification = %+v", n)
	}
	if store.entries["ev1"].Tags()["project"] != "Website" {
		t.Error("tag not rewritten")
	}
	// A task, into one that exists: the confirm card says they merge.
	c = shown(t, post(t, s, event("askRename", nil, map[string]any{"renClient": "Acme", "renProject": "Website", "renTask": "Build", "renTo": "Design"})))
	if !strings.Contains(text(c), "1 entry change") || !strings.Contains(text(c), "the two become one") {
		t.Errorf("merge confirm = %s", text(c))
	}
	// A client.
	post(t, s, event("rename", map[string]string{"client": "Beta", "to": "Gamma"}, nil))
	if store.entries["ev4"].Title() != "Gamma · Site · Design" {
		t.Errorf("client rename = %s", store.entries["ev4"].Title())
	}
}

func TestRenameRefuses(t *testing.T) {
	s := service(renameStore(), nil)
	for inputs, want := range map[string]string{
		`{"renClient":"Acme"}`:                                 "Type the new name.",
		`{"renClient":"Acme","renTo":"Acme"}`:                  "That is its name already.",
		`{"renClient":"Acme","renProject":"Gone","renTo":"X"}`: "No entry uses that name any more.",
	} {
		var in map[string]any
		_ = json.Unmarshal([]byte(inputs), &in)
		if c := shown(t, post(t, s, event("askRename", nil, in))); !strings.Contains(text(c), want) {
			t.Errorf("%s: card = %s", inputs, text(c))
		}
	}
	if c := shown(t, post(t, service(newFake(), nil), event("manage", nil, nil))); !strings.Contains(text(c), "Nothing to rename yet.") {
		t.Errorf("empty manage = %s", text(c))
	}
}

func TestRenamePartialFailureFinishesOnRetry(t *testing.T) {
	store := renameStore()
	store.failUpdate = map[string]bool{"ev2": true}
	s := service(store, nil)
	p := map[string]string{"client": "Acme", "to": "Acme Corp"}
	res := post(t, s, event("rename", p, nil))
	if n := res.RenderActions.Action.Notification; n == nil || n.Text != "Renamed 2 of 3. The rest failed: Backend Error Choose Rename again to finish." {
		t.Errorf("notification = %+v", n)
	}
	store.failUpdate = nil
	res = post(t, s, event("rename", p, nil))
	if n := res.RenderActions.Action.Notification; n == nil || n.Text != "Renamed in 1 entry." {
		t.Errorf("retry notification = %+v", n)
	}
	if got := titles(store); !strings.HasPrefix(got, "Acme Corp · Site · Design | Acme Corp · Site · Build | Acme Corp · App · Design") {
		t.Errorf("after retry = %s", got)
	}
}
