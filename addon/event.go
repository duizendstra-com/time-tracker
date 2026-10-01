// Package addon is Time Tracker as a Google Workspace add-on over HTTP: Workspace
// POSTs an event object, and the service answers with the card to draw.
package addon

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Event is the part of the event object the add-on reads
// (https://developers.google.com/workspace/add-ons/concepts/event-objects).
type Event struct {
	Common struct {
		HostApp    string            `json:"hostApp"`
		Parameters map[string]string `json:"parameters"`
		FormInputs map[string]Input  `json:"formInputs"`
	} `json:"commonEventObject"`
	Auth struct {
		UserOAuthToken string `json:"userOAuthToken"`
		// AuthorizedScopes are the scopes the user granted; nil when Workspace did not say.
		AuthorizedScopes []string `json:"authorizedScopes"`
	} `json:"authorizationEventObject"`
	Calendar struct {
		ID         string `json:"id"`
		CalendarID string `json:"calendarId"`
	} `json:"calendar"`
}

// Input is one form field's value.
type Input struct {
	StringInputs *struct {
		Value []string `json:"value"`
	} `json:"stringInputs"`
	DateInput     *msInput `json:"dateInput"`
	DateTimeInput *msInput `json:"dateTimeInput"`
	raw           string   // the field as Workspace sent it, for the log
}

type msInput struct {
	MsSinceEpoch json.RawMessage `json:"msSinceEpoch"`
}

func (in *Input) UnmarshalJSON(b []byte) error {
	type plain Input
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*in = Input(p)
	in.raw = string(b)
	return nil
}

// String is a text or dropdown field's value, trimmed; "" when absent.
func (e *Event) String(name string) string {
	in, ok := e.Common.FormInputs[name]
	if !ok || in.StringInputs == nil || len(in.StringInputs.Value) == 0 {
		return ""
	}
	return strings.TrimSpace(in.StringInputs.Value[0])
}

// Date is a date picker's value in milliseconds; ok is false when absent. Workspace
// documents dateInput for a date-only picker, but dateTimeInput is read too, and
// msSinceEpoch may come as a string, an integer or a number in exponent form.
func (e *Event) Date(name string) (ms int64, ok bool) {
	in, found := e.Common.FormInputs[name]
	if !found {
		return 0, false
	}
	d := in.DateInput
	if d == nil {
		d = in.DateTimeInput
	}
	if d == nil {
		return 0, false
	}
	v := strings.Trim(string(d.MsSinceEpoch), `"`)
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n, true
	}
	f, err := strconv.ParseFloat(v, 64)
	return int64(f), err == nil
}

// Raw is a form field as Workspace sent it, or "absent".
func (e *Event) Raw(name string) string {
	in, ok := e.Common.FormInputs[name]
	if !ok {
		return "absent"
	}
	return in.raw
}

// Param is an action parameter; "" when absent.
func (e *Event) Param(name string) string { return e.Common.Parameters[name] }

// ref is the event an update or delete acts on.
type ref struct{ CalendarID, EventID string }

func (r *ref) params() map[string]string {
	if r == nil {
		return nil
	}
	return map[string]string{"calendarId": r.CalendarID, "eventId": r.EventID}
}

// Ref is the event an action carries, or nil.
func (e *Event) Ref() *ref {
	if e.Param("eventId") == "" {
		return nil
	}
	return &ref{CalendarID: e.Param("calendarId"), EventID: e.Param("eventId")}
}
