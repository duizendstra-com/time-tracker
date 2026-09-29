package addon

import (
	"fmt"
	"slices"

	"github.com/duizendstra-com/time-tracker/card"
	"github.com/duizendstra-com/time-tracker/entry"
	"github.com/duizendstra-com/time-tracker/remote"
)

// onSync rebuilds the spreadsheet from every entry, sorted by start. The sheet is a
// copy: Sync overwrites it, and nothing reads it back.
func (r *request) onSync() card.Response {
	if r.store == nil {
		return noToken()
	}
	if r.s.Sheets == nil {
		return card.Notify("Sync is not set up on this service.")
	}
	var entries []entry.Entry
	id, err := r.store.CalendarID(r.ctx, false)
	if err == nil && id != "" {
		entries, err = r.store.Entries(r.ctx, id)
	}
	if err != nil {
		r.log.Error("sync: list entries", "err", err)
		return card.Notify("Sync failed: " + err.Error())
	}
	slices.SortStableFunc(entries, func(a, b entry.Entry) int { return a.Start.Compare(b.Start) })
	rows := make([][]any, len(entries))
	for i, e := range entries {
		rows[i] = e.Row()
	}
	sheet, made, err := r.s.Sheets(r.ev.Auth.UserOAuthToken).Write(r.ctx, entry.Columns, rows)
	if err != nil {
		if remote.IsInsufficientScope(err) {
			return card.Ask(scopeDriveFile)
		}
		r.log.Error("sync: write sheet", "err", err)
		return card.Notify("Sync failed: " + err.Error())
	}

	var widgets []card.Widget
	if made {
		widgets = append(widgets, card.Row("STAR", "", "Sync made a spreadsheet named "+entry.Calendar+" in your Drive."))
	}
	noun := "entries"
	if len(rows) == 1 {
		noun = "entry"
	}
	open := card.Button{Text: "Open sheet", OnClick: &card.OnClick{
		OpenLink: &card.OpenLink{URL: "https://docs.google.com/spreadsheets/d/" + sheet + "/edit"}}}
	widgets = append(widgets,
		card.Text(fmt.Sprintf("%d %s synced.", len(rows), noun)),
		card.Widget{ButtonList: &card.ButtonList{Buttons: []card.Button{open}}},
	)
	return card.Push(card.Card{
		Header:   card.Head("Synced to your sheet", entry.Calendar),
		Sections: []card.Section{{Widgets: widgets}},
	})
}
