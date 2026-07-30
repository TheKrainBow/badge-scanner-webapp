// Package auth implements the dashboard's real auth layer — user accounts
// (bcrypt password, JWT session cookie) with an env-bootstrapped admin and
// admin-managed user creation from then on — plus a separate, optional
// scoped API-key gate (X-Client-Id/X-Client-Secret, checked against
// DB-backed keys with permission scopes — see APIKey in internal/store and
// RequireAPIKeyPermission below) used only by external non-browser clients
// like the C badge-lookup client's "lookup"-scope key.
//
// The dashboard itself does NOT require an API key — only a session. An
// earlier version of this backend gated every route (including login)
// behind a "full"-scope key mirroring an even earlier single static shared
// secret; that meant deleting or losing that one key locked the whole
// dashboard out of its own backend, including the Admin page that manages
// keys, with the session auth underneath adding no protection against that
// self-lockout. Session auth was already the real security boundary
// (RequireSession/RequireAdmin below), so the API-key layer was pure
// redundant risk for browser traffic and has been removed from those
// routes — it now exists solely to authenticate machine clients that can
// set custom headers on a request (which a browser's own fetch/WebSocket
// calls already do implicitly via cookies instead).
package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"badgescanner/backend/internal/store"
)

const SessionCookieName = "badgescanner_session"
const sessionTTL = 24 * time.Hour

type Service struct {
	st           *store.Store
	jwtSecret    []byte
	secureCookie bool
}

// secureCookie should be true in any real deployment (HTTPS); set it false
// only for plain-HTTP local development, where browsers won't store a
// Secure cookie at all.
func NewService(st *store.Store, jwtSecret string, secureCookie bool) *Service {
	return &Service{st: st, jwtSecret: []byte(jwtSecret), secureCookie: secureCookie}
}

// Bootstrap creates the single hardcoded admin from env vars if (and only
// if) no users exist yet. Every user after that is created from the admin
// interface — see internal/api's admin user-management handlers.
func (s *Service) Bootstrap(adminUsername, adminPassword string) error {
	n, err := s.st.CountUsers()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if adminUsername == "" || adminPassword == "" {
		return errors.New("no users exist yet: ADMIN_USERNAME and ADMIN_PASSWORD must be set to bootstrap the first admin")
	}
	hash, err := HashPassword(adminPassword)
	if err != nil {
		return err
	}
	_, err = s.st.CreateUser(adminUsername, hash, true)
	return err
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

type claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"isAdmin"`
	jwt.RegisteredClaims
}

func (s *Service) IssueSession(w http.ResponseWriter, u store.User) error {
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		UserID:   u.ID,
		Username: u.Username,
		IsAdmin:  u.IsAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(sessionTTL)),
		},
	})
	signed, err := tok.SignedString(s.jwtSecret)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    signed,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return nil
}

func (s *Service) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// Identity is the authenticated caller, attached to the request context.
type Identity struct {
	UserID   int64
	Username string
	IsAdmin  bool
}

type contextKey string

const identityContextKey contextKey = "identity"

func (s *Service) parseSession(r *http.Request) (*Identity, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, errors.New("not authenticated")
	}
	tok, err := jwt.ParseWithClaims(cookie.Value, &claims{}, func(t *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid session")
	}
	c, ok := tok.Claims.(*claims)
	if !ok {
		return nil, errors.New("invalid session")
	}
	return &Identity{UserID: c.UserID, Username: c.Username, IsAdmin: c.IsAdmin}, nil
}

// RequireSession is HTTP middleware enforcing a valid user session.
func (s *Service) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.parseSession(r)
		if err != nil {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), identityContextKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin is HTTP middleware enforcing a valid *admin* session.
func (s *Service) RequireAdmin(next http.Handler) http.Handler {
	return s.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := FromContext(r.Context())
		if id == nil || !id.IsAdmin {
			http.Error(w, `{"error":"admin required"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func FromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityContextKey).(*Identity)
	return id
}

// BootstrapAPIKey optionally creates one "full"-scope API key from env
// vars, if (and only if) both are set and no keys exist yet. Unlike
// Bootstrap (admin account) above, this is entirely optional — nothing in
// the dashboard needs a key to exist (see this package's doc comment) — it
// only matters if you want an external "full"-access automation client
// pre-provisioned at first boot instead of created later from the Admin
// page's API Keys section. No env vars set = silently does nothing.
func (s *Service) BootstrapAPIKey(clientID, secret string) error {
	if clientID == "" || secret == "" {
		return nil
	}
	n, err := s.st.CountAPIKeys()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := HashPassword(secret)
	if err != nil {
		return err
	}
	_, err = s.st.CreateAPIKey(clientID, hash, "bootstrap", "full", 0, 0)
	return err
}

// APIKeyIdentity is the authenticated API-key caller, attached to the
// request context by RequireAPIKeyPermission. RateLimitPerMinute/Hour (0 =
// unlimited) are enforced by internal/api's performLookup, not here — this
// middleware only authenticates and scope-checks.
type APIKeyIdentity struct {
	ID                 int64
	ClientID           string
	Name               string
	Permissions        []string
	RateLimitPerMinute int
	RateLimitPerHour   int
}

// HasPermission reports whether this key grants perm — a "full" key grants
// every permission, matching how "full" replaces the old all-or-nothing
// static client secret.
func (i APIKeyIdentity) HasPermission(perm string) bool {
	for _, p := range i.Permissions {
		if p == perm || p == "full" {
			return true
		}
	}
	return false
}

const apiKeyContextKey contextKey = "apiKeyIdentity"

func APIKeyFromContext(ctx context.Context) *APIKeyIdentity {
	id, _ := ctx.Value(apiKeyContextKey).(*APIKeyIdentity)
	return id
}

// RequireAPIKeyPermission is HTTP middleware enforcing a valid API key
// (X-Client-Id/X-Client-Secret headers) with at least the given permission
// scope, checked before any user session. This is a coarse app-level gate,
// not a strong secret (a browser build's key is readable via devtools);
// the real security boundary for "full"-scope routes is user auth plus
// network placement (VPN/reverse proxy/allowlist). For "lookup"-scope
// routes the scope restriction itself *is* the boundary — see
// service.Lookup's doc comment for why it can't leak blame/TIG/points data.
func (s *Service) RequireAPIKeyPermission(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientID := r.Header.Get("X-Client-Id")
			secret := r.Header.Get("X-Client-Secret")
			if clientID == "" || secret == "" {
				http.Error(w, `{"error":"invalid client credentials"}`, http.StatusUnauthorized)
				return
			}
			key, err := s.st.GetAPIKeyByClientID(clientID)
			if err != nil || !CheckPassword(key.SecretHash, secret) {
				http.Error(w, `{"error":"invalid client credentials"}`, http.StatusUnauthorized)
				return
			}
			identity := APIKeyIdentity{
				ID: key.ID, ClientID: key.ClientID, Name: key.Name, Permissions: key.Scopes(),
				RateLimitPerMinute: key.RateLimitPerMinute, RateLimitPerHour: key.RateLimitPerHour,
			}
			if !identity.HasPermission(perm) {
				http.Error(w, `{"error":"insufficient API key permission"}`, http.StatusForbidden)
				return
			}
			_ = s.st.TouchAPIKeyLastUsed(key.ID) // best-effort, ignore error
			ctx := context.WithValue(r.Context(), apiKeyContextKey, &identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
