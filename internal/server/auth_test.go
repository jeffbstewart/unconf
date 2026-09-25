package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeffbstewart/unconf/internal/domain"
	"github.com/jeffbstewart/unconf/internal/store"
)

const testAdminKey = "sesame"

func newTestServer(t *testing.T, static fs.FS) *Server {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ev, err := st.EnsureDefaultEvent(ctx, "Test Camp")
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{
		Store:         st,
		EventID:       ev.ID,
		AdminKey:      testAdminKey,
		SessionSecret: []byte("test-secret"),
		Static:        static,
	})
}

// client is a browser stand-in with its own cookie jar.
type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newClient(t *testing.T, ts *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: ts.URL, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path, body string) (*http.Response, map[string]any) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res, out
}

func (c *client) login(body string) map[string]any {
	c.t.Helper()
	res, me := c.do("POST", "/api/login", body)
	if res.StatusCode != http.StatusOK {
		c.t.Fatalf("login %s: status %d %v", body, res.StatusCode, me)
	}
	return me
}

func setup(t *testing.T) (*Server, *httptest.Server) {
	s := newTestServer(t, builtDist)
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts
}

func TestLoginCreatesAndResumes(t *testing.T) {
	_, ts := setup(t)
	a, b := newClient(t, ts), newClient(t, ts)

	meA := a.login(`{"name":"  Ada  "}`)
	if meA["name"] != "Ada" || meA["role"] != "participant" || meA["votesRemaining"] != 5.0 {
		t.Fatalf("new user: %v", meA)
	}
	meB := b.login(`{"name":"Grace"}`)
	if meB["id"] == meA["id"] {
		t.Fatal("distinct names must be distinct users")
	}

	// Each browser sees its own identity.
	if _, me := a.do("GET", "/api/me", ""); me["id"] != meA["id"] {
		t.Fatalf("a /me: %v", me)
	}
	if _, me := b.do("GET", "/api/me", ""); me["id"] != meB["id"] {
		t.Fatalf("b /me: %v", me)
	}

	// Same name, any case, resumes the same user (asserted identity).
	c := newClient(t, ts)
	if me := c.login(`{"name":"ADA"}`); me["id"] != meA["id"] || me["name"] != "Ada" {
		t.Fatalf("resume: %v", me)
	}
}

func TestLoginValidation(t *testing.T) {
	_, ts := setup(t)
	c := newClient(t, ts)
	for _, body := range []string{
		`{"name":""}`,
		`{"name":"   "}`,
		`{"name":"` + strings.Repeat("x", 41) + `"}`,
		`{"name":"Ada","email":"not-an-email"}`,
		`{"name":"Ada","extra":1}`,
		`not json`,
	} {
		if res, _ := c.do("POST", "/api/login", body); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, res.StatusCode)
		}
	}
	req, _ := http.NewRequest("POST", ts.URL+"/api/login", strings.NewReader("name=Ada"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form login: status %d, want 415", res.StatusCode)
	}
}

func TestAdminKeyBootstrapsOrganizer(t *testing.T) {
	s, ts := setup(t)
	c := newClient(t, ts)

	if res, out := c.do("POST", "/api/login", `{"name":"Ada","adminKey":"wrong"}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong key: %d %v", res.StatusCode, out)
	}
	if _, err := s.cfg.Store.UserByName(context.Background(), s.cfg.EventID, "Ada"); err == nil {
		t.Fatal("a rejected admin login must not create the user")
	}

	// An existing participant is promoted by logging in with the key.
	me := c.login(`{"name":"Ada"}`)
	if me["role"] != "participant" {
		t.Fatalf("got %v", me)
	}
	me = newClient(t, ts).login(`{"name":"ada","adminKey":"sesame"}`)
	if me["role"] != "organizer" {
		t.Fatalf("admin key should grant organizer: %v", me)
	}
	// The original browser's session now sees the new role too.
	if _, me := c.do("GET", "/api/me", ""); me["role"] != "organizer" {
		t.Fatalf("role not persisted: %v", me)
	}
	// A new user can be created directly as organizer.
	if me := newClient(t, ts).login(`{"name":"Boss","adminKey":"sesame"}`); me["role"] != "organizer" {
		t.Fatalf("new organizer: %v", me)
	}

	log, err := s.cfg.Store.AuditLog(context.Background(), s.cfg.EventID)
	if err != nil || len(log) != 2 || log[0].Action != "role_changed" {
		t.Fatalf("audit log: %+v, %v", log, err)
	}
}

func TestLoginUpdatesEmail(t *testing.T) {
	s, ts := setup(t)
	newClient(t, ts).login(`{"name":"Ada","email":"a@x.org"}`)
	newClient(t, ts).login(`{"name":"Ada"}`) // omitted email leaves it alone
	u, _ := s.cfg.Store.UserByName(context.Background(), s.cfg.EventID, "Ada")
	if u.Email != "a@x.org" {
		t.Fatalf("email: %q", u.Email)
	}
	newClient(t, ts).login(`{"name":"Ada","email":"b@x.org"}`)
	u, _ = s.cfg.Store.UserByName(context.Background(), s.cfg.EventID, "Ada")
	if u.Email != "b@x.org" {
		t.Fatalf("email: %q", u.Email)
	}
}

func TestSessionRequired(t *testing.T) {
	_, ts := setup(t)
	c := newClient(t, ts)
	for _, p := range []struct{ method, path string }{{"GET", "/api/me"}, {"POST", "/api/logout"}} {
		if res, _ := c.do(p.method, p.path, ""); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without session: %d", p.method, p.path, res.StatusCode)
		}
	}
	if res, _ := c.do("GET", "/api/healthz", ""); res.StatusCode != http.StatusOK {
		t.Error("healthz must not require a session")
	}
}

func TestLogout(t *testing.T) {
	_, ts := setup(t)
	c := newClient(t, ts)
	c.login(`{"name":"Ada"}`)
	if res, _ := c.do("POST", "/api/logout", ""); res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", res.StatusCode)
	}
	if res, _ := c.do("GET", "/api/me", ""); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", res.StatusCode)
	}
}

func TestSessionCookie(t *testing.T) {
	s, ts := setup(t)
	c := newClient(t, ts)
	id := c.login(`{"name":"Ada"}`)["id"].(string)

	res, _ := newClient(t, ts).do("POST", "/api/login", `{"name":"Grace"}`)
	var ck *http.Cookie
	for _, k := range res.Cookies() {
		if k.Name == sessionCookie {
			ck = k
		}
	}
	if ck == nil || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Secure || ck.MaxAge <= 0 {
		t.Fatalf("cookie attributes: %+v", ck)
	}

	meWith := func(value string) int {
		req, _ := http.NewRequest("GET", ts.URL+"/api/me", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := meWith(s.signSession(id)); got != http.StatusOK {
		t.Fatalf("valid cookie: %d", got)
	}
	// Forging a cookie for another id without the secret fails.
	forged := New(Config{SessionSecret: []byte("other")}).signSession(id)
	idPart, _, _ := strings.Cut(s.signSession("someone-else"), ".")
	_, macPart, _ := strings.Cut(s.signSession(id), ".")
	for name, v := range map[string]string{
		"wrong secret":  forged,
		"swapped id":    idPart + "." + macPart,
		"no separator":  "abc",
		"bad base64":    "!!!.!!!",
		"empty":         "",
		"unknown user":  s.signSession(domain.NewID()),
		"trailing junk": s.signSession(id) + "x",
	} {
		if got := meWith(v); got != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", name, got)
		}
	}
}

func TestSecureCookieBehindTLS(t *testing.T) {
	s := newTestServer(t, builtDist)
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"name":"Ada"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("want Secure cookie behind TLS proxy: %+v", cookies)
	}
}

func TestRequireRole(t *testing.T) {
	s, _ := setup(t)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(userFrom(r.Context()).Name))
	})
	s.mux.Handle("GET /api/test/mod", s.requireRole(domain.RoleModerator, ok))
	ts := httptest.NewServer(s)
	defer ts.Close()

	if res, _ := newClient(t, ts).do("GET", "/api/test/mod", ""); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}

	participant := newClient(t, ts)
	participant.login(`{"name":"Pat"}`)
	if res, _ := participant.do("GET", "/api/test/mod", ""); res.StatusCode != http.StatusForbidden {
		t.Fatalf("participant: %d", res.StatusCode)
	}

	mod := newClient(t, ts)
	id := mod.login(`{"name":"Mo"}`)["id"].(string)
	s.cfg.Store.SetUserRole(context.Background(), id, domain.RoleModerator)
	if res, _ := mod.do("GET", "/api/test/mod", ""); res.StatusCode != http.StatusOK {
		t.Fatalf("moderator: %d", res.StatusCode)
	}

	org := newClient(t, ts)
	org.login(`{"name":"Org","adminKey":"sesame"}`)
	if res, _ := org.do("GET", "/api/test/mod", ""); res.StatusCode != http.StatusOK {
		t.Fatalf("organizer: %d", res.StatusCode)
	}
}
