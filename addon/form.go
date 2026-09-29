package addon

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/duizendstra-com/time-tracker/card"
	"github.com/duizendstra-com/time-tracker/entry"
)

const (
	newItem     = "__new__"
	slot        = 30 * time.Minute
	defaultLen  = time.Hour
	longest     = 12 * time.Hour
	dateLayout  = "2006-01-02"
	clockLayout = "15:04"
)

// form is the entry form as the card holds it.
type form struct {
	Client, NewClient   string
	Project, NewProject string
	Task, NewTask       string
	Note                string
	Billable            bool
	Date, Start         string // yyyy-MM-dd and HH:mm, in entry.Zone
	Length              time.Duration
	Describe            string // the text Gemini reads
	// Prefill keeps a chosen name on its dropdown when the lists do not hold it: an
	// entry being edited, or a name Gemini proposed.
	Prefill bool
}

// formOf reads the form from an action's event object.
func formOf(ev *Event) form {
	f := form{
		Client: ev.String("client"), NewClient: ev.String("newClient"),
		Project: ev.String("project"), NewProject: ev.String("newProject"),
		Task: ev.String("task"), NewTask: ev.String("newTask"),
		Note: ev.String("description"), Start: ev.String("start"),
		Billable: ev.String("billable") == "yes",
		Describe: ev.String("describe"),
		Prefill:  ev.Param("prefill") == "1",
	}
	// A date picker sends midnight UTC of the chosen day.
	if ms, ok := ev.Date("date"); ok {
		f.Date = time.UnixMilli(ms).UTC().Format(dateLayout)
	}
	if m, err := strconv.Atoi(ev.String("length")); err == nil && m > 0 {
		f.Length = time.Duration(m) * time.Minute
	}
	return f
}

// entryOf is the entry the form describes; its times are zero when the date or
// start is missing.
func (f form) entryOf() entry.Entry {
	name := func(chosen, typed string) string {
		if chosen == newItem {
			return typed
		}
		return chosen
	}
	e := entry.Entry{
		Client: name(f.Client, f.NewClient), Project: name(f.Project, f.NewProject), Task: name(f.Task, f.NewTask),
		Note: f.Note, Billable: f.Billable,
	}
	if start, err := time.ParseInLocation(dateLayout+" "+clockLayout, f.Date+" "+f.Start, entry.Zone); err == nil {
		length := f.Length
		if length <= 0 {
			length = defaultLen
		}
		e.Start, e.End = start, start.Add(length)
	}
	return e
}

// defaultWhen is an hour ago, rounded down to the half hour, in entry.Zone.
func defaultWhen(now time.Time) (date, start string) {
	t := now.Add(-time.Hour).In(entry.Zone)
	t = t.Add(-time.Duration(t.Minute()%30)*time.Minute - time.Duration(t.Second())*time.Second - time.Duration(t.Nanosecond()))
	return t.Format(dateLayout), t.Format(clockLayout)
}

// formFrom fills the form from an entry being edited.
func formFrom(e entry.Entry) form {
	start := e.Start.In(entry.Zone)
	return form{
		Client: e.Client, Project: e.Project, Task: e.Task, Note: e.Note, Billable: e.Billable,
		Date: start.Format(dateLayout), Start: start.Format(clockLayout),
		Length: e.End.Sub(e.Start), Prefill: true,
	}
}

// pick keeps the choice if it is on the list (or is new); otherwise the first name.
func pick(chosen string, names []string) string {
	if chosen == newItem || slices.Contains(names, chosen) {
		return chosen
	}
	if len(names) > 0 {
		return names[0]
	}
	return newItem
}

// startTimes are every half hour, plus the chosen one if it is off the grid.
func startTimes(chosen string) []string {
	var out []string
	for m := 0; m < 24*60; m += 30 {
		out = append(out, fmt.Sprintf("%02d:%02d", m/60, m%60))
	}
	if chosen != "" && !slices.Contains(out, chosen) {
		out = append(out, chosen)
		slices.Sort(out)
	}
	return out
}

// lengths are half hours up to twelve hours, plus the chosen one if it is off the grid.
func lengths(chosen time.Duration) []time.Duration {
	var out []time.Duration
	for d := slot; d <= longest; d += slot {
		out = append(out, d)
	}
	if chosen > 0 && !slices.Contains(out, chosen) {
		out = append(out, chosen)
		slices.Sort(out)
	}
	return out
}

func lengthLabel(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}

// formCard is the entry form: what, when and a note. With a ref it edits that
// event; without one it creates an entry. message, if any, is shown in red above it;
// info in plain text.
func (r *request) formCard(f form, lists entry.Lists, at *ref, message, info string) card.Card {
	withChosen := func(names []string, chosen string) []string {
		if f.Prefill && chosen != "" && chosen != newItem && !slices.Contains(names, chosen) {
			return append(slices.Clone(names), chosen)
		}
		return names
	}
	clientNames := withChosen(lists.Names(), f.Client)
	client := pick(f.Client, clientNames)
	c, _ := lists.Client(client)
	projectNames := withChosen(c.Names(), f.Project)
	project := pick(f.Project, projectNames)
	p, _ := c.Project(project)
	taskNames := withChosen(p.Tasks, f.Task)
	task := pick(f.Task, taskNames)

	if f.Date == "" || f.Start == "" {
		f.Date, f.Start = defaultWhen(r.now())
	}
	if f.Length <= 0 {
		f.Length = defaultLen
	}

	title, subtitle := "Time Tracker", "Log project time"
	if at != nil {
		title, subtitle = "Update entry", "Change what and when"
	}
	out := card.Card{Header: card.Head(title, subtitle)}
	if message != "" {
		out.Sections = append(out.Sections, card.Section{Widgets: []card.Widget{card.Warning(message)}})
	}
	if info != "" {
		out.Sections = append(out.Sections, card.Section{Widgets: []card.Widget{card.Row("STAR", "", info)}})
	}
	if at == nil && r.s.Gemini != nil {
		out.Sections = append(out.Sections, card.Section{
			Header: "Describe it",
			Widgets: []card.Widget{
				{TextInput: &card.TextInput{Name: "describe", Label: "What did you do?", Type: "MULTIPLE_LINE",
					HintText: "Two hours on the website header for Acme", Value: f.Describe}},
				{ButtonList: &card.ButtonList{Buttons: []card.Button{r.button("Fill in with Gemini", "propose", nil, "")}}},
			},
		})
	}

	change := r.action("change", at.params())
	if f.Prefill {
		change.Parameters = append(change.Parameters, card.Param{Key: "prefill", Value: "1"})
	}
	dropdown := func(name, label string, names []string, chosen, newLabel string) card.Widget {
		in := &card.SelectionInput{Name: name, Label: label, Type: "DROPDOWN", OnChangeAction: change}
		for _, n := range names {
			in.Items = append(in.Items, card.Item{Text: n, Value: n, Selected: n == chosen})
		}
		in.Items = append(in.Items, card.Item{Text: newLabel, Value: newItem, Selected: chosen == newItem})
		return card.Widget{SelectionInput: in}
	}
	typed := func(name, label, value string) card.Widget {
		return card.Widget{TextInput: &card.TextInput{Name: name, Label: label, Value: value}}
	}
	what := card.Section{Header: "What"}
	what.Widgets = append(what.Widgets, dropdown("client", "Client", clientNames, client, "+ New client…"))
	if client == newItem {
		what.Widgets = append(what.Widgets, typed("newClient", "New client", f.NewClient))
	}
	what.Widgets = append(what.Widgets, dropdown("project", "Project", projectNames, project, "+ New project…"))
	if project == newItem {
		what.Widgets = append(what.Widgets, typed("newProject", "New project", f.NewProject))
	}
	what.Widgets = append(what.Widgets, dropdown("task", "Task", taskNames, task, "+ New task…"))
	if task == newItem {
		what.Widgets = append(what.Widgets, typed("newTask", "New task", f.NewTask))
	}
	// A checkbox sends its value only when ticked.
	what.Widgets = append(what.Widgets, card.Widget{SelectionInput: &card.SelectionInput{
		Name: "billable", Label: "Billing", Type: "CHECK_BOX",
		Items: []card.Item{{Text: "Billable", Value: "yes", Selected: f.Billable}},
	}})
	out.Sections = append(out.Sections, what)

	day, _ := time.Parse(dateLayout, f.Date)
	start := &card.SelectionInput{Name: "start", Label: "Start", Type: "DROPDOWN"}
	for _, t := range startTimes(f.Start) {
		start.Items = append(start.Items, card.Item{Text: t, Value: t, Selected: t == f.Start})
	}
	length := &card.SelectionInput{Name: "length", Label: "Length", Type: "DROPDOWN"}
	for _, d := range lengths(f.Length) {
		m := strconv.Itoa(int(d / time.Minute))
		length.Items = append(length.Items, card.Item{Text: lengthLabel(d), Value: m, Selected: d == f.Length})
	}
	out.Sections = append(out.Sections, card.Section{
		Header: "When",
		Widgets: []card.Widget{
			{DateTimePicker: &card.DateTimePicker{Name: "date", Label: "Date", Type: "DATE_ONLY", ValueMsEpoch: day.UnixMilli()}},
			{SelectionInput: start},
			{SelectionInput: length},
		},
	})
	out.Sections = append(out.Sections, card.Section{
		Header: "Note",
		Widgets: []card.Widget{{TextInput: &card.TextInput{Name: "description", Label: "Description",
			Type: "MULTIPLE_LINE", HintText: "What did you do?", Value: f.Note}}},
	})
	if more, ok := r.moreSection(); ok && at == nil {
		out.Sections = append(out.Sections, more)
	}

	footer := &card.Footer{}
	if at == nil {
		b := r.button("Create entry", "create", nil, "FILLED")
		footer.PrimaryButton = &b
	} else {
		b, cancel := r.button("Save changes", "update", at, "FILLED"), r.button("Cancel", "keep", nil, "")
		footer.PrimaryButton, footer.SecondaryButton = &b, &cancel
	}
	out.FixedFooter = footer
	return out
}
