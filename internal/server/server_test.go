package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, method, target string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec.Result()
}

func body(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var builtDist = fstest.MapFS{
	"index.html":        {Data: []byte("<html>unconf</html>")},
	"favicon.svg":       {Data: []byte("<svg/>")},
	"assets/app-abc.js": {Data: []byte("console.log(1)")},
}

func TestHealthz(t *testing.T) {
	res := get(t, newTestServer(t, builtDist), "GET", "/api/healthz")
	if res.StatusCode != http.StatusOK || body(t, res) != "ok\n" {
		t.Fatalf("got %d", res.StatusCode)
	}
}

func TestUnknownAPIIs404NotSPA(t *testing.T) {
	res := get(t, newTestServer(t, builtDist), "GET", "/api/nope")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("got %d, want 404", res.StatusCode)
	}
}

func TestStatic(t *testing.T) {
	h := newTestServer(t, builtDist)
	tests := []struct {
		target, wantBody, wantCache string
	}{
		{"/", "<html>unconf</html>", "no-cache"},
		{"/index.html", "<html>unconf</html>", "no-cache"},
		{"/schedule", "<html>unconf</html>", "no-cache"}, // SPA fallback
		{"/assets", "<html>unconf</html>", "no-cache"},   // directory → fallback
		{"/favicon.svg", "<svg/>", "no-cache"},
		{"/assets/app-abc.js", "console.log(1)", "public, max-age=31536000, immutable"},
	}
	for _, tt := range tests {
		res := get(t, h, "GET", tt.target)
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", tt.target, res.StatusCode)
			continue
		}
		if got := body(t, res); got != tt.wantBody {
			t.Errorf("%s: body %q, want %q", tt.target, got, tt.wantBody)
		}
		if got := res.Header.Get("Cache-Control"); got != tt.wantCache {
			t.Errorf("%s: Cache-Control %q, want %q", tt.target, got, tt.wantCache)
		}
	}
}

func TestStaticRejectsNonGet(t *testing.T) {
	res := get(t, newTestServer(t, builtDist), "POST", "/")
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want 405", res.StatusCode)
	}
}

func TestNotBuilt(t *testing.T) {
	res := get(t, newTestServer(t, fstest.MapFS{".gitkeep": {}}), "GET", "/")
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", res.StatusCode)
	}
	if !strings.Contains(body(t, res), "make build") {
		t.Fatal("expected not-built notice")
	}
}
