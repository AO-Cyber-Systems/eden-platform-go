package fixtures

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// cassettesFS embeds the recorded cassettes so replay works regardless of the
// test's working directory. Recorded once, committed, replayed forever — NO
// live external calls in tests (playbook habit #4).
//
//go:embed cassettes/*.json
var cassettesFS embed.FS

// cassette is the on-disk shape of a recorded HTTP exchange.
type cassette struct {
	Description string `json:"description"`
	Request     struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	} `json:"request"`
	Response struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	} `json:"response"`
}

// loadCassette reads and decodes a committed cassette by name (without the
// .json extension) from the embedded filesystem.
func loadCassette(name string) (*cassette, error) {
	raw, err := cassettesFS.ReadFile("cassettes/" + name + ".json")
	if err != nil {
		return nil, fmt.Errorf("fixtures: load cassette %q: %w", name, err)
	}
	var c cassette
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("fixtures: decode cassette %q: %w", name, err)
	}
	return &c, nil
}

// NewCassetteServer spins up an httptest server that replays the named
// cassette's recorded response for its recorded method+path, deterministically
// and with no live calls. Any unrecorded request fails the test, so a drifting
// caller can't silently fall through to a real network call.
//
// Caller is responsible for srv.Close() (typically via defer).
func NewCassetteServer(t *testing.T, name string) *httptest.Server {
	t.Helper()
	c, err := loadCassette(name)
	if err != nil {
		t.Fatalf("NewCassetteServer: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != c.Request.Method || r.URL.Path != c.Request.Path {
			t.Errorf("unrecorded request %s %s (cassette %q records %s %s)",
				r.Method, r.URL.Path, name, c.Request.Method, c.Request.Path)
			http.Error(w, "no cassette match", http.StatusNotImplemented)
			return
		}
		for k, v := range c.Response.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(c.Response.Status)
		_, _ = w.Write([]byte(c.Response.Body))
	})

	return httptest.NewServer(handler)
}
