package sync

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	things "github.com/arthursoares/things-cloud-sdk"
)

// TestSync_PaginatesAllPages verifies that Sync() steps through every page of
// history items. Before the fix, the loop used LatestServerIndex (the server's
// total item count) as the next start-index, which caused the second request to
// ask for items at the very end of the history and receive nothing. The correct
// field is LoadedServerIndex, which accumulates the number of item-batches
// received so far.
func TestSync_PaginatesAllPages(t *testing.T) {
	t.Parallel()

	const historyID = "test-history-id"

	// Page 1: one item-batch, server total = 2 (so hasMore = 1 < 2 = true)
	page1 := `{"items":[{"task-page1":{"e":"Task6","t":0,"p":{"tt":"Page 1 Task","tp":0}}}],"current-item-index":2,"schema":301}`
	// Page 2: one item-batch, server total = 2 (so hasMore = 2 < 2 = false)
	page2 := `{"items":[{"task-page2":{"e":"Task6","t":0,"p":{"tt":"Page 2 Task","tp":0}}}],"current-item-index":2,"schema":301}`
	// History metadata for the pre-check getServerIndex call
	historyMeta := `{"latest-server-index":2,"latest-schema-version":301,"is-empty":false,"latest-total-content-size":0}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		if strings.HasSuffix(path, "/items") {
			startIndex := r.URL.Query().Get("start-index")
			switch startIndex {
			case "0":
				fmt.Fprint(w, page1)
			case "1":
				// Correct next start-index after receiving 1 item-batch (LoadedServerIndex=1)
				fmt.Fprint(w, page2)
			default:
				// Wrong start-index (e.g. LatestServerIndex=2): return empty to simulate the bug
				fmt.Fprint(w, `{"items":[],"current-item-index":2,"schema":301}`)
			}
			return
		}
		// History metadata endpoint
		fmt.Fprint(w, historyMeta)
	}))
	defer ts.Close()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := things.New(ts.URL, "test@example.com", "password")
	syncer, err := Open(dbPath, client)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	// Pre-seed sync state so Sync() skips the OwnHistory() network call
	// and goes straight to fetching items.
	if err := syncer.saveSyncState(historyID, 0); err != nil {
		t.Fatalf("saveSyncState failed: %v", err)
	}

	changes, err := syncer.Sync()
	if err != nil {
		t.Fatalf("Sync() failed: %v", err)
	}

	// Both pages must have been processed
	if len(changes) != 2 {
		t.Errorf("expected 2 changes (one per page), got %d", len(changes))
	}

	state := syncer.State()
	tasks, err := state.AllTasks(QueryOpts{})
	if err != nil {
		t.Fatalf("AllTasks failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks in state, got %d — pagination stopped early", len(tasks))
	}
}

func TestOpen(t *testing.T) {
	t.Parallel()

	t.Run("creates new database", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")

		syncer, err := Open(dbPath, nil)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer syncer.Close()

		// Verify file was created
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Fatal("Database file was not created")
		}

		// Verify schema was applied by checking tables exist
		var tableName string
		err = syncer.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='tasks'").Scan(&tableName)
		if err != nil {
			t.Fatalf("tasks table not created: %v", err)
		}
	})

	t.Run("reopens existing database", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")

		// Create and close
		syncer1, err := Open(dbPath, nil)
		if err != nil {
			t.Fatalf("First Open failed: %v", err)
		}

		// Insert test data
		_, err = syncer1.db.Exec("INSERT INTO areas (uuid, title) VALUES ('test-uuid', 'Test Area')")
		if err != nil {
			t.Fatalf("Insert failed: %v", err)
		}
		syncer1.Close()

		// Reopen
		syncer2, err := Open(dbPath, nil)
		if err != nil {
			t.Fatalf("Second Open failed: %v", err)
		}
		defer syncer2.Close()

		// Verify data persisted
		var title string
		err = syncer2.db.QueryRow("SELECT title FROM areas WHERE uuid = 'test-uuid'").Scan(&title)
		if err != nil {
			t.Fatalf("Data not persisted: %v", err)
		}
		if title != "Test Area" {
			t.Fatalf("Expected 'Test Area', got %q", title)
		}
	})
}
