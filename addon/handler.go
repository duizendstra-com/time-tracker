package addon

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/duizendstra-com/time-tracker/card"
	"github.com/duizendstra-com/time-tracker/entry"
	"github.com/duizendstra-com/time-tracker/remote"
)

// Store reads and writes entries as the user.
type Store interface {
	CalendarID(ctx context.Context, create bool) (string, error)
	Entries(ctx context.Context, calendarID string) ([]entry.Entry, error)
	Get(ctx context.Context, calendarID, eventID string) (entry.Entry, bool, error)
	Insert(ctx context.Context, calendarID string, e entry.Entry) (entry.Entry, error)
	Update(ctx context.Context, calendarID string, e entry.Entry) error
	Delete(ctx context.Context, calendarID, eventID string) error
}

// Sheets writes the spreadsheet Sync fills, as the user.
type Sheets interface {
	Write(ctx context.Context, header []string, rows [][]any) (id string, made bool, err error)
}

// Proposer turns a typed note into a proposed entry.
type Proposer interface {
	Propose(ctx context.Context, note string, lists entry.Lists) (remote.Proposal, error)
}

// Service answers Workspace's requests. Every trigger and card action calls the one
// URL; the action it wants travels in the "do" parameter.
type Service struct {
	// Store is the user's calendar, for the token Workspace sent.
	Store func(token string) Store
	// Sheets is the user's spreadsheet, for the token Workspace sent.
	Sheets func(token string) Sheets
	// Gemini is nil when the service has no key; the form then has no Describe it.
	Gemini Proposer
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Log gets each failure's detail; the card gets a short message.
	Log *slog.Logger
	// Project is the Cloud project id; with it each log line names its request's trace.
	Project string
}

// request is one POST: the event object, and what it needs to answer.
type request struct {
	s     *Service
	ctx   context.Context
	ev    *Event
	self  string // the URL Workspace called, which every action calls again
	store Store  // nil without a user token
	log   *slog.Logger
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var ev Event
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&ev); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With("do", ev.Param("do"))
	if trace, _, _ := strings.Cut(r.Header.Get("X-Cloud-Trace-Context"), "/"); trace != "" && s.Project != "" {
		log = log.With("logging.googleapis.com/trace", "projects/"+s.Project+"/traces/"+trace)
	}
	req := &request{s: s, ctx: r.Context(), ev: &ev, self: selfURL(r), log: log}
	if ev.Auth.UserOAuthToken != "" && s.Store != nil {
		req.store = s.Store(ev.Auth.UserOAuthToken)
	}
	res := req.dispatch()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		log.Error("write response", "err", err)
	}
}

// selfURL is the URL this request came to, which card actions call back: https on
// Cloud Run, http on a laptop.
func selfURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") == "" && isLoopback(r.Host) {
		scheme = "http"
	}
	return scheme + "://" + r.Host + r.URL.Path
}

func isLoopback(host string) bool {
	for _, p := range []string{"localhost", "127.0.0.1", "[::1]"} {
		if host == p || len(host) > len(p) && host[:len(p)+1] == p+":" {
			return true
		}
	}
	return false
}

func (r *request) now() time.Time {
	if r.s.Now != nil {
		return r.s.Now()
	}
	return time.Now()
}

// The scopes the actions use. Consent is granular: a user can leave any of them
// unticked, so each action checks for its own before it calls Google.
const (
	scopeCalendar  = "https://www.googleapis.com/auth/calendar"
	scopeDriveFile = "https://www.googleapis.com/auth/drive.file"
)

// missing are the scopes of need the user has not granted. With no token, or when
// Workspace did not list the grant, it finds none, and Google's own refusal decides.
func (r *request) missing(need ...string) []string {
	granted := r.ev.Auth.AuthorizedScopes
	if r.store == nil || granted == nil {
		return nil
	}
	var out []string
	for _, s := range need {
		if !slices.Contains(granted, s) {
			out = append(out, s)
		}
	}
	return out
}

func (r *request) dispatch() card.Response {
	do := r.ev.Param("do")
	need := []string{scopeCalendar}
	switch do {
	case "keep":
		need = nil
	case "sync":
		need = append(need, scopeDriveFile)
	}
	if lack := r.missing(need...); len(lack) > 0 {
		return card.Ask(lack...)
	}
	switch do {
	case "":
		if r.ev.Calendar.ID != "" {
			return r.onEventOpen()
		}
		return card.Push(r.homepage(form{}, "", ""))
	case "change":
		return card.Update(r.formCard(formOf(r.ev), r.lists(), r.ev.Ref(), "", ""))
	case "propose":
		return r.onPropose()
	case "create":
		return r.onCreate()
	case "edit":
		return r.onEdit()
	case "update":
		return r.onUpdate()
	case "askDelete":
		return r.onAskDelete()
	case "delete":
		return r.onDelete()
	case "sync":
		return r.onSync()
	case "manage":
		return card.Push(r.manageCard(rename{}, ""))
	case "manageChange":
		return card.Update(r.manageCard(renameOf(r.ev), ""))
	case "askRename":
		return r.onAskRename()
	case "rename":
		return r.onRename()
	case "keep":
		return card.Back()
	case "reset":
		return card.Root(r.homepage(form{}, "", ""))
	}
	return card.Notify("Time Tracker does not know that action.")
}

// action calls this service again with do and params.
func (r *request) action(do string, params map[string]string) *card.Action {
	a := &card.Action{Function: r.self, Parameters: []card.Param{{Key: "do", Value: do}}}
	for _, k := range slices.Sorted(maps.Keys(params)) {
		a.Parameters = append(a.Parameters, card.Param{Key: k, Value: params[k]})
	}
	return a
}

func (r *request) button(text, do string, at *ref, style string) card.Button {
	return card.Button{Text: text, Type: style, OnClick: &card.OnClick{Action: r.action(do, at.params())}}
}

// moreSection holds the rarer buttons, folded away.
func (r *request) moreSection() (card.Section, bool) {
	buttons := []card.Button{r.button("Rename…", "manage", nil, "")}
	if r.s.Sheets != nil {
		buttons = append([]card.Button{r.button("Sync to sheet", "sync", nil, "")}, buttons...)
	}
	return card.Section{
		Header: "More", Collapsible: true,
		Widgets: []card.Widget{{ButtonList: &card.ButtonList{Buttons: buttons}}},
	}, true
}

// lists are the names the form offers, from the user's entries; none without a token
// or a calendar. A failure is logged and the form shows empty lists.
func (r *request) lists() entry.Lists {
	if r.store == nil {
		return nil
	}
	id, err := r.store.CalendarID(r.ctx, false)
	if err != nil || id == "" {
		if err != nil {
			r.log.Error("find calendar", "err", err)
		}
		return nil
	}
	entries, err := r.store.Entries(r.ctx, id)
	if err != nil {
		r.log.Error("list entries", "err", err)
		return nil
	}
	return entry.ListsOf(entries)
}

func (r *request) homepage(f form, message, info string) card.Card {
	return r.formCard(f, r.lists(), nil, message, info)
}

// noToken answers an action that needs the user's calendar when Workspace sent no token.
func noToken() card.Response {
	return card.Notify("Open Time Tracker from Google Calendar: this request has no sign-in.")
}
