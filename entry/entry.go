// Package entry is the entry contract Time Tracker shares with its Apps Script
// version: an event in the calendar named "Time Tracker", tagged with its client,
// project and task, titled "Client · Project · Task". Change it and both sides change.
package entry

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // the zone must load on a base image without zoneinfo
)

const (
	// Calendar is the name of the calendar entries live in.
	Calendar = "Time Tracker"
	// Signature ends every entry's description; a reader strips it.
	Signature = "\n\n— Logged with Time Tracker"
	// Sep joins the three names in a title: a space, a middle dot (U+00B7), a space.
	Sep = " · "

	// The tags, the event's private extended properties, as CalendarEvent.setTag writes them.
	TagClient  = "client"
	TagProject = "project"
	TagTask    = "task"
	// TagBillable is "yes" or "no"; an entry without it is not billable.
	TagBillable = "billable"
)

// Zone is the time zone entries are read and written in, the Apps Script manifest's.
var Zone = mustZone("Europe/Tirane")

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// Entry is one logged block of time.
type Entry struct {
	ID         string
	Client     string
	Project    string
	Task       string
	Note       string
	Billable   bool
	Start, End time.Time
	// UID is the event's iCalUID, what CalendarEvent.getId answers in Apps Script;
	// the sheet's Event id column holds it.
	UID string
}

// Title is the event's title: the three names joined by Sep.
func (e Entry) Title() string { return e.Client + Sep + e.Project + Sep + e.Task }

// Description is the event's description: the note, then the signature.
func (e Entry) Description() string { return e.Note + Signature }

// Tags are the event's private extended properties.
func (e Entry) Tags() map[string]string {
	billable := "no"
	if e.Billable {
		billable = "yes"
	}
	return map[string]string{TagClient: e.Client, TagProject: e.Project, TagTask: e.Task, TagBillable: billable}
}

// Missing names the parts an entry cannot be saved without, in order.
func (e Entry) Missing() []string {
	var out []string
	for _, f := range [][2]string{{"client", e.Client}, {"project", e.Project}, {"task", e.Task}} {
		if strings.TrimSpace(f[1]) == "" {
			out = append(out, f[0])
		}
	}
	return out
}

// Read makes an entry from an event's tags and description. ok is false for an
// event with no client tag, which is not an entry.
func Read(tags map[string]string, description string) (e Entry, ok bool) {
	if tags[TagClient] == "" {
		return Entry{}, false
	}
	return Entry{
		Client:   tags[TagClient],
		Project:  tags[TagProject],
		Task:     tags[TagTask],
		Billable: tags[TagBillable] == "yes",
		Note:     strings.TrimSuffix(description, Signature),
	}, true
}

// Columns are the sheet's header row, as Sync writes it.
var Columns = []string{"Date", "Start", "End", "Hours", "Client", "Project", "Task", "Billable", "Description", "Event id"}

// Row is the entry's line in the sheet, under Columns: text, a number of hours to two
// decimals, and a true or false for Billable.
func (e Entry) Row() []any {
	start, end := e.Start.In(Zone), e.End.In(Zone)
	hours := math.Round(end.Sub(start).Hours()*100) / 100
	return []any{
		start.Format("2006-01-02"), start.Format("15:04"), end.Format("15:04"), hours,
		e.Client, e.Project, e.Task, e.Billable, e.Note, e.UID,
	}
}

// Lists are the names the form offers, derived from the entries' tags.
type Lists []Client

// Client is a client's name and its projects.
type Client struct {
	Name     string
	Projects []Project
}

// Project is a project's name and its tasks.
type Project struct {
	Name  string
	Tasks []string
}

// ListsOf derives the lists from entries, each level sorted by name, ignoring case.
func ListsOf(entries []Entry) Lists {
	tree := map[string]map[string]map[string]bool{}
	for _, e := range entries {
		if e.Client == "" {
			continue
		}
		if tree[e.Client] == nil {
			tree[e.Client] = map[string]map[string]bool{}
		}
		if e.Project == "" {
			continue
		}
		if tree[e.Client][e.Project] == nil {
			tree[e.Client][e.Project] = map[string]bool{}
		}
		if e.Task != "" {
			tree[e.Client][e.Project][e.Task] = true
		}
	}
	var lists Lists
	for _, c := range sortedKeys(tree) {
		client := Client{Name: c}
		for _, p := range sortedKeys(tree[c]) {
			client.Projects = append(client.Projects, Project{Name: p, Tasks: sortedKeys(tree[c][p])})
		}
		lists = append(lists, client)
	}
	return lists
}

// Client finds a client by name.
func (l Lists) Client(name string) (Client, bool) {
	i := slices.IndexFunc(l, func(c Client) bool { return c.Name == name })
	if i < 0 {
		return Client{}, false
	}
	return l[i], true
}

// Project finds a project of the client by name.
func (c Client) Project(name string) (Project, bool) {
	i := slices.IndexFunc(c.Projects, func(p Project) bool { return p.Name == name })
	if i < 0 {
		return Project{}, false
	}
	return c.Projects[i], true
}

// Names are the clients' names, in order.
func (l Lists) Names() []string {
	out := make([]string, len(l))
	for i, c := range l {
		out[i] = c.Name
	}
	return out
}

// Names are the client's projects' names, in order.
func (c Client) Names() []string {
	out := make([]string, len(c.Projects))
	for i, p := range c.Projects {
		out[i] = p.Name
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
	})
	return keys
}
