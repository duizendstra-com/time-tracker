package addon

import (
	"strings"
	"time"

	"github.com/duizendstra-com/time-tracker/card"
	"github.com/duizendstra-com/time-tracker/entry"
)

// refuse shows the form again, with what is missing said on it.
func (r *request) refuse(f form, at *ref, e entry.Entry) card.Response {
	if missing := e.Missing(); len(missing) > 0 {
		msg := "Pick or type a " + strings.Join(missing, " and a ") + ": client, project and task are all required."
		return card.Update(r.formCard(f, r.lists(), at, msg, ""))
	}
	r.log.Warn("no start time", "date", r.ev.Raw("date"), "start", r.ev.Raw("start"))
	return card.Update(r.formCard(f, r.lists(), at, "Pick a date and a start time.", ""))
}

func (r *request) onCreate() card.Response {
	if r.store == nil {
		return noToken()
	}
	f := formOf(r.ev)
	e := f.entryOf()
	if len(e.Missing()) > 0 || e.Start.IsZero() {
		return r.refuse(f, nil, e)
	}
	id, err := r.store.CalendarID(r.ctx, true)
	if err == nil {
		_, err = r.store.Insert(r.ctx, id, e)
	}
	if err != nil {
		r.log.Error("create entry", "err", err)
		return card.Notify("Failed to create event: " + err.Error())
	}
	done := card.Card{
		Header: card.Head("Time logged", e.Title()),
		Sections: []card.Section{{Widgets: []card.Widget{
			card.Row("CLOCK", "In the "+entry.Calendar+" calendar", span(e, "Monday 2 January, 15:04")),
			card.Text("Drag it in Calendar to change the length."),
		}}},
	}
	another := r.button("Log another entry", "reset", nil, "FILLED")
	done.FixedFooter = &card.Footer{PrimaryButton: &another}
	return card.Root(done).With("Entry created in the " + entry.Calendar + " calendar.")
}

// span is an entry's start in layout, then its end time.
func span(e entry.Entry, layout string) string {
	return e.Start.In(entry.Zone).Format(layout) + " – " + e.End.In(entry.Zone).Format(clockLayout)
}

// opened is the entry an action or the event open trigger names; ok is false (and
// res says why) when it cannot be read.
func (r *request) opened(at *ref) (e entry.Entry, res card.Response, ok bool) {
	if r.store == nil {
		return e, noToken(), false
	}
	if at == nil {
		return e, card.Notify("This entry is gone."), false
	}
	e, found, err := r.store.Get(r.ctx, at.CalendarID, at.EventID)
	if err != nil {
		r.log.Error("read entry", "err", err)
		return e, card.Notify("Failed to read the entry: " + err.Error()), false
	}
	if !found {
		return e, card.Notify("This entry is gone."), false
	}
	return e, res, true
}

func (r *request) onEventOpen() card.Response {
	at := &ref{CalendarID: r.ev.Calendar.CalendarID, EventID: r.ev.Calendar.ID}
	var (
		e     entry.Entry
		found bool
		err   error
	)
	if r.store != nil {
		e, found, err = r.store.Get(r.ctx, at.CalendarID, at.EventID)
		if err != nil {
			r.log.Error("read opened event", "err", err)
		}
	}
	if !found {
		return card.Push(card.Card{
			Header:   card.Head("Time Tracker", ""),
			Sections: []card.Section{{Widgets: []card.Widget{card.Row("DESCRIPTION", "", "This event is not a Time Tracker entry.")}}},
		})
	}
	return card.Push(r.entryCard(e, at))
}

// entryCard shows an opened entry, then Update and Delete.
func (r *request) entryCard(e entry.Entry, at *ref) card.Card {
	task := e.Task
	if task == "" {
		task = "Time Tracker entry"
	}
	start := e.Start.In(entry.Zone)
	c := card.Card{
		Header: card.Head(task, e.Client+entry.Sep+e.Project),
		Sections: []card.Section{
			{Header: "When", Widgets: []card.Widget{card.Row("CLOCK", start.Format("Monday 2 January"), span(e, clockLayout))}},
			{Header: "What", Widgets: []card.Widget{
				card.Row("PERSON", "Client", e.Client),
				card.Row("BOOKMARK", "Project", e.Project),
				card.Row("STAR", "Task", e.Task),
				card.Row("DOLLAR", "Billing", billing(e)),
			}},
		},
	}
	if e.Note != "" {
		c.Sections = append(c.Sections, card.Section{Header: "Note", Widgets: []card.Widget{card.Row("DESCRIPTION", "Description", e.Note)}})
	}
	update, del := r.button("Update", "edit", at, "FILLED"), r.button("Delete", "askDelete", at, "")
	c.FixedFooter = &card.Footer{PrimaryButton: &update, SecondaryButton: &del}
	return c
}

func (r *request) onEdit() card.Response {
	at := r.ev.Ref()
	e, res, ok := r.opened(at)
	if !ok {
		return res
	}
	return card.Push(r.formCard(formFrom(e), r.lists(), at, "", ""))
}

func (r *request) onUpdate() card.Response {
	at := r.ev.Ref()
	old, res, ok := r.opened(at)
	if !ok {
		return res
	}
	f := formOf(r.ev)
	f.Prefill = true
	e := f.entryOf()
	if len(e.Missing()) > 0 || e.Start.IsZero() {
		return r.refuse(f, at, e)
	}
	e.ID = old.ID
	if err := r.store.Update(r.ctx, at.CalendarID, e); err != nil {
		r.log.Error("update entry", "err", err)
		return card.Notify("Failed to update: " + err.Error())
	}
	return card.Root(r.entryCard(e, at)).With("Entry updated.")
}

func (r *request) onAskDelete() card.Response {
	at := r.ev.Ref()
	e, res, ok := r.opened(at)
	if !ok {
		return res
	}
	del, keep := r.button("Delete", "delete", at, "FILLED"), r.button("Keep it", "keep", nil, "")
	return card.Push(card.Card{
		Header:      &card.Header{Title: "Delete this entry?", Subtitle: e.Title()},
		Sections:    []card.Section{{Widgets: []card.Widget{card.Row("DESCRIPTION", "", "It is removed from your calendar. This cannot be undone here.")}}},
		FixedFooter: &card.Footer{PrimaryButton: &del, SecondaryButton: &keep},
	})
}

func (r *request) onDelete() card.Response {
	at := r.ev.Ref()
	if _, res, ok := r.opened(at); !ok {
		return res
	}
	if err := r.store.Delete(r.ctx, at.CalendarID, at.EventID); err != nil {
		r.log.Error("delete entry", "err", err)
		return card.Notify("Failed to delete: " + err.Error())
	}
	return card.Root(card.Card{
		Header:   card.Head("Entry deleted", ""),
		Sections: []card.Section{{Widgets: []card.Widget{card.Text("Close the event to see your calendar without it.")}}},
	}).With("Entry deleted.")
}

// onPropose asks Gemini to fill the form from the Describe it note. The user checks
// the form and chooses Create entry; nothing is written here.
func (r *request) onPropose() card.Response {
	f := formOf(r.ev)
	lists := r.lists()
	if r.s.Gemini == nil {
		return card.Update(r.formCard(f, lists, nil, "Gemini is not set up on this service.", ""))
	}
	if f.Describe == "" {
		return card.Update(r.formCard(f, lists, nil, "Type what you did first, then choose Fill in with Gemini.", ""))
	}
	p, err := r.s.Gemini.Propose(r.ctx, f.Describe, lists)
	if err != nil {
		r.log.Error("gemini", "err", err)
		return card.Update(r.formCard(f, lists, nil, "Gemini could not read that note. Fill in the form yourself, or try again.", ""))
	}
	f.Client, f.Project, f.Task = orNew(p.Client), orNew(p.Project), orNew(p.Task)
	if p.Description != "" {
		f.Note = p.Description
	}
	f.Length = halfHours(p.Hours)
	f.Prefill = true
	return card.Update(r.formCard(f, lists, nil, "", "Gemini filled this in. Check it, then choose Create entry."))
}

func billing(e entry.Entry) string {
	if e.Billable {
		return "Billable"
	}
	return "Not billable"
}

// orNew is a proposed name, or the "+ New …" choice when Gemini gave none.
func orNew(name string) string {
	if name == "" {
		return newItem
	}
	return name
}

// halfHours rounds hours to the half hour, between half an hour and the longest length.
func halfHours(h float64) time.Duration {
	d := time.Duration(h * float64(time.Hour)).Round(slot)
	return min(max(d, slot), longest)
}
