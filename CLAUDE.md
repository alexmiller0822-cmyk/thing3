# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go SDK for the Things 3 cloud API (Cultured Code). This is a reverse-engineered, unofficial SDK — there is no official API documentation. The client mimics `ThingsMac/32209501` and sends a base64-encoded `things-client-info` device metadata header.

## Build & Test Commands

```bash
go build -v ./...          # Build all packages
go test -v ./...           # Run all tests
go test -v -run TestName   # Run a single test
go test -v ./state/memory  # Run tests for a specific package
go generate                # Regenerate stringer methods (itemaction_string.go)
```

## Architecture

All source code lives at the package root (`package things`), with one sub-package (`state/memory`).

### Core Design: Event-Sourced Sync

The SDK models all changes as immutable **Items** (events). A **History** is a sync stream identified by a UUID. The client pushes/pulls Items through Histories to stay in sync with the Things Cloud server.

- **`client.go`** — HTTP client with `ClientInfo` header and configurable `Debug` logging, base endpoint `https://cloud.culturedcode.com`
- **`histories.go`** — History CRUD and sync operations (list, create, delete, read/write items with ancestor indices)
- **`items.go`** — Item construction: every mutation (create/modify/delete) on a Task, Area, Tag, CheckListItem, or Tombstone produces an Item
- **`types.go`** — Domain types: `Task` (with `TaskType` enum: Task/Project/Heading), `Area`, `Tag`, `CheckListItem`, `Tombstone`, plus custom JSON types (`Timestamp`, `Boolean`)
- **`notes.go`** — Structured `Note` type with full-text and delta patch support (`ApplyPatches`)
- **`repeat.go`** — Recurring task date calculation (daily/weekly/monthly/yearly, with end conditions)
- **`helpers.go`** — Pointer helpers (`String()`, `Status()`, `Schedule()`, `Time()`, `TaskTypePtr()`)
- **`account.go`** — AccountService for signup, confirmation, password change, deletion
- **`app_instance.go`** — Device registration for APNS push notifications

### State Aggregation

`state/memory` provides an in-memory store that aggregates Items into a queryable hierarchy: Areas → Tasks → Subtasks → CheckListItems. Key queries: `Projects()`, `Headings()`, `TasksByHeading()`, `TasksByArea()`, `Subtasks()`, `CheckListItemsByTask()`. It is **not thread-safe**.

### Test Infrastructure

Tests use `httptest.Server` with pre-recorded JSON responses in the `tapes/` directory. Tests use `t.Parallel()`.

### Code Generation

`types.go` contains `//go:generate stringer -type ItemAction,TaskStatus,TaskSchedule`. Run `go generate` after modifying these enum types. Do not hand-edit `itemaction_string.go`.

### Web UI

`cmd/thingsweb/` contains a Polymer-based web UI with its own build process (see its README). It uses `statik` for static file embedding.

## Environment Variables

The example app and real usage require:
- `THINGS_USERNAME` — Things account email
- `THINGS_PASSWORD` — Things account password
