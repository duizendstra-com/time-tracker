package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/duizendstra-com/time-tracker/entry"
)

const token, key = "ya29.secret-token", "AIza-secret-key"

// calendarAPI is a stand-in for the Calendar API that records each request.
func calendarAPI(t *testing.T, h http.HandlerFunc) (*Calendar, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("%s %s: Authorization = %q", r.Method, r.URL.Path, got)
		}
		if strings.Contains(r.URL.String(), token) {
			t.Errorf("the token is in the URL: %s", r.URL)
		}
		seen = append(seen, r.Method+" "+r.URL.Path)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	c := NewCalendar(srv.Client(), token)
	c.base = srv.URL
	return c, &seen
}

func TestInsertWritesTheContract(t *testing.T) {
	var got map[string]any
	c, _ := calendarAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/calendars/cal@group/events" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		io.WriteString(w, `{"id":"ev9"}`)
	})
	start := time.Date(2026, 9, 28, 9, 0, 0, 0, entry.Zone)
	e, err := c.Insert(context.Background(), "cal@group", entry.Entry{Client: "Acme", Project: "Site", Task: "Design", Note: "Header", Billable: true, Start: start, End: start.Add(time.Hour)})
	if err != nil || e.ID != "ev9" {
		t.Fatalf("Insert = %+v, %v", e, err)
	}
	want := `{"colorId":"7","description":"Header\n\n— Logged with Time Tracker","end":{"dateTime":"2026-09-28T10:00:00+02:00","timeZone":"Europe/Tirane"},"extendedProperties":{"private":{"billable":"yes","client":"Acme","project":"Site","task":"Design"}},"start":{"dateTime":"2026-09-28T09:00:00+02:00","timeZone":"Europe/Tirane"},"summary":"Acme · Site · Design"}`
	if b, _ := json.Marshal(got); string(b) != want {
		t.Errorf("event =\n%s\nwant\n%s", b, want)
	}
}

func TestEntriesReadsTaggedEventsOnly(t *testing.T) {
	c, seen := calendarAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "" {
			io.WriteString(w, `{"items":[
				{"id":"a","iCalUID":"a@google.com","summary":"Acme · Site · Design","description":"Note\n\n— Logged with Time Tracker",
				 "start":{"dateTime":"2026-09-28T09:00:00+02:00"},"end":{"dateTime":"2026-09-28T10:00:00+02:00"},
				 "extendedProperties":{"private":{"client":"Acme","project":"Site","task":"Design"}}},
				{"id":"b","summary":"Lunch","start":{"dateTime":"2026-09-28T12:00:00+02:00"}}],
				"nextPageToken":"p2"}`)
			return
		}
		io.WriteString(w, `{"items":[{"id":"c","status":"cancelled","extendedProperties":{"private":{"client":"X"}}}]}`)
	})
	es, err := c.Entries(context.Background(), "cal")
	if err != nil || len(es) != 1 || es[0].ID != "a" || es[0].Note != "Note" || es[0].Start.UTC().Hour() != 7 || es[0].UID != "a@google.com" || es[0].Billable {
		t.Fatalf("Entries = %+v, %v", es, err)
	}
	if len(*seen) != 2 {
		t.Errorf("pages read: %v", *seen)
	}
}

func TestCalendarIDFindsOrMakes(t *testing.T) {
	c, seen := calendarAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /users/me/calendarList":
			io.WriteString(w, `{"items":[{"id":"primary","summary":"me"}]}`)
		case "POST /calendars":
			var b map[string]string
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["summary"] != "Time Tracker" || b["timeZone"] != "Europe/Tirane" {
				t.Errorf("made %v", b)
			}
			io.WriteString(w, `{"id":"new@group"}`)
		}
	})
	if id, err := c.CalendarID(context.Background(), false); id != "" || err != nil {
		t.Errorf("without create = %q, %v", id, err)
	}
	if id, err := c.CalendarID(context.Background(), true); id != "new@group" || err != nil {
		t.Errorf("with create = %q, %v", id, err)
	}
	if strings.Join(*seen, ",") != "GET /users/me/calendarList,GET /users/me/calendarList,POST /calendars" {
		t.Errorf("calls %v", *seen)
	}
}

func TestGetGoneAndErrors(t *testing.T) {
	c, _ := calendarAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/gone") {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"message":"You do not have permission","errors":[{"reason":"insufficientPermissions"}]}}`)
	})
	if _, ok, err := c.Get(context.Background(), "cal", "gone"); ok || err != nil {
		t.Errorf("gone = %v, %v", ok, err)
	}
	_, _, err := c.Get(context.Background(), "cal", "x")
	if err == nil || err.Error() != "You do not have permission" || !IsInsufficientScope(err) {
		t.Errorf("err = %v", err)
	}
}

func TestTransportErrorCarriesNoURL(t *testing.T) {
	c := NewCalendar(nil, token)
	c.base = "http://127.0.0.1:1/secret-path"
	_, err := c.Entries(context.Background(), "cal")
	if err == nil || strings.Contains(err.Error(), "secret-path") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("err = %v", err)
	}
}

func TestGeminiKeyInHeaderOnly(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Goog-Api-Key") != key {
			t.Errorf("X-Goog-Api-Key = %q", r.Header.Get("X-Goog-Api-Key"))
		}
		if strings.Contains(r.URL.String(), key) || r.URL.Query().Has("key") {
			t.Errorf("the key is in the URL: %s", r.URL)
		}
		if !strings.HasSuffix(r.URL.Path, "/models/gemini-3.8-flash:generateContent") {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"{\"client\":\" Acme \",\"project\":\"Site\",\"task\":\"Review\",\"hours\":2,\"description\":\"Reviewed\"}"}]}}]}`)
	}))
	defer srv.Close()
	old := GeminiEndpoint
	GeminiEndpoint = srv.URL + "/v1beta/models/" + Model + ":generateContent"
	defer func() { GeminiEndpoint = old }()

	lists := entry.ListsOf([]entry.Entry{{Client: "Acme", Project: "Site", Task: "Design"}})
	p, err := NewGemini(srv.Client(), key).Propose(context.Background(), "two hours", lists)
	if err != nil || p.Client != "Acme" || p.Hours != 2 || p.Task != "Review" {
		t.Fatalf("Propose = %+v, %v", p, err)
	}
	b, _ := json.Marshal(body)
	if !strings.Contains(string(b), `Known:\nAcme · Site · Design\n\nNote:\ntwo hours`) || !strings.Contains(string(b), `"responseMimeType":"application/json"`) {
		t.Errorf("body = %s", b)
	}
}

func TestGeminiErrorCarriesNoKeyOrURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"API key not valid","status":"INVALID_ARGUMENT"}}`)
	}))
	defer srv.Close()
	old := GeminiEndpoint
	GeminiEndpoint = srv.URL + "/models/x"
	defer func() { GeminiEndpoint = old }()
	_, err := NewGemini(srv.Client(), key).Propose(context.Background(), "n", nil)
	if err == nil || strings.Contains(err.Error(), key) || strings.Contains(err.Error(), srv.URL) {
		t.Errorf("err = %v", err)
	}
	GeminiEndpoint = "http://127.0.0.1:1/models/x"
	_, err = NewGemini(nil, key).Propose(context.Background(), "n", nil)
	if err == nil || strings.Contains(err.Error(), "127.0.0.1") || strings.Contains(err.Error(), key) {
		t.Errorf("transport err = %v", err)
	}
}
