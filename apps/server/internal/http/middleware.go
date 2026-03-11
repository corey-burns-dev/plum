package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"time"

	"plum/internal/db"
)

type contextKey string

const userContextKey contextKey = "plum.user"

func UserFromContext(ctx context.Context) *db.User {
	val := ctx.Value(userContextKey)
	if val == nil {
		return nil
	}
	if u, ok := val.(*db.User); ok {
		return u
	}
	return nil
}

func withUser(ctx context.Context, u *db.User) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

func sessionCookieName() string {
	if v := os.Getenv("PLUM_SESSION_COOKIE"); v != "" {
		return v
	}
	return "plum_session"
}

func sessionIDFromRequest(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName())
	if err != nil {
		return ""
	}
	return c.Value
}

func setSessionCookie(w http.ResponseWriter, sessionID string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName(),
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
		Expires:  expires,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName(),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

func AuthMiddleware(dbConn *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessID := sessionIDFromRequest(r)
			if sessID == "" {
				next.ServeHTTP(w, r)
				return
			}

			var (
				userID    int
				expiresAt time.Time
			)
			err := dbConn.QueryRow(
				`SELECT user_id, expires_at FROM sessions WHERE id = ?`,
				sessID,
			).Scan(&userID, &expiresAt)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if time.Now().After(expiresAt) {
				_, _ = dbConn.Exec(`DELETE FROM sessions WHERE id = ?`, sessID)
				clearSessionCookie(w)
				next.ServeHTTP(w, r)
				return
			}

			var u db.User
			err = dbConn.QueryRow(
				`SELECT id, email, is_admin, created_at FROM users WHERE id = ?`,
				userID,
			).Scan(&u.ID, &u.Email, &u.IsAdmin, &u.CreatedAt)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := withUser(r.Context(), &u)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil || !u.IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
