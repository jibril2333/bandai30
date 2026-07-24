// Package auth handles password hashing and cookie-based sessions.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/rei/bandai30/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	CookieName      = "bandai30_session"
	SessionDuration = 30 * 24 * time.Hour // 30 days
)

type ctxKey int

const userKey ctxKey = 1

// HashPassword returns a bcrypt hash suitable for storing.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword returns nil if the password matches.
func CheckPassword(hash, pw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
}

// newToken returns 32 bytes of randomness as a URL-safe base64 string.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// IssueSession creates a session row + sets the cookie. Caller must have already
// verified the password.
func IssueSession(ctx context.Context, st *store.Store, w http.ResponseWriter, r *http.Request, username string) error {
	tok, err := newToken()
	if err != nil {
		return err
	}
	exp := time.Now().Add(SessionDuration)
	if err := st.CreateSession(ctx, tok, username, exp.Unix()); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    tok,
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// RevokeSession deletes the session row and clears the cookie.
func RevokeSession(ctx context.Context, st *store.Store, w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(CookieName)
	if err == nil && c.Value != "" {
		_ = st.DeleteSession(ctx, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// CurrentUser returns the username stored in the request context, or "" if unauth.
func CurrentUser(r *http.Request) string {
	v, _ := r.Context().Value(userKey).(string)
	return v
}

// Middleware resolves the session cookie into a username on the request context.
// It does NOT block unauthenticated requests — RequireAuth does that.
func Middleware(st *store.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err == nil && c.Value != "" {
			if user, ok, lerr := st.LookupSession(r.Context(), c.Value); lerr == nil && ok {
				ctx := contextWithUser(r.Context(), user)
				r = r.WithContext(ctx)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth wraps a handler to require a logged-in user.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r) == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func contextWithUser(ctx context.Context, u string) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// AnonMiddleware injects a fixed "anon" username on every request. Use only when
// the server is started with --no-auth and trust is enforced at the network
// layer (e.g. Cloudflare Access in front of the tunnel).
func AnonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(contextWithUser(r.Context(), "anon")))
	})
}

// isHTTPS reports whether the original request was HTTPS, honouring X-Forwarded-Proto
// so the app works behind Cloudflare Tunnel / reverse proxies.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if xfp := r.Header.Get("X-Forwarded-Proto"); xfp == "https" {
		return true
	}
	return false
}

// EnsureSeedUser creates an admin account from env vars if no users exist yet.
// Returns the username it created, or "" if the table already had users.
func EnsureSeedUser(ctx context.Context, st *store.Store, username, password string) (string, error) {
	n, err := st.UserCount(ctx)
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", nil
	}
	if username == "" || password == "" {
		return "", errors.New("no users exist and BANDAI30_ADMIN_USER / BANDAI30_ADMIN_PASS are not set")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	if err := st.CreateUser(ctx, username, hash); err != nil {
		return "", err
	}
	return username, nil
}
