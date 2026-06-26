package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"booklet/db"
	"booklet/logger"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	jwt.RegisteredClaims
}

type User struct {
	ID    string
	Email string
	Name  string
}

var (
	jwtSecret     []byte
	oauth2Config  *oauth2.Config
	oidcProvider  *oidc.Provider
	oidcVerifier  *oidc.IDTokenVerifier
	useOIDC       bool
	cookieName    = "session_token"
	ctxUserKey    = struct{}{}
)

func InitAuth() {
	// Initialize JWT Secret
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		// Generate random secret if not provided
		bytes := make([]byte, 32)
		if _, err := rand.Read(bytes); err != nil {
			log.Fatalf("failed to generate random JWT secret: %v", err)
		}
		jwtSecret = bytes
		log.Println("Generated temporary random JWT secret.")
	} else {
		jwtSecret = []byte(secret)
	}

	// Initialize OIDC config
	issuer := os.Getenv("OIDC_ISSUER_URL")
	clientID := os.Getenv("OIDC_CLIENT_ID")
	clientSecret := os.Getenv("OIDC_CLIENT_SECRET")
	redirectURL := os.Getenv("OIDC_REDIRECT_URL")

	if issuer != "" && clientID != "" && clientSecret != "" && redirectURL != "" {
		log.Printf("Initializing OIDC Auth (Issuer: %s)...", issuer)
		
		// Run provider setup in background or retry to prevent startup crash if OIDC is booting up
		ctx := context.Background()
		var provider *oidc.Provider
		var err error
		
		for i := 0; i < 5; i++ {
			provider, err = oidc.NewProvider(ctx, issuer)
			if err == nil {
				break
			}
			log.Printf("Failed to connect to OIDC provider (attempt %d/5): %v. Retrying...", i+1, err)
			time.Sleep(3 * time.Second)
		}

		if err != nil {
			log.Printf("OIDC initialization failed: %v. Falling back to Mock Auth.", err)
			useOIDC = false
			return
		}

		oidcProvider = provider
		oidcVerifier = provider.Verifier(&oidc.Config{ClientID: clientID})
		
		oauth2Config = &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		}
		useOIDC = true
		log.Println("OIDC Auth successfully initialized.")
	} else {
		log.Println("OIDC environment variables not fully configured. Using Mock Auth.")
		useOIDC = false
	}
}

// GenerateToken creates a signed JWT for user sessions
func GenerateToken(u User) (string, error) {
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		UserID: u.ID,
		Email:  u.Email,
		Name:   u.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// VerifyToken decodes and validates a session JWT
func VerifyToken(tokenStr string) (*User, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errors.New("invalid session token")
	}

	return &User{
		ID:    claims.UserID,
		Email: claims.Email,
		Name:  claims.Name,
	}, nil
}

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

// Handlers for Auth routing

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if !useOIDC {
		logger.Logf(r.Context(), "HandleLogin: OIDC authentication is not configured")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"OIDC authentication is not configured"}`)
		return
	}

	// Generate random state for CSRF prevention
	stateBytes := make([]byte, 16)
	rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)

	// Set state as a short-lived cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    state,
		Path:     "/",
		Expires:  time.Now().Add(10 * time.Minute),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	url := oauth2Config.AuthCodeURL(state)
	logger.Logf(r.Context(), "HandleLogin: redirecting user to OIDC provider with state segment")
	http.Redirect(w, r, url, http.StatusFound)
}

// HandleDevLogin handles the developer bypass login.
// This handler must only be registered in the router when APP_ENV=development.
// It creates a fully functional session (real JWT + real DB upsert) with a
// "dev_" prefixed user ID to prevent any collision with real OIDC subject IDs.
func HandleDevLogin(w http.ResponseWriter, r *http.Request) {
	// Double-check guard in case the handler was somehow called outside dev routing.
	if os.Getenv("APP_ENV") != "development" {
		logger.Logf(r.Context(), "HandleDevLogin: blocked attempt outside development mode")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"developer bypass is only available in development environments"}`)
		return
	}

	// Resolve email and name from query params, then env vars, then hardcoded defaults.
	devEmail := r.URL.Query().Get("email")
	devName := r.URL.Query().Get("name")

	if devEmail == "" {
		devEmail = os.Getenv("DEV_USER_EMAIL")
	}
	if devEmail == "" {
		devEmail = "dev@booklet.local"
	}
	if devName == "" {
		devName = os.Getenv("DEV_USER_NAME")
	}
	if devName == "" {
		devName = "Developer User"
	}

	// Prefix with "dev_" to ensure this ID can never collide with a real OIDC sub claim.
	user := User{
		ID:    "dev_" + sanitizeIDSegment(devEmail),
		Email: devEmail,
		Name:  devName,
	}

	logger.Logf(r.Context(), "[DEV BYPASS] Creating session for user: id=%s email=%s name=%s", user.ID, user.Email, user.Name)

	// Detect a stale user row that shares the same email but has a different ID.
	// This can happen when switching from the old hardcoded "usr_dev" ID scheme to
	// the new "dev_*" scheme. We do NOT silently delete it — instead we return a
	// clear 409 so the developer knows exactly what to fix and can clean it up
	// deliberately rather than having data removed behind their back.
	var staleID string
	_ = db.DB.QueryRow(`SELECT id FROM users WHERE email = $1 AND id != $2`, user.Email, user.ID).Scan(&staleID)
	if staleID != "" {
		logger.Logf(r.Context(), "[DEV BYPASS] Conflict: email %s exists as stale ID %s", user.Email, staleID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w,
			`{"error":"dev bypass conflict: email %q is already registered under a different user ID (%q). `+
				`Remove the stale row manually: DELETE FROM users WHERE id = '%s';"}`,
			user.Email, staleID, staleID,
		)
		return
	}

	// Upsert user into DB so that /auth/me can return full user info.
	_, err := db.DB.Exec(`
		INSERT INTO users (id, email, name, updated_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name, updated_at = CURRENT_TIMESTAMP;
	`, user.ID, user.Email, user.Name)
	if err != nil {
		logger.Logf(r.Context(), "[DEV BYPASS] DB Error upserting user: %v", err)
		http.Error(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}

	tokenStr, err := GenerateToken(user)
	if err != nil {
		logger.Logf(r.Context(), "[DEV BYPASS] Session token generation failed: %v", err)
		http.Error(w, "session generation failed", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tokenStr,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	logger.Logf(r.Context(), "[DEV BYPASS] Authentication successful, redirecting to %s", frontendURL)
	http.Redirect(w, r, frontendURL+"/", http.StatusFound)
}

// sanitizeIDSegment converts an email-like string into a safe ID segment
// by replacing non-alphanumeric characters with underscores.
func sanitizeIDSegment(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			result[i] = c
		} else {
			result[i] = '_'
		}
	}
	return string(result)
}

func HandleCallback(w http.ResponseWriter, r *http.Request) {
	if !useOIDC {
		logger.Logf(r.Context(), "HandleCallback: OIDC auth not enabled")
		http.Error(w, "OIDC auth not enabled", http.StatusBadRequest)
		return
	}

	stateCookie, err := r.Cookie("oidc_state")
	if err != nil {
		logger.Logf(r.Context(), "HandleCallback: missing state cookie: %v", err)
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}

	if r.URL.Query().Get("state") != stateCookie.Value {
		logger.Logf(r.Context(), "HandleCallback: state parameter mismatch (CSRF?)")
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return
	}

	// Exchange code for token
	oauth2Token, err := oauth2Config.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		logger.Logf(r.Context(), "HandleCallback: failed to exchange token: %v", err)
		http.Error(w, fmt.Sprintf("failed to exchange token: %v", err), http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		logger.Logf(r.Context(), "HandleCallback: no id_token in response")
		http.Error(w, "no id_token in response", http.StatusInternalServerError)
		return
	}

	idToken, err := oidcVerifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		logger.Logf(r.Context(), "HandleCallback: failed to verify id token: %v", err)
		http.Error(w, fmt.Sprintf("failed to verify id token: %v", err), http.StatusInternalServerError)
		return
	}

	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
	}

	if err := idToken.Claims(&claims); err != nil {
		logger.Logf(r.Context(), "HandleCallback: failed to parse claims: %v", err)
		http.Error(w, "failed to parse claims", http.StatusInternalServerError)
		return
	}

	user := User{
		ID:    claims.Subject,
		Email: claims.Email,
		Name:  claims.Name,
	}

	logger.Logf(r.Context(), "HandleCallback: authenticating and upserting user: subject=%s email=%s", user.ID, user.Email)

	// Upsert user in database
	_, err = db.DB.Exec(`
		INSERT INTO users (id, email, name, updated_at) 
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name, updated_at = CURRENT_TIMESTAMP;
	`, user.ID, user.Email, user.Name)
	if err != nil {
		logger.Logf(r.Context(), "HandleCallback: failed to save user info to database: %v", err)
		http.Error(w, "failed to save user info to database", http.StatusInternalServerError)
		return
	}

	// Generate session token
	tokenStr, err := GenerateToken(user)
	if err != nil {
		logger.Logf(r.Context(), "HandleCallback: session generation failed: %v", err)
		http.Error(w, "session generation failed", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tokenStr,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to frontend
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	logger.Logf(r.Context(), "HandleCallback: authentication successful, redirecting to %s", frontendURL)
	http.Redirect(w, r, frontendURL+"/", http.StatusFound)
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	logger.Logf(r.Context(), "HandleLogout: clearing session cookie")
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	http.Redirect(w, r, frontendURL+"/login", http.StatusFound)
}

func HandleMe(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		logger.Logf(r.Context(), "HandleMe: missing session cookie, returning unauthenticated")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"authenticated":false}`)
		return
	}

	user, err := VerifyToken(cookie.Value)
	if err != nil {
		logger.Logf(r.Context(), "HandleMe: token verification failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"authenticated":false}`)
		return
	}

	logger.Logf(r.Context(), "HandleMe: authenticated session active for user %s (%s)", user.Name, user.Email)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"authenticated":true,"user":{"id":"%s","email":"%s","name":"%s"}}`, user.ID, user.Email, user.Name)
}
