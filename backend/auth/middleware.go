package auth

import (
	"context"
	"fmt"
	"net/http"

	"booklet/logger"
)

// RequireAuth middleware protects endpoints and injects user session into context
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			logger.Logf(r.Context(), "RequireAuth: unauthorized access (missing session cookie)")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"unauthorized"}`)
			return
		}

		user, err := VerifyToken(cookie.Value)
		if err != nil {
			logger.Logf(r.Context(), "RequireAuth: session expired or token invalid: %v", err)
			// Clear invalid cookie
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
			})
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"session expired"}`)
			return
		}

		logger.Logf(r.Context(), "RequireAuth: user %s (%s) authorized successfully", user.Name, user.Email)
		ctx := context.WithValue(r.Context(), ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth middleware injects user if session exists but does not block requests
func OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err == nil {
			if user, err := VerifyToken(cookie.Value); err == nil {
				logger.Logf(r.Context(), "OptionalAuth: user %s (%s) detected", user.Name, user.Email)
				ctx := context.WithValue(r.Context(), ctxUserKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		logger.Logf(r.Context(), "OptionalAuth: no authenticated user session detected")
		next.ServeHTTP(w, r)
	})
}

func GetUser(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(ctxUserKey).(*User)
	return u, ok
}

// WithUser attaches a session user to a context using the same key RequireAuth
// does. It exists so other packages — handler tests, and the worker if it ever
// needs to act as a user — can build an authenticated context without
// duplicating the unexported context key.
func WithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, ctxUserKey, user)
}
