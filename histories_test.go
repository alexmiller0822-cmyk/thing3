package thingscloud

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// authCapturingServer creates a test server that captures headers and serves a fixture file.
func authCapturingServer(t *testing.T, statusCode int, fixture string) (*httptest.Server, *http.Header) {
	t.Helper()
	var captured http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		f, err := os.Open(fmt.Sprintf("tapes/%s", fixture))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer f.Close()
		content, _ := io.ReadAll(f)
		w.WriteHeader(statusCode)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, string(content))
	}))
	return server, &captured
}

func TestClient_Histories(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		server := fakeServer(fakeResponse{200, "histories-success.json"})
		defer server.Close()

		c := New(fmt.Sprintf("http://%s", server.Listener.Addr().String()), "martin@example.com", "")
		hs, err := c.Histories()
		if err != nil {
			t.Fatalf("Expected history request to succeed, but didn't: %q", err.Error())
		}
		if len(hs) != 1 {
			t.Errorf("Expected to receive %d histories, but got %d", 1, len(hs))
		}
	})

	t.Run("SetsAuthorizationHeader", func(t *testing.T) {
		t.Parallel()
		server, captured := authCapturingServer(t, 200, "histories-success.json")
		defer server.Close()

		c := New(fmt.Sprintf("http://%s", server.Listener.Addr().String()), "martin@example.com", "secret")
		c.Histories() //nolint:errcheck
		if got := (*captured).Get("Authorization"); got != "Password secret" {
			t.Errorf("Authorization = %q, want %q", got, "Password secret")
		}
	})

	t.Run("Error", func(t *testing.T) {
		t.Parallel()
		server := fakeServer(fakeResponse{401, "error.json"})
		defer server.Close()

		c := New(fmt.Sprintf("http://%s", server.Listener.Addr().String()), "unknown@example.com", "")
		_, err := c.Histories()
		if err == nil {
			t.Error("Expected history request to fail, but didn't")
		}
	})
}

func TestClient_CreateHistory(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		server := fakeServer(fakeResponse{200, "create-history-success.json"})
		defer server.Close()

		c := New(fmt.Sprintf("http://%s", server.Listener.Addr().String()), "martin@example.com", "")
		h, err := c.CreateHistory()
		if err != nil {
			t.Fatalf("Expected request to succeed, but didn't: %q", err.Error())
		}
		if h.ID != "33333abb-bfe4-4b03-a5c9-106d42220c72" {
			t.Fatalf("Expected key %s but got %s", "33333abb-bfe4-4b03-a5c9-106d42220c72", h.ID)
		}
	})

	t.Run("SetsAuthorizationHeader", func(t *testing.T) {
		t.Parallel()
		server, captured := authCapturingServer(t, 200, "create-history-success.json")
		defer server.Close()

		c := New(fmt.Sprintf("http://%s", server.Listener.Addr().String()), "martin@example.com", "secret")
		c.CreateHistory() //nolint:errcheck
		if got := (*captured).Get("Authorization"); got != "Password secret" {
			t.Errorf("Authorization = %q, want %q", got, "Password secret")
		}
	})
}

func TestHistory_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		server := fakeServer(fakeResponse{202, "create-history-success.json"})
		defer server.Close()

		c := New(fmt.Sprintf("http://%s", server.Listener.Addr().String()), "martin@example.com", "")
		h := History{Client: c, ID: "33333abb-bfe4-4b03-a5c9-106d42220c72"}
		err := h.Delete()
		if err != nil {
			t.Fatalf("Expected request to succeed, but didn't: %q", err.Error())
		}
	})

	t.Run("SetsAuthorizationHeader", func(t *testing.T) {
		t.Parallel()
		server, captured := authCapturingServer(t, 202, "create-history-success.json")
		defer server.Close()

		c := New(fmt.Sprintf("http://%s", server.Listener.Addr().String()), "martin@example.com", "secret")
		h := History{Client: c, ID: "33333abb-bfe4-4b03-a5c9-106d42220c72"}
		h.Delete() //nolint:errcheck
		if got := (*captured).Get("Authorization"); got != "Password secret" {
			t.Errorf("Authorization = %q, want %q", got, "Password secret")
		}
	})
}

func TestHistory_Sync(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		server := fakeServer(fakeResponse{200, "history-success.json"})
		defer server.Close()

		c := New(fmt.Sprintf("http://%s", server.Listener.Addr().String()), "martin@example.com", "")
		h := History{Client: c, ID: "33333abb-bfe4-4b03-a5c9-106d42220c72"}
		err := h.Sync()
		if err != nil {
			t.Fatalf("Expected request to succeed, but didn't: %q", err.Error())
		}
		if h.LatestServerIndex != 27 {
			t.Errorf("Expected LatestServerIndex of %d, but got %d", 27, h.LatestServerIndex)
		}
	})
}
