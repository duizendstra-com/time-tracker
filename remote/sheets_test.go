package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sheetAPI is a stand-in for Drive and Sheets. With have, Drive lists one spreadsheet.
func sheetAPI(t *testing.T, have bool) (*Sheet, *[]string, *map[string]any) {
	t.Helper()
	var seen []string
	var batch map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("%s %s: Authorization = %q", r.Method, r.URL.Path, got)
		}
		if strings.Contains(r.URL.String(), token) {
			t.Errorf("the token is in the URL: %s", r.URL)
		}
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/drive/files":
			if q := r.URL.Query().Get("q"); q != "name = 'Time Tracker' and mimeType = 'application/vnd.google-apps.spreadsheet' and trashed = false" {
				t.Errorf("q = %s", q)
			}
			if have {
				io.WriteString(w, `{"files":[{"id":"old"}]}`)
			} else {
				io.WriteString(w, `{"files":[]}`)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/sheets/spreadsheets/old":
			io.WriteString(w, `{"sheets":[{"properties":{"sheetId":5}}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/sheets/spreadsheets":
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), `"title":"Entries"`) || !strings.Contains(string(b), `"title":"Time Tracker"`) {
				t.Errorf("create = %s", b)
			}
			io.WriteString(w, `{"spreadsheetId":"new","sheets":[{"properties":{"sheetId":0}}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":batchUpdate"):
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &batch)
			io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	s := NewSheet(srv.Client(), token)
	s.drive, s.sheets = srv.URL+"/drive", srv.URL+"/sheets"
	return s, &seen, &batch
}

func TestSheetWritesTypedCells(t *testing.T) {
	s, seen, batch := sheetAPI(t, true)
	id, made, err := s.Write(context.Background(), []string{"Date", "Hours", "Billable", "Description"},
		[][]any{{"2026-09-28", 1.5, true, "=1+1"}})
	if err != nil || id != "old" || made {
		t.Fatalf("Write = %q, %v, %v", id, made, err)
	}
	if got := strings.Join(*seen, ", "); got != "GET /drive/files, GET /sheets/spreadsheets/old, POST /sheets/spreadsheets/old:batchUpdate" {
		t.Errorf("calls = %s", got)
	}
	b, _ := json.Marshal(*batch)
	for _, want := range []string{
		`{"updateCells":{"fields":"userEnteredValue","range":{"sheetId":5}}}`,
		`{"userEnteredValue":{"stringValue":"Billable"}}`,
		`{"userEnteredValue":{"numberValue":1.5}}`,
		`{"userEnteredValue":{"boolValue":true}}`,
		// A note starting with "=" stays text.
		`{"userEnteredValue":{"stringValue":"=1+1"}}`,
		`"gridProperties":{"frozenRowCount":1}`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("batchUpdate lacks %s:\n%s", want, b)
		}
	}
}

func TestSheetMadeWhenNone(t *testing.T) {
	s, seen, batch := sheetAPI(t, false)
	id, made, err := s.Write(context.Background(), []string{"Date"}, nil)
	if err != nil || id != "new" || !made {
		t.Fatalf("Write = %q, %v, %v", id, made, err)
	}
	if got := strings.Join(*seen, ", "); got != "GET /drive/files, POST /sheets/spreadsheets, POST /sheets/spreadsheets/new:batchUpdate" {
		t.Errorf("calls = %s", got)
	}
	if b, _ := json.Marshal(*batch); !strings.Contains(string(b), `"sheetId":0`) {
		t.Errorf("batchUpdate = %s", b)
	}
}
