package addon

import (
	"fmt"
	"slices"
	"sync"

	"github.com/duizendstra-com/time-tracker/card"
	"github.com/duizendstra-com/time-tracker/entry"
)

// rename is a name to change: a client, or a project of it, or a task of that, and
// the name it becomes. The lists come from the entries, so renaming rewrites every
// entry that uses the name, its tags and its title.
type rename struct {
	Client, Project, Task string
	To                    string
}

func renameOf(ev *Event) rename {
	return rename{Client: ev.String("renClient"), Project: ev.String("renProject"), Task: ev.String("renTask"), To: ev.String("renTo")}
}

// renameParams reads a rename from the confirm button's parameters.
func renameParams(ev *Event) rename {
	return rename{Client: ev.Param("client"), Project: ev.Param("project"), Task: ev.Param("task"), To: ev.Param("to")}
}

func (n rename) params() map[string]string {
	return map[string]string{"client": n.Client, "project": n.Project, "task": n.Task, "to": n.To}
}

// level is what the rename changes, and its name now.
func (n rename) level() (level, old string) {
	switch {
	case n.Task != "":
		return "task", n.Task
	case n.Project != "":
		return "project", n.Project
	}
	return "client", n.Client
}

func (n rename) matches(e entry.Entry) bool {
	return e.Client == n.Client && (n.Project == "" || e.Project == n.Project) && (n.Task == "" || e.Task == n.Task)
}

func (n rename) apply(e entry.Entry) entry.Entry {
	switch level, _ := n.level(); level {
	case "task":
		e.Task = n.To
	case "project":
		e.Project = n.To
	default:
		e.Client = n.To
	}
	return e
}

// taken says whether the new name is already in use beside the old one: the two
// become one.
func (n rename) taken(lists entry.Lists) bool {
	c, _ := lists.Client(n.Client)
	p, _ := c.Project(n.Project)
	switch level, _ := n.level(); level {
	case "task":
		return slices.Contains(p.Tasks, n.To)
	case "project":
		return slices.Contains(c.Names(), n.To)
	}
	return slices.Contains(lists.Names(), n.To)
}

// using are the entries the rename changes, and the calendar they are in.
func (r *request) using(n rename) (calendarID string, out []entry.Entry, err error) {
	calendarID, err = r.store.CalendarID(r.ctx, false)
	if err != nil || calendarID == "" {
		return calendarID, nil, err
	}
	all, err := r.store.Entries(r.ctx, calendarID)
	if err != nil {
		return calendarID, nil, err
	}
	for _, e := range all {
		if n.matches(e) {
			out = append(out, e)
		}
	}
	return calendarID, out, nil
}

// manageCard picks a name to rename. A project or task left on "All of …" renames the
// level above it.
func (r *request) manageCard(n rename, message string) card.Card {
	out := card.Card{Header: card.Head("Rename", "A client, project or task")}
	back := r.button("Back", "keep", nil, "")
	lists := r.lists()
	if len(lists) == 0 {
		out.Sections = []card.Section{{Widgets: []card.Widget{card.Text("Nothing to rename yet. Log an entry first; its client, project and task appear here.")}}}
		out.FixedFooter = &card.Footer{PrimaryButton: &back}
		return out
	}
	if _, ok := lists.Client(n.Client); !ok {
		n.Client = lists[0].Name
	}
	c, _ := lists.Client(n.Client)
	if _, ok := c.Project(n.Project); !ok {
		n.Project = ""
	}
	p, _ := c.Project(n.Project)
	if n.Project == "" || !slices.Contains(p.Tasks, n.Task) {
		n.Task = ""
	}

	change := r.action("manageChange", nil)
	dropdown := func(name, label string, all string, names []string, chosen string) card.Widget {
		in := &card.SelectionInput{Name: name, Label: label, Type: "DROPDOWN", OnChangeAction: change}
		if all != "" {
			in.Items = append(in.Items, card.Item{Text: all, Value: "", Selected: chosen == ""})
		}
		for _, s := range names {
			in.Items = append(in.Items, card.Item{Text: s, Value: s, Selected: s == chosen})
		}
		return card.Widget{SelectionInput: in}
	}
	var widgets []card.Widget
	if message != "" {
		widgets = append(widgets, card.Warning(message))
	}
	widgets = append(widgets,
		card.Text("Every entry that uses the name changes: its tags and its title."),
		dropdown("renClient", "Client", "", lists.Names(), n.Client),
		dropdown("renProject", "Project", "All of this client", c.Names(), n.Project),
	)
	if n.Project != "" {
		widgets = append(widgets, dropdown("renTask", "Task", "All of this project", p.Tasks, n.Task))
	}
	level, _ := n.level()
	widgets = append(widgets, card.Widget{TextInput: &card.TextInput{Name: "renTo", Label: "New " + level + " name", Value: n.To}})
	out.Sections = []card.Section{{Widgets: widgets}}
	next := r.button("Rename…", "askRename", nil, "FILLED")
	out.FixedFooter = &card.Footer{PrimaryButton: &next, SecondaryButton: &back}
	return out
}

func (r *request) onAskRename() card.Response {
	if r.store == nil {
		return noToken()
	}
	n := renameOf(r.ev)
	level, old := n.level()
	switch {
	case n.To == "":
		return card.Update(r.manageCard(n, "Type the new name."))
	case n.To == old:
		return card.Update(r.manageCard(n, "That is its name already."))
	}
	_, using, err := r.using(n)
	if err != nil {
		r.log.Error("rename: list entries", "err", err)
		return card.Notify("Failed to read the entries: " + err.Error())
	}
	if len(using) == 0 {
		return card.Update(r.manageCard(n, "No entry uses that name any more."))
	}
	widgets := []card.Widget{card.Row("DESCRIPTION", "", fmt.Sprintf("%s change: the %s in their tags and their titles.", count(len(using)), level))}
	if n.taken(r.lists()) {
		widgets = append(widgets, card.Row("STAR", "", "There is already a "+level+" named "+n.To+": the two become one."))
	}
	do, keep := r.action("rename", n.params()), r.button("Keep it", "keep", nil, "")
	return card.Push(card.Card{
		Header:      &card.Header{Title: "Rename this " + level + "?", Subtitle: old + " → " + n.To},
		Sections:    []card.Section{{Widgets: widgets}},
		FixedFooter: &card.Footer{PrimaryButton: &card.Button{Text: "Rename", Type: "FILLED", OnClick: &card.OnClick{Action: do}}, SecondaryButton: &keep},
	})
}

// onRename rewrites every entry that uses the name, a few at a time. What fails is
// said; choosing Rename again finishes it, since the renamed entries no longer match.
func (r *request) onRename() card.Response {
	if r.store == nil {
		return noToken()
	}
	n := renameParams(r.ev)
	if n.To == "" || n.Client == "" {
		return card.Notify("Choose Rename… again: this rename is incomplete.")
	}
	calendarID, using, err := r.using(n)
	if err != nil {
		r.log.Error("rename: list entries", "err", err)
		return card.Notify("Failed to read the entries: " + err.Error())
	}
	if len(using) == 0 {
		return card.Root(r.homepage(form{}, "", "")).With("Nothing to rename: no entry uses that name any more.")
	}
	var (
		mu     sync.Mutex
		failed int
		first  error
		wg     sync.WaitGroup
		slots  = make(chan struct{}, 8)
	)
	for _, e := range using {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			if err := r.store.Update(r.ctx, calendarID, n.apply(e)); err != nil {
				mu.Lock()
				failed++
				if first == nil {
					first = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if failed > 0 {
		r.log.Error("rename: update entries", "failed", failed, "of", len(using), "err", first)
		return card.Notify(fmt.Sprintf("Renamed %d of %d. The rest failed: %v Choose Rename again to finish.", len(using)-failed, len(using), first))
	}
	return card.Root(r.homepage(form{}, "", "")).With(fmt.Sprintf("Renamed in %s.", count(len(using))))
}

// count is n entries, in words.
func count(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}
