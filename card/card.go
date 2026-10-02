// Package card is the JSON a Google Workspace add-on over HTTP answers with: a
// card (google.apps.card.v1) in a RenderActions at the top level, as
// https://developers.google.com/workspace/add-ons/guides/alternate-runtimes shows.
package card

import "strings"

// Logo is the header image, the same as the Apps Script version's.
const Logo = "https://www.gstatic.com/images/icons/material/system/1x/schedule_black_24dp.png"

type Card struct {
	Header      *Header   `json:"header,omitempty"`
	Sections    []Section `json:"sections,omitempty"`
	FixedFooter *Footer   `json:"fixedFooter,omitempty"`
}

type Header struct {
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	ImageURL  string `json:"imageUrl,omitempty"`
	ImageType string `json:"imageType,omitempty"`
}

type Section struct {
	Header                    string   `json:"header,omitempty"`
	Collapsible               bool     `json:"collapsible,omitempty"`
	UncollapsibleWidgetsCount int      `json:"uncollapsibleWidgetsCount,omitempty"`
	Widgets                   []Widget `json:"widgets"`
}

type Widget struct {
	TextParagraph  *TextParagraph  `json:"textParagraph,omitempty"`
	DecoratedText  *DecoratedText  `json:"decoratedText,omitempty"`
	TextInput      *TextInput      `json:"textInput,omitempty"`
	SelectionInput *SelectionInput `json:"selectionInput,omitempty"`
	DateTimePicker *DateTimePicker `json:"dateTimePicker,omitempty"`
	ButtonList     *ButtonList     `json:"buttonList,omitempty"`
}

type TextParagraph struct {
	Text string `json:"text"`
}

type DecoratedText struct {
	TopLabel  string `json:"topLabel,omitempty"`
	Text      string `json:"text"`
	WrapText  bool   `json:"wrapText,omitempty"`
	StartIcon *Icon  `json:"startIcon,omitempty"`
}

type Icon struct {
	KnownIcon string `json:"knownIcon"`
}

type TextInput struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Value    string `json:"value,omitempty"`
	Type     string `json:"type,omitempty"`
	HintText string `json:"hintText,omitempty"`
}

type SelectionInput struct {
	Name           string  `json:"name"`
	Label          string  `json:"label"`
	Type           string  `json:"type"`
	Items          []Item  `json:"items"`
	OnChangeAction *Action `json:"onChangeAction,omitempty"`
}

type Item struct {
	Text     string `json:"text"`
	Value    string `json:"value"`
	Selected bool   `json:"selected,omitempty"`
}

type DateTimePicker struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	Type         string `json:"type"`
	ValueMsEpoch int64  `json:"valueMsEpoch,string,omitempty"`
}

type ButtonList struct {
	Buttons []Button `json:"buttons"`
}

type Button struct {
	Text    string   `json:"text"`
	Type    string   `json:"type,omitempty"`
	OnClick *OnClick `json:"onClick,omitempty"`
}

type OnClick struct {
	Action   *Action   `json:"action,omitempty"`
	OpenLink *OpenLink `json:"openLink,omitempty"`
}

// OpenLink opens a URL in a new tab.
type OpenLink struct {
	URL string `json:"url"`
}

// Action names the endpoint a click or change calls, and what it carries.
type Action struct {
	Function   string  `json:"function"`
	Parameters []Param `json:"parameters,omitempty"`
}

type Param struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Footer struct {
	PrimaryButton   *Button `json:"primaryButton,omitempty"`
	SecondaryButton *Button `json:"secondaryButton,omitempty"`
}

// Response is what the service answers every request with: cards to draw, or a
// request for scopes the user has not granted.
type Response struct {
	// RenderActions is embedded, so its "action" is the top-level key. Calendar
	// parses the body as RenderActions and rejects a "renderActions" wrapper.
	*RenderActions
	// Scopes asks Workspace to show its consent screen; Workspace runs the action
	// again once the user grants them.
	Scopes *ScopeRequest `json:"requesting_google_scopes,omitempty"`
}

// ScopeRequest names the scopes to ask for
// (https://developers.google.com/workspace/add-ons/guides/alternate-runtimes).
type ScopeRequest struct {
	Scopes []string `json:"scopes"`
}

// Ask requests scopes the user has not granted.
func Ask(scopes ...string) Response { return Response{Scopes: &ScopeRequest{Scopes: scopes}} }

type RenderActions struct {
	Action ResponseAction `json:"action"`
}

type ResponseAction struct {
	Navigations  []Navigation  `json:"navigations,omitempty"`
	Notification *Notification `json:"notification,omitempty"`
}

type Navigation struct {
	PopToRoot  bool  `json:"popToRoot,omitempty"`
	Pop        bool  `json:"pop,omitempty"`
	PushCard   *Card `json:"pushCard,omitempty"`
	UpdateCard *Card `json:"updateCard,omitempty"`
}

type Notification struct {
	Text string `json:"text"`
}

// Push shows c on top of the current card.
func Push(c Card) Response { return nav(Navigation{PushCard: &c}) }

// Update replaces the current card with c.
func Update(c Card) Response { return nav(Navigation{UpdateCard: &c}) }

// Root goes back to the first card and replaces it with c.
func Root(c Card) Response { return nav(Navigation{PopToRoot: true}, Navigation{UpdateCard: &c}) }

// Back goes back one card.
func Back() Response { return nav(Navigation{Pop: true}) }

// Notify shows a short message and changes no card.
func Notify(text string) Response {
	return Response{RenderActions: &RenderActions{ResponseAction{Notification: &Notification{Text: text}}}}
}

// With adds a notification to r.
func (r Response) With(text string) Response {
	ra := RenderActions{}
	if r.RenderActions != nil {
		ra = *r.RenderActions
	}
	ra.Action.Notification = &Notification{Text: text}
	r.RenderActions = &ra
	return r
}

func nav(n ...Navigation) Response {
	return Response{RenderActions: &RenderActions{ResponseAction{Navigations: n}}}
}

// Head is the header every Time Tracker card has.
func Head(title, subtitle string) *Header {
	return &Header{Title: title, Subtitle: subtitle, ImageURL: Logo, ImageType: "CIRCLE"}
}

// Text is a paragraph of plain text, escaped.
func Text(s string) Widget { return Widget{TextParagraph: &TextParagraph{Text: Escape(s)}} }

// Warning is a paragraph of plain text in red.
func Warning(s string) Widget {
	return Widget{TextParagraph: &TextParagraph{Text: `<font color="#b3261e">` + Escape(s) + `</font>`}}
}

// Row is a line with an icon, an optional label above it and plain text.
func Row(icon, label, text string) Widget {
	if text == "" {
		text = "—"
	}
	return Widget{DecoratedText: &DecoratedText{StartIcon: &Icon{KnownIcon: icon}, TopLabel: label, Text: Escape(text), WrapText: true}}
}

// Escape makes s safe inside a card's formatted text.
func Escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
