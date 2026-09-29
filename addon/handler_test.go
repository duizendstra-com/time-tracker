package addon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/duizendstra-com/time-tracker/card"
	"github.com/duizendstra-com/time-tracker/entry"
	"github.com/duizendstra-com/time-tracker/remote"
)

// fakeStore is a calendar in memory.
type fakeStore struct {
	calID   string
	entries map[string]entry.Entry
	next    int
	fail    error
	// failUpdate fails Update for these event ids.
	failUpdate map[string]bool
}

func newFake(es ...entry.Entry) *fakeStore {
	f := &fakeStore{calID: "cal1", entries: map[string]entry.Entry{}}
	for _, e := range es {
		f.next++
		e.ID = "ev" + string(rune('0'+f.next))
		f.entries[e.ID] = e
	}
	return f
}

func (f *fakeStore) CalendarID(ctx context.Context, create bool) (string, error) {
	if f.calID == "" && create {
		f.calID = "made"
	}
	return f.calID, nil
}
func (f *fakeStore) Entries(ctx context.Context, id string) ([]entry.Entry, error) {
	var out []entry.Entry
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out, nil
}
func (f *fakeStore) Get(ctx context.Context, cal, id string) (entry.Entry, bool, error) {
	e, ok := f.entries[id]
	return e, ok, nil
}
func (f *fakeStore) Insert(ctx context.Context, cal string, e entry.Entry) (entry.Entry, error) {
	if f.fail != nil {
		return e, f.fail
	}
	f.next++
	e.ID = "ev" + string(rune('0'+f.next))
	f.entries[e.ID] = e
	return e, nil
}

var storeMu sync.Mutex

func (f *fakeStore) Update(ctx context.Context, cal string, e entry.Entry) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	if f.failUpdate[e.ID] {
		return &remote.APIError{Status: 500, Message: "Backend Error"}
	}
	f.entries[e.ID] = e
	return nil
}
func (f *fakeStore) Delete(ctx context.Context, cal, id string) error {
	delete(f.entries, id)
	return nil
}

type fakeGemini struct {
	p    remote.Proposal
	err  error
	note string
}

func (g *fakeGemini) Propose(ctx context.Context, note string, lists entry.Lists) (remote.Proposal, error) {
	g.note = note
	return g.p, g.err
}

// 2026-09-29 11:10 in Tirane (CEST): the default start is 10:00.
var now = time.Date(2026, 9, 29, 11, 10, 0, 0, entry.Zone)

func service(store *fakeStore, g Proposer) *Service {
	s := &Service{Now: func() time.Time { return now }}
	if store != nil {
		s.Store = func(token string) Store { return store }
	}
	if g != nil {
		s.Gemini = g
	}
	return s
}

// post sends body to s and decodes the answer.
func post(t *testing.T, s *Service, body string) card.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "https://tt.example.run.app/", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var res card.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

// event builds an event object: the token, the action, its parameters and form inputs.
func event(do string, params map[string]string, inputs map[string]any) string {
	p := map[string]string{}
	if do != "" {
		p["do"] = do
	}
	for k, v := range params {
		p[k] = v
	}
	fi := map[string]any{}
	for k, v := range inputs {
		switch v := v.(type) {
		case string:
			fi[k] = map[string]any{"stringInputs": map[string]any{"value": []string{v}}}
		case int64:
			fi[k] = map[string]any{"dateInput": map[string]any{"msSinceEpoch": v}}
		}
	}
	b, _ := json.Marshal(map[string]any{
		"commonEventObject":        map[string]any{"hostApp": "CALENDAR", "parameters": p, "formInputs": fi},
		"authorizationEventObject": map[string]any{"userOAuthToken": "tok"},
	})
	return string(b)
}

func day(y int, m time.Month, d int) int64 {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).UnixMilli()
}

// shown is the card a response draws: its last pushed or updated card.
func shown(t *testing.T, r card.Response) card.Card {
	t.Helper()
	for i := len(r.RenderActions.Action.Navigations) - 1; i >= 0; i-- {
		n := r.RenderActions.Action.Navigations[i]
		if n.PushCard != nil {
			return *n.PushCard
		}
		if n.UpdateCard != nil {
			return *n.UpdateCard
		}
	}
	t.Fatalf("no card in %+v", r)
	return card.Card{}
}

func dropdown(c card.Card, name string) *card.SelectionInput {
	for _, s := range c.Sections {
		for _, w := range s.Widgets {
			if w.SelectionInput != nil && w.SelectionInput.Name == name {
				return w.SelectionInput
			}
		}
	}
	return nil
}

func chosen(in *card.SelectionInput) string {
	for _, it := range in.Items {
		if it.Selected {
			return it.Value
		}
	}
	return ""
}

func items(in *card.SelectionInput) []string {
	var out []string
	for _, it := range in.Items {
		out = append(out, it.Value)
	}
	return out
}

func text(c card.Card) string {
	b, _ := json.Marshal(c)
	return string(b)
}

func TestGetIsRefused(t *testing.T) {
	rec := httptest.NewRecorder()
	service(nil, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405", rec.Code)
	}
}

// The curl on the page: no token, no action. The homepage card, with empty lists.
func TestHomepageWithoutToken(t *testing.T) {
	res := post(t, service(nil, nil), `{"commonEventObject": {"hostApp": "CALENDAR"}}`)
	c := shown(t, res)
	if c.Header.Title != "Time Tracker" || chosen(dropdown(c, "client")) != newItem {
		t.Errorf("homepage = %s", text(c))
	}
	if got := chosen(dropdown(c, "start")); got != "10:00" {
		t.Errorf("default start = %q, want 10:00 (an hour ago, rounded down)", got)
	}
	if got := chosen(dropdown(c, "length")); got != "60" {
		t.Errorf("default length = %q, want 60", got)
	}
	if strings.Contains(text(c), "Describe it") {
		t.Error("Describe it shown with no Gemini")
	}
	if a := dropdown(c, "client").OnChangeAction; a == nil || a.Function != "https://tt.example.run.app/" {
		t.Errorf("onChange calls %+v, want the service's own URL", a)
	}
}

func TestListsAreDerivedAndNarrow(t *testing.T) {
	store := newFake(
		entry.Entry{Client: "Acme", Project: "Site", Task: "Design"},
		entry.Entry{Client: "Acme", Project: "App", Task: "Build"},
		entry.Entry{Client: "Beta", Project: "Ops", Task: "Run"},
	)
	c := shown(t, post(t, service(store, nil), event("", nil, nil)))
	if got := strings.Join(items(dropdown(c, "client")), ","); got != "Acme,Beta,"+newItem {
		t.Errorf("clients = %s", got)
	}
	if got := strings.Join(items(dropdown(c, "project")), ","); got != "App,Site,"+newItem {
		t.Errorf("Acme's projects = %s", got)
	}
	c = shown(t, post(t, service(store, nil), event("change", nil, map[string]any{"client": "Beta", "project": "App"})))
	if got := strings.Join(items(dropdown(c, "project")), ","); got != "Ops,"+newItem || chosen(dropdown(c, "project")) != "Ops" {
		t.Errorf("Beta's projects = %s, chosen %s", got, chosen(dropdown(c, "project")))
	}
	c = shown(t, post(t, service(store, nil), event("change", nil, map[string]any{"client": newItem})))
	if !strings.Contains(text(c), `"name":"newClient"`) || chosen(dropdown(c, "project")) != newItem {
		t.Errorf("+ New client form = %s", text(c))
	}
}

func TestCreate(t *testing.T) {
	store := newFake(entry.Entry{Client: "Acme", Project: "Site", Task: "Design"})
	res := post(t, service(store, nil), event("create", nil, map[string]any{
		"client": "Acme", "project": "Site", "task": newItem, "newTask": "Review",
		"description": "Header", "date": day(2026, 9, 28), "start": "09:00", "length": "90",
	}))
	if n := res.RenderActions.Action.Notification; n == nil || n.Text != "Entry created in the Time Tracker calendar." {
		t.Errorf("notification = %+v", n)
	}
	var made entry.Entry
	for _, e := range store.entries {
		if e.Task == "Review" {
			made = e
		}
	}
	if made.Title() != "Acme · Site · Review" || made.Note != "Header" {
		t.Fatalf("made %+v", made)
	}
	// 09:00 in Tirane on 28 September (CEST) is 07:00 UTC; 90 minutes long.
	if got := made.Start.UTC().Format(time.RFC3339); got != "2026-09-28T07:00:00Z" {
		t.Errorf("start = %s", got)
	}
	if made.End.Sub(made.Start) != 90*time.Minute {
		t.Errorf("length = %s", made.End.Sub(made.Start))
	}
	// Winter time: 2 November 09:00 is 08:00 UTC.
	post(t, service(store, nil), event("create", nil, map[string]any{
		"client": "Acme", "project": "Site", "task": "Design", "date": day(2026, 11, 2), "start": "09:00",
	}))
	found := false
	for _, e := range store.entries {
		if e.Start.UTC().Format(time.RFC3339) == "2026-11-02T08:00:00Z" && e.End.Sub(e.Start) == time.Hour {
			found = true
		}
	}
	if !found {
		t.Error("no one-hour entry at 08:00 UTC on 2 November")
	}
	if c := shown(t, res); c.Header.Title != "Time logged" || !strings.Contains(text(c), "Monday 28 September, 09:00 – 10:30") {
		t.Errorf("success card = %s", text(c))
	}
}

func TestCreateRefusesMissing(t *testing.T) {
	store := newFake()
	c := shown(t, post(t, service(store, nil), event("create", nil, map[string]any{
		"client": newItem, "newClient": "Acme", "project": newItem, "task": newItem, "date": day(2026, 9, 28), "start": "09:00",
	})))
	if !strings.Contains(text(c), "Pick or type a project and a task: client, project and task are all required.") {
		t.Errorf("refusal = %s", text(c))
	}
	if !strings.Contains(text(c), `"value":"Acme"`) {
		t.Error("the typed client was lost")
	}
	if len(store.entries) != 0 {
		t.Error("an entry was written")
	}
}

func TestCreateFailureShowsGoogleMessageOnly(t *testing.T) {
	store := newFake()
	store.fail = &remote.APIError{Status: 403, Message: "You do not have permission"}
	res := post(t, service(store, nil), event("create", nil, map[string]any{
		"client": "A", "project": "B", "task": "C", "date": day(2026, 9, 28), "start": "09:00",
	}))
	if n := res.RenderActions.Action.Notification; n == nil || n.Text != "Failed to create event: You do not have permission" {
		t.Errorf("notification = %+v", n)
	}
}

func TestOpenEditUpdateDelete(t *testing.T) {
	store := newFake(entry.Entry{Client: "Acme", Project: "Site", Task: "Design", Note: "Header",
		Start: time.Date(2026, 9, 28, 9, 0, 0, 0, entry.Zone), End: time.Date(2026, 9, 28, 9, 50, 0, 0, entry.Zone)})
	b, _ := json.Marshal(map[string]any{
		"commonEventObject":        map[string]any{"hostApp": "CALENDAR"},
		"authorizationEventObject": map[string]any{"userOAuthToken": "tok"},
		"calendar":                 map[string]any{"id": "ev1", "calendarId": "cal1"},
	})
	c := shown(t, post(t, service(store, nil), string(b)))
	if c.Header.Title != "Design" || c.Header.Subtitle != "Acme · Site" || !strings.Contains(text(c), "09:00 – 09:50") {
		t.Errorf("entry card = %s", text(c))
	}
	at := map[string]string{"calendarId": "cal1", "eventId": "ev1"}
	c = shown(t, post(t, service(store, nil), event("edit", at, nil)))
	if chosen(dropdown(c, "client")) != "Acme" || chosen(dropdown(c, "start")) != "09:00" || chosen(dropdown(c, "length")) != "50" {
		t.Errorf("edit form = %s", text(c))
	}
	res := post(t, service(store, nil), event("update", at, map[string]any{
		"client": "Acme", "project": "Site", "task": newItem, "newTask": "Review",
		"date": day(2026, 9, 28), "start": "14:00", "length": "50",
	}))
	e := store.entries["ev1"]
	if e.Task != "Review" || e.Start.UTC().Format("15:04") != "12:00" || e.End.Sub(e.Start) != 50*time.Minute {
		t.Errorf("updated = %+v", e)
	}
	if n := res.RenderActions.Action.Notification; n == nil || n.Text != "Entry updated." {
		t.Errorf("notification = %+v", n)
	}
	c = shown(t, post(t, service(store, nil), event("askDelete", at, nil)))
	if c.Header.Title != "Delete this entry?" {
		t.Errorf("confirm = %s", text(c))
	}
	post(t, service(store, nil), event("delete", at, nil))
	if _, ok := store.entries["ev1"]; ok {
		t.Error("not deleted")
	}
	if n := post(t, service(store, nil), event("edit", at, nil)).RenderActions.Action.Notification; n == nil || n.Text != "This entry is gone." {
		t.Errorf("gone = %+v", n)
	}
}

func TestNotAnEntry(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"authorizationEventObject": map[string]any{"userOAuthToken": "tok"},
		"calendar":                 map[string]any{"id": "other", "calendarId": "primary"},
	})
	if c := shown(t, post(t, service(newFake(), nil), string(b))); !strings.Contains(text(c), "This event is not a Time Tracker entry.") {
		t.Errorf("card = %s", text(c))
	}
}

func TestProposeFillsTheFormAndWritesNothing(t *testing.T) {
	store := newFake(entry.Entry{Client: "Acme", Project: "Site", Task: "Design"})
	g := &fakeGemini{p: remote.Proposal{Client: "Acme", Project: "Site", Task: "Review", Hours: 2.2, Description: "Reviewed the header"}}
	c := shown(t, post(t, service(store, g), event("propose", nil, map[string]any{"describe": "two hours reviewing acme's header"})))
	if g.note != "two hours reviewing acme's header" {
		t.Errorf("Gemini read %q", g.note)
	}
	if chosen(dropdown(c, "client")) != "Acme" || chosen(dropdown(c, "task")) != "Review" || chosen(dropdown(c, "length")) != "120" {
		t.Errorf("proposed form = %s", text(c))
	}
	if !strings.Contains(text(c), "Gemini filled this in. Check it, then choose Create entry.") || !strings.Contains(text(c), "Reviewed the header") {
		t.Errorf("proposed form = %s", text(c))
	}
	if len(store.entries) != 1 {
		t.Error("propose wrote an entry")
	}
	// A proposed new name stays chosen when another dropdown changes.
	if a := dropdown(c, "client").OnChangeAction; !strings.Contains(text(card.Card{Sections: []card.Section{{Widgets: []card.Widget{{ButtonList: &card.ButtonList{Buttons: []card.Button{{OnClick: &card.OnClick{Action: a}}}}}}}}}), `"prefill"`) {
		t.Error("the proposal is lost on the next change")
	}
}

func TestProposeFailureIsShort(t *testing.T) {
	g := &fakeGemini{err: errors.New("gemini: status 500 with detail")}
	c := shown(t, post(t, service(newFake(), g), event("propose", nil, map[string]any{"describe": "x"})))
	if !strings.Contains(text(c), "Gemini could not read that note.") || strings.Contains(text(c), "detail") {
		t.Errorf("failure card = %s", text(c))
	}
}

func TestHalfHours(t *testing.T) {
	for h, want := range map[float64]time.Duration{0: 30 * time.Minute, 1.2: time.Hour, 1.3: 90 * time.Minute, 40: 12 * time.Hour} {
		if got := halfHours(h); got != want {
			t.Errorf("halfHours(%v) = %s, want %s", h, got, want)
		}
	}
}
