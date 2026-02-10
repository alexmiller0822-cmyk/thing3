# things cloud sdk

[Things](https://culturedcode.com/things/) comes with a cloud based API, which can
be used to synchronize data between devices.
This is a golang SDK to interact with that API, opening the API so that you
can enhance your Things experience on iOS and Mac.

[![Go](https://github.com/arthursoares/things-cloud-sdk/actions/workflows/go.yml/badge.svg)](https://github.com/arthursoares/things-cloud-sdk/actions/workflows/go.yml)

## Features

- **Verify Credentials** — validate account access
- **Account Management** — signup, confirmation, password change, deletion
- **History Management** — list, create, delete, sync histories
- **Item Read/Write** — full event-sourced CRUD for tasks, areas, tags, checklist items
- **Task Types** — tasks, projects, and headings (action groups within projects)
- **Structured Notes** — full-text and delta patch support for task notes
- **Recurring Tasks** — neverending, end on date, end after N times
- **Tombstone Deletion** — explicit deletion records via `Tombstone2` entities
- **Device Registration** — register app instances for APNS push notifications
- **Alarm/Reminders** — alarm time offset support on tasks
- **State Aggregation** — in-memory state built from history items, with queries for projects, headings, subtasks, areas, tags, and checklist items

## CLI

`things-cli` is a command-line tool for interacting with Things Cloud directly.

### Setup

```bash
export THINGS_USERNAME='your@email.com'
export THINGS_PASSWORD='yourpassword'
go build -o things-cli ./cmd/things-cli/
```

### Commands

```bash
# Read
things-cli list [--today] [--inbox] [--area NAME] [--project NAME]
things-cli show <uuid>
things-cli areas
things-cli projects
things-cli tags

# Create
things-cli create "Title" [--note ...] [--when today|anytime|someday|inbox] \
  [--deadline YYYY-MM-DD] [--scheduled YYYY-MM-DD] \
  [--project UUID] [--heading UUID] [--area UUID] \
  [--tags UUID,...] [--type task|project|heading]
things-cli create-area "Name"
things-cli create-tag "Name" [--shorthand KEY] [--parent UUID]

# Modify
things-cli edit <uuid> [--title ...] [--note ...] [--when ...] [--deadline ...]
things-cli complete <uuid>
things-cli trash <uuid>
things-cli purge <uuid>
things-cli move-to-today <uuid>
```

### Examples

```bash
# Create a project with tasks
things-cli create "My Project" --type project --when anytime
# → {"status":"created","uuid":"BXmAcvS6yK1eDhW31MuZrL","title":"My Project"}

things-cli create "First Task" --project BXmAcvS6yK1eDhW31MuZrL --when today --note "Details here"

# Create an area and assign tasks
things-cli create-area "Work"
things-cli create "Review PR" --area <area-uuid> --when today --deadline 2026-02-15
```

## SDK Usage

```go
package main

import (
    "fmt"
    "os"

    thingscloud "github.com/nicolai86/things-cloud-sdk"
)

func main() {
    c := thingscloud.New(
        thingscloud.APIEndpoint,
        os.Getenv("THINGS_USERNAME"),
        os.Getenv("THINGS_PASSWORD"),
    )

    resp, err := c.Verify()
    if err != nil {
        panic(err)
    }
    fmt.Printf("Account: %s (status: %s)\n", resp.Email, resp.Status)
}
```

See the `example/` directory for a more complete example including history sync, task creation, and state aggregation.

## Wire Format Notes

Key findings from reverse engineering the Things Cloud sync protocol:

- **UUIDs must be Base58-encoded** (Bitcoin alphabet: `123456789ABCDEFGH...`). Standard UUID strings or other encodings will crash Things.app during sync.
- **`md` (modification date) must be `null` on creates.** Set timestamps only on updates.
- **Schedule field (`st`)**: `0` = Inbox, `1` = Anytime/Today (with `sr`/`tir` dates = Today), `2` = Someday/Upcoming (with dates = Upcoming).
- **Kind strings**: `Task6`, `Tag4`, `ChecklistItem3`, `Area2`, `Tombstone2`

See `docs/client-side-bugs.md` for the full investigation and crash analysis.

## Architecture

The SDK models all changes as immutable Items (events). A History is a sync stream identified by a UUID. The client pushes/pulls Items through Histories, inspired by [operational transformations and Git's internals](https://www.swift.org/blog/how-swifts-server-support-powers-things-cloud/).

## TODO

- [ ] Repeat after completion
- [ ] Persistent state storage

## Note

As there is no official API documentation available all requests need to be reverse engineered,
which takes some time. Feel free to contribute and improve & extend this implementation.
