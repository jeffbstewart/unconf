package server

// Asserted identity (SPEC §6): a user logs in by typing a name, and an
// existing name resumes that user. This file is the entire auth surface so
// SSO can replace it later.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/jeffbstewart/unconf/internal/domain"
	"github.com/jeffbstewart/unconf/internal/store"
)

const (
	sessionCookie    = "unconf_session"
	sessionMaxAge    = 30 * 24 * time.Hour // events can span weeks of ideation
	maxLoginBodySize = 4 << 10
)

type ctxKey struct{}

// userFrom returns the authenticated user attached by requireUser.
func userFrom(ctx context.Context) store.User {
	u, _ := ctx.Value(ctxKey{}).(store.User)
	return u
}

// signSession returns base64url(userID) + "." + base64url(HMAC-SHA256(userID)).
func (s *Server) signSession(userID string) string {
	mac := hmac.New(sha256.New, s.cfg.SessionSecret)
	mac.Write([]byte(userID))
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(userID)) + "." + enc.EncodeToString(mac.Sum(nil))
}

// verifySession returns the user id from a cookie value if its MAC is valid.
func (s *Server) verifySession(value string) (string, bool) {
	idPart, macPart, ok := strings.Cut(value, ".")
	if !ok {
		return "", false
	}
	enc := base64.RawURLEncoding
	id, err := enc.DecodeString(idPart)
	if err != nil || len(id) == 0 {
		return "", false
	}
	got, err := enc.DecodeString(macPart)
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, s.cfg.SessionSecret)
	mac.Write(id)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return "", false
	}
	return string(id), true
}

func isTLS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, userID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.signSession(userID),
		Path:     "/",
		MaxAge:   int(sessionMaxAge / time.Second),
		HttpOnly: true,
		Secure:   isTLS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isTLS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// authenticate resolves the request's session cookie to a user of this
// server's event.
func (s *Server) authenticate(r *http.Request) (store.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.User{}, false
	}
	id, ok := s.verifySession(c.Value)
	if !ok {
		return store.User{}, false
	}
	u, err := s.cfg.Store.UserByID(r.Context(), id)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			log.Printf("auth: load user: %v", err)
		}
		return store.User{}, false
	}
	return u, u.EventID == s.cfg.EventID
}

// requireUser rejects requests without a valid session with 401 and
// attaches the user to the request context.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.authenticate(r)
		if !ok {
			clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

// requireRole wraps next so only users holding at least min get through
// (401 without a session, 403 with too low a role).
func (s *Server) requireRole(min domain.Role, next http.Handler) http.Handler {
	return s.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r.Context()).Role.AtLeast(min) {
			writeError(w, http.StatusForbidden, "requires "+string(min))
			return
		}
		next.ServeHTTP(w, r)
	}))
}

type loginRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	AdminKey string `json:"adminKey"`
}

type meResponse struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Role           domain.Role `json:"role"`
	VotesRemaining int         `json:"votesRemaining"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Requiring a JSON content type means a cross-site HTML form cannot
	// submit a login (it would need a CORS preflight).
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "expected application/json")
		return
	}
	var req loginRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLoginBodySize))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, err := domain.NormalizeName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	email, err := domain.NormalizeEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	asOrganizer := false
	if req.AdminKey != "" {
		if subtle.ConstantTimeCompare([]byte(req.AdminKey), []byte(s.cfg.AdminKey)) != 1 {
			writeError(w, http.StatusForbidden, "invalid admin key")
			return
		}
		asOrganizer = true
	}

	u, err := s.loginUser(r.Context(), name, email, asOrganizer)
	if err != nil {
		log.Printf("login: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.setSessionCookie(w, r, u.ID)
	s.writeMe(w, r.Context(), u)
}

// loginUser resumes the user with this name, or creates one. A matching
// admin key promotes the user to organizer.
func (s *Server) loginUser(ctx context.Context, name, email string, asOrganizer bool) (store.User, error) {
	st := s.cfg.Store
	u, err := st.UserByName(ctx, s.cfg.EventID, name)
	if errors.Is(err, store.ErrNotFound) {
		u = store.User{
			ID:        domain.NewID(),
			EventID:   s.cfg.EventID,
			Name:      name,
			Email:     email,
			Role:      domain.RoleParticipant,
			CreatedAt: domain.Timestamp(time.Now()),
		}
		if asOrganizer {
			u.Role = domain.RoleOrganizer
		}
		err = st.CreateUser(ctx, u)
		if errors.Is(err, store.ErrConflict) {
			// A concurrent login created the same name; resume it instead.
			return s.loginUser(ctx, name, email, asOrganizer)
		}
		if err == nil && asOrganizer {
			s.auditRoleChange(ctx, u.ID, "", domain.RoleOrganizer)
		}
		return u, err
	}
	if err != nil {
		return store.User{}, err
	}
	if email != "" && email != u.Email {
		if err := st.SetUserEmail(ctx, u.ID, email); err != nil {
			return store.User{}, err
		}
		u.Email = email
	}
	if asOrganizer && u.Role != domain.RoleOrganizer {
		if err := st.SetUserRole(ctx, u.ID, domain.RoleOrganizer); err != nil {
			return store.User{}, err
		}
		s.auditRoleChange(ctx, u.ID, u.Role, domain.RoleOrganizer)
		u.Role = domain.RoleOrganizer
	}
	return u, nil
}

func (s *Server) auditRoleChange(ctx context.Context, userID string, from, to domain.Role) {
	detail, _ := json.Marshal(map[string]string{"from": string(from), "to": string(to), "via": "admin_key"})
	if err := s.cfg.Store.AppendAudit(ctx, store.AuditEntry{
		EventID: s.cfg.EventID, ActorID: userID, Action: "role_changed", Target: userID, Detail: string(detail),
	}); err != nil {
		log.Printf("audit: %v", err)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	s.writeMe(w, r.Context(), userFrom(r.Context()))
}

func (s *Server) writeMe(w http.ResponseWriter, ctx context.Context, u store.User) {
	ev, err := s.cfg.Store.Event(ctx, s.cfg.EventID)
	if err != nil {
		log.Printf("me: load event: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	cast, err := s.cfg.Store.VotesCast(ctx, u.ID)
	if err != nil {
		log.Printf("me: count votes: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		ID:             u.ID,
		Name:           u.Name,
		Role:           u.Role,
		VotesRemaining: max(0, ev.VotesPerUser-cast),
	})
}
