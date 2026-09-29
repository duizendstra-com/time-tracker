package remote

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/duizendstra-com/time-tracker/entry"
)

// CalendarBase is the Calendar API's address; tests point it at a stand-in.
var CalendarBase = "https://www.googleapis.com/calendar/v3"

// cyan is CalendarApp.EventColor.CYAN, the colour the Apps Script version gives entries.
const cyan = "7"

// Calendar reads and writes entries with the user's token.
type Calendar struct {
	c    client
	base string
}

// NewCalendar is a Calendar acting as the user whose token this is.
func NewCalendar(h *http.Client, token string) *Calendar {
	return &Calendar{c: newClient(h, bearer(token)), base: CalendarBase}
}

type event struct {
	ID                 string        `json:"id,omitempty"`
	ICalUID            string        `json:"iCalUID,omitempty"`
	Status             string        `json:"status,omitempty"`
	Summary            string        `json:"summary,omitempty"`
	Description        string        `json:"description,omitempty"`
	ColorID            string        `json:"colorId,omitempty"`
	Start              *when         `json:"start,omitempty"`
	End                *when         `json:"end,omitempty"`
	ExtendedProperties *extendedProp `json:"extendedProperties,omitempty"`
}

type when struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type extendedProp struct {
	Private map[string]string `json:"private,omitempty"`
}

func toEvent(e entry.Entry) event {
	return event{
		Summary:            e.Title(),
		Description:        e.Description(),
		Start:              &when{DateTime: e.Start.In(entry.Zone).Format(time.RFC3339), TimeZone: entry.Zone.String()},
		End:                &when{DateTime: e.End.In(entry.Zone).Format(time.RFC3339), TimeZone: entry.Zone.String()},
		ExtendedProperties: &extendedProp{Private: e.Tags()},
	}
}

// fromEvent reads an entry back; ok is false for an event that is not one.
func fromEvent(ev event) (entry.Entry, bool) {
	if ev.Status == "cancelled" {
		return entry.Entry{}, false
	}
	var tags map[string]string
	if ev.ExtendedProperties != nil {
		tags = ev.ExtendedProperties.Private
	}
	e, ok := entry.Read(tags, ev.Description)
	if !ok {
		return entry.Entry{}, false
	}
	e.ID, e.UID = ev.ID, ev.ICalUID
	e.Start, e.End = parseWhen(ev.Start), parseWhen(ev.End)
	return e, true
}

func parseWhen(w *when) time.Time {
	if w == nil {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, w.DateTime); err == nil {
		return t
	}
	t, _ := time.ParseInLocation("2006-01-02", w.Date, entry.Zone)
	return t
}

// CalendarID finds the user's calendar named entry.Calendar. With create, it makes
// one when there is none; without, it answers "" and no error.
func (c *Calendar) CalendarID(ctx context.Context, create bool) (string, error) {
	page := ""
	for {
		q := url.Values{"minAccessRole": {"owner"}, "maxResults": {"250"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var list struct {
			Items []struct {
				ID      string `json:"id"`
				Summary string `json:"summary"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.c.do(ctx, http.MethodGet, c.base+"/users/me/calendarList?"+q.Encode(), nil, &list); err != nil {
			return "", err
		}
		for _, cal := range list.Items {
			if cal.Summary == entry.Calendar {
				return cal.ID, nil
			}
		}
		if page = list.NextPageToken; page == "" {
			break
		}
	}
	if !create {
		return "", nil
	}
	var made struct {
		ID string `json:"id"`
	}
	body := map[string]string{
		"summary":     entry.Calendar,
		"description": "Dedicated calendar for Time Tracker entries",
		"timeZone":    entry.Zone.String(),
	}
	if err := c.c.do(ctx, http.MethodPost, c.base+"/calendars", body, &made); err != nil {
		return "", err
	}
	return made.ID, nil
}

// Entries are every entry in the calendar, in no particular order.
func (c *Calendar) Entries(ctx context.Context, calendarID string) ([]entry.Entry, error) {
	var out []entry.Entry
	page := ""
	for {
		q := url.Values{"singleEvents": {"true"}, "maxResults": {"2500"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var list struct {
			Items         []event `json:"items"`
			NextPageToken string  `json:"nextPageToken"`
		}
		if err := c.c.do(ctx, http.MethodGet, c.events(calendarID)+"?"+q.Encode(), nil, &list); err != nil {
			return nil, err
		}
		for _, ev := range list.Items {
			if e, ok := fromEvent(ev); ok {
				out = append(out, e)
			}
		}
		if page = list.NextPageToken; page == "" {
			return out, nil
		}
	}
}

// Get is one event as an entry. ok is false when the event is gone or is not an entry.
func (c *Calendar) Get(ctx context.Context, calendarID, eventID string) (e entry.Entry, ok bool, err error) {
	var ev event
	if err := c.c.do(ctx, http.MethodGet, c.event(calendarID, eventID), nil, &ev); err != nil {
		if IsNotFound(err) {
			return entry.Entry{}, false, nil
		}
		return entry.Entry{}, false, err
	}
	e, ok = fromEvent(ev)
	return e, ok, nil
}

// Insert creates the entry's event and answers the entry with its id.
func (c *Calendar) Insert(ctx context.Context, calendarID string, e entry.Entry) (entry.Entry, error) {
	ev := toEvent(e)
	ev.ColorID = cyan
	var made event
	if err := c.c.do(ctx, http.MethodPost, c.events(calendarID), ev, &made); err != nil {
		return entry.Entry{}, err
	}
	e.ID = made.ID
	return e, nil
}

// Update writes the entry over its event: title, description, tags and time.
func (c *Calendar) Update(ctx context.Context, calendarID string, e entry.Entry) error {
	return c.c.do(ctx, http.MethodPatch, c.event(calendarID, e.ID), toEvent(e), nil)
}

// Delete removes the entry's event.
func (c *Calendar) Delete(ctx context.Context, calendarID, eventID string) error {
	return c.c.do(ctx, http.MethodDelete, c.event(calendarID, eventID), nil, nil)
}

func (c *Calendar) events(calendarID string) string {
	return c.base + "/calendars/" + url.PathEscape(calendarID) + "/events"
}

func (c *Calendar) event(calendarID, eventID string) string {
	return c.events(calendarID) + "/" + url.PathEscape(eventID)
}
