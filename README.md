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

## TODO

- [ ] Repeat after completion
- [ ] Persistent state storage

## Usage

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

## Note

As there is no official API documentation available all requests need to be reverse engineered,
which takes some time. Feel free to contribute and improve & extend this implementation.
