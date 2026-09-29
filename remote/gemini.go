package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/duizendstra-com/time-tracker/entry"
)

// Model is the Gemini model the service calls: on the free tier, with no shutdown date.
const Model = "gemini-3.8-flash"

// GeminiEndpoint carries no key: the key travels in the X-Goog-Api-Key header.
var GeminiEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/" + Model + ":generateContent"

// Proposal is what Gemini made of a note, for the user to check before Create.
type Proposal struct {
	Client      string  `json:"client"`
	Project     string  `json:"project"`
	Task        string  `json:"task"`
	Hours       float64 `json:"hours"`
	Description string  `json:"description"`
}

// Gemini turns a typed note into a proposed entry.
type Gemini struct {
	c client
}

// NewGemini is a Gemini client that sends key in a header, never in the URL.
func NewGemini(h *http.Client, key string) *Gemini {
	if h == nil {
		h = &http.Client{Timeout: 30 * time.Second}
	}
	return &Gemini{c: newClient(h, http.Header{"X-Goog-Api-Key": {key}})}
}

const instruction = `You turn a short note about work someone did into one time-tracker entry.
Answer with the client, the project, the task, the hours spent and a one-line description.
Known clients, projects and tasks are listed as "Client · Project · Task", one per line.
When the note means one of them, use its names exactly as listed. Otherwise make up a
short name. Hours: the time the note says, rounded to the half hour; 1 if it says none.
The description restates the note in a few words, in the note's language.`

// Propose asks Gemini for an entry from note, preferring the names in lists.
func (g *Gemini) Propose(ctx context.Context, note string, lists entry.Lists) (Proposal, error) {
	var known []string
	for _, c := range lists {
		for _, p := range c.Projects {
			for _, t := range p.Tasks {
				known = append(known, c.Name+entry.Sep+p.Name+entry.Sep+t)
			}
		}
	}
	str := map[string]string{"type": "STRING"}
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": instruction}}},
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]string{{"text": "Known:\n" + strings.Join(known, "\n") + "\n\nNote:\n" + note}},
		}},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"responseSchema": map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"client": str, "project": str, "task": str, "description": str,
					"hours": map[string]string{"type": "NUMBER"},
				},
				"required": []string{"client", "project", "task", "hours", "description"},
			},
		},
	}
	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := g.c.do(ctx, http.MethodPost, GeminiEndpoint, body, &res); err != nil {
		return Proposal{}, err
	}
	if len(res.Candidates) == 0 || len(res.Candidates[0].Content.Parts) == 0 {
		return Proposal{}, errors.New("gemini: no answer")
	}
	var p Proposal
	if err := json.Unmarshal([]byte(res.Candidates[0].Content.Parts[0].Text), &p); err != nil {
		return Proposal{}, errors.New("gemini: the answer is not the entry asked for")
	}
	p.Client, p.Project, p.Task = strings.TrimSpace(p.Client), strings.TrimSpace(p.Project), strings.TrimSpace(p.Task)
	p.Description = strings.TrimSpace(p.Description)
	return p, nil
}
