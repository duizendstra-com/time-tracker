# AGENTS.md — time-tracker

Time Tracker as a Google Workspace add-on over HTTP, in Go, on Cloud Run. An Apps
Script version reads and writes the same entries (§ The entry contract).

## Layout

- `main.go`: the server. It binds 127.0.0.1 unless `K_SERVICE` is set, logs JSON for
  Cloud Logging on Cloud Run, and reads `GEMINI_API_KEY`.
- `addon/`: the event object in, the card out. It has one URL, and each card action
  names what it wants in the `do` parameter.
- `card/`: the card JSON (google.apps.card.v1) and `renderActions`.
- `entry/`: the entry contract, and the lists derived from entries.
- `remote/`: everything that reaches the network: Calendar, Sheets and Drive with the
  user's token, and Gemini with the service's key.

## Rules

- **A token or key travels in a header, never in a URL.** No error carries a URL, and
  `remote/` wraps transport errors so a `*url.Error` never reaches a card or a log.
  `remote/remote_test.go` checks both.
- **A failure shows a short card; the detail goes to the log.** A Google API's own
  message may show ("Failed to create event: …"), as the Apps Script version does.
- **Nothing is written until the user chooses Create entry.** Gemini fills the form
  only. Likewise, Rename writes nothing until its confirm card's Rename is chosen.
- **Check the grant before calling Google.** Consent is granular, so any scope can be
  unticked. Each action checks `authorizationEventObject.authorizedScopes` for the
  scopes it uses, and answers `requesting_google_scopes` with the ones missing: calendar
  for everything, plus drive.file for Sync
  ([HTTP add-ons](https://developers.google.com/workspace/add-ons/guides/alternate-runtimes)).

## The entry contract

An entry is an event in the calendar named `Time Tracker`. This is the spec for the
Go version and the Apps Script one alike: change it and both sides change.

- **Tags**: `client`, `project` and `task`, the event's private extended properties
  (what `CalendarEvent.setTag` writes). An event with no `client` tag is not an entry.
  `billable` is `yes` or `no`; an entry without it is not billable, and the title does
  not change.
- **Title**: `Client · Project · Task`, joined by a space, a middle dot (U+00B7) and a
  space.
- **Description**: the note, then `\n\n— Logged with Time Tracker`, which a reader
  strips.
- **Time**: the date and start picked on the card, in `Europe/Tirane`. The Go form also
  picks a length (default one hour), which Gemini can propose. On Update, the length
  starts at the entry's own.
- **Lists**: derived from the entries' tags, with no store. So Rename rewrites every
  entry that uses the name, its tags and its title, after a confirm card with the
  count; there is no Remove.
- **Sheet**: Sync rebuilds the spreadsheet named `Time Tracker`, first sheet `Entries`,
  header row frozen, one row per entry sorted by start: `Date`, `Start`, `End`,
  `Hours`, `Client`, `Project`, `Task`, `Billable` (true or false), `Description` and
  `Event id` (the iCalUID, what `CalendarEvent.getId` answers). Every cell is a typed
  value, never parsed. Without a store, Go finds the file by name through Drive, which
  under drive.file shows only the files this service made.

## Gates

```bash
GOWORK=off go vet ./... && GOWORK=off go test -count=1 ./... && test -z "$(gofmt -l .)"
GOWORK=off ko build --push=false --tarball=/dev/null .
```

## Log

- 2026-09-29: first version. The homepage form with derived lists and "+ New …",
  Gemini's Describe it, create, and update and delete on event open; billable, Sync to
  sheet and Rename; every action checks the granular grant first.
