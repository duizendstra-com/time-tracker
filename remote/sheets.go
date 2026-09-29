package remote

import (
	"context"
	"net/http"
	"net/url"

	"github.com/duizendstra-com/time-tracker/entry"
)

// DriveBase and SheetsBase are the APIs' addresses; tests point them at a stand-in.
var (
	DriveBase  = "https://www.googleapis.com/drive/v3"
	SheetsBase = "https://sheets.googleapis.com/v4"
)

const spreadsheet = "application/vnd.google-apps.spreadsheet"

// Sheet is the spreadsheet Sync fills, reached with the user's token under drive.file:
// Drive shows this service only the files it made, so the one named entry.Calendar is
// its own.
type Sheet struct {
	c             client
	drive, sheets string
}

// NewSheet is a Sheet acting as the user whose token this is.
func NewSheet(h *http.Client, token string) *Sheet {
	return &Sheet{c: newClient(h, bearer(token)), drive: DriveBase, sheets: SheetsBase}
}

// Write clears the first sheet of the spreadsheet named entry.Calendar and writes a
// frozen header row, then rows. It makes the spreadsheet, with a first sheet named
// Entries, when there is none; made says so. Every cell is a value, never parsed.
func (s *Sheet) Write(ctx context.Context, header []string, rows [][]any) (id string, made bool, err error) {
	id, sheetID, err := s.find(ctx)
	if err != nil {
		return "", false, err
	}
	if id == "" {
		if id, sheetID, err = s.create(ctx); err != nil {
			return "", false, err
		}
		made = true
	}
	values := []map[string]any{row(anys(header))}
	for _, r := range rows {
		values = append(values, row(r))
	}
	body := map[string]any{"requests": []any{
		map[string]any{"updateCells": map[string]any{
			"range": map[string]any{"sheetId": sheetID}, "fields": "userEnteredValue"}},
		map[string]any{"appendCells": map[string]any{
			"sheetId": sheetID, "rows": values, "fields": "userEnteredValue"}},
		map[string]any{"updateSheetProperties": map[string]any{
			"properties": map[string]any{"sheetId": sheetID, "gridProperties": map[string]any{"frozenRowCount": 1}},
			"fields":     "gridProperties.frozenRowCount"}},
	}}
	err = s.c.do(ctx, http.MethodPost, s.sheets+"/spreadsheets/"+url.PathEscape(id)+":batchUpdate", body, nil)
	return id, made, err
}

// find is the oldest spreadsheet named entry.Calendar that this service can see and
// open, and its first sheet; "" when there is none.
func (s *Sheet) find(ctx context.Context) (id string, sheetID int64, err error) {
	q := url.Values{
		"q":        {"name = '" + entry.Calendar + "' and mimeType = '" + spreadsheet + "' and trashed = false"},
		"orderBy":  {"createdTime"},
		"fields":   {"files(id)"},
		"pageSize": {"1"},
		"spaces":   {"drive"},
	}
	var list struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	if err := s.c.do(ctx, http.MethodGet, s.drive+"/files?"+q.Encode(), nil, &list); err != nil {
		return "", 0, err
	}
	if len(list.Files) == 0 {
		return "", 0, nil
	}
	id = list.Files[0].ID
	var got struct {
		Sheets []struct {
			Properties struct {
				SheetID int64 `json:"sheetId"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	q = url.Values{"fields": {"sheets.properties.sheetId"}}
	if err := s.c.do(ctx, http.MethodGet, s.sheets+"/spreadsheets/"+url.PathEscape(id)+"?"+q.Encode(), nil, &got); err != nil {
		if IsNotFound(err) {
			return "", 0, nil
		}
		return "", 0, err
	}
	if len(got.Sheets) == 0 {
		return "", 0, nil
	}
	return id, got.Sheets[0].Properties.SheetID, nil
}

func (s *Sheet) create(ctx context.Context) (id string, sheetID int64, err error) {
	body := map[string]any{
		"properties": map[string]any{"title": entry.Calendar},
		"sheets":     []any{map[string]any{"properties": map[string]any{"title": "Entries"}}},
	}
	var made struct {
		SpreadsheetID string `json:"spreadsheetId"`
		Sheets        []struct {
			Properties struct {
				SheetID int64 `json:"sheetId"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	if err := s.c.do(ctx, http.MethodPost, s.sheets+"/spreadsheets", body, &made); err != nil {
		return "", 0, err
	}
	if len(made.Sheets) > 0 {
		sheetID = made.Sheets[0].Properties.SheetID
	}
	return made.SpreadsheetID, sheetID, nil
}

// row is one row of cells, each a typed value: text is never parsed, so a note
// starting with "=" stays text.
func row(values []any) map[string]any {
	cells := make([]any, len(values))
	for i, v := range values {
		var value map[string]any
		switch v := v.(type) {
		case float64:
			value = map[string]any{"numberValue": v}
		case bool:
			value = map[string]any{"boolValue": v}
		case string:
			value = map[string]any{"stringValue": v}
		default:
			value = map[string]any{"stringValue": ""}
		}
		cells[i] = map[string]any{"userEnteredValue": value}
	}
	return map[string]any{"values": cells}
}

func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
