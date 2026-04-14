package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
)

func TestDetectSupervisor(t *testing.T) {
	t.Run("supervisor with cities", func(t *testing.T) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v0/ws" {
				http.NotFound(w, r)
				return
			}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Fatalf("upgrade: %v", err)
			}
			defer conn.Close()
			_ = conn.WriteJSON(map[string]any{"type": "hello"})
			var req struct {
				Action string `json:"action"`
			}
			if err := conn.ReadJSON(&req); err != nil {
				t.Fatalf("read request: %v", err)
			}
			if req.Action != "cities.list" {
				t.Fatalf("action = %q, want cities.list", req.Action)
			}
			_ = conn.WriteJSON(map[string]any{
				"type": "response",
				"id":   "cli-1",
				"result": map[string]any{
					"items": []map[string]any{
						{"name": "bright-lights"},
						{"name": "test-city"},
					},
				},
			})
		}))
		defer srv.Close()

		if !detectSupervisor(srv.URL) {
			t.Error("detectSupervisor() = false, want true")
		}
	})

	t.Run("standalone mode (404)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer srv.Close()

		if detectSupervisor(srv.URL) {
			t.Error("detectSupervisor() = true, want false")
		}
	})

	t.Run("unreachable server", func(t *testing.T) {
		if detectSupervisor("http://127.0.0.1:1") {
			t.Error("detectSupervisor() = true, want false")
		}
	})
}

func TestValidateAPI(t *testing.T) {
	t.Run("reachable health endpoint", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		if err := ValidateAPI(srv.URL); err != nil {
			t.Fatalf("ValidateAPI(%q): %v", srv.URL, err)
		}
	})

	t.Run("non-200 health endpoint", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "broken", http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		if err := ValidateAPI(srv.URL); err == nil {
			t.Fatal("ValidateAPI() succeeded for unhealthy server")
		}
	})
}
