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
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"unauthorized"}`)
			return
		}

		user, err := VerifyToken(cookie.Value)
		if err != nil {
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
				ctx := context.WithValue(r.Context(), ctxUserKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func GetUser(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(ctxUserKey).(*User)
	return u, ok
}

// Handlers for Auth routing

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if useOIDC {
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
		http.Redirect(w, r, url, http.StatusFound)
	} else {
		// In mock mode, we present a mock login or redirect back with a mock session
		mockEmail := r.URL.Query().Get("email")
		mockName := r.URL.Query().Get("name")
		
		if mockEmail == "" {
			// Present a simple inline login screen if they hit this directly or let frontend handle it.
			// Let's redirect to frontend mock login page or set a default session:
			mockEmail = "dev@example.com"
			mockName = "Developer User"
		}

		user := User{
			ID:    "usr_dev",
			Email: mockEmail,
			Name:  mockName,
		}

		// Save user to DB
		_, err := db.DB.Exec(`
			INSERT INTO users (id, email, name, updated_at) 
			VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
			ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name, updated_at = CURRENT_TIMESTAMP;
		`, user.ID, user.Email, user.Name)

		if err != nil {
			http.Error(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
			return
		}

		tokenStr, err := GenerateToken(user)
		if err != nil {
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
		http.Redirect(w, r, frontendURL+"/", http.StatusFound)
	}
}

func HandleCallback(w http.ResponseWriter, r *http.Request) {
	if !useOIDC {
		http.Error(w, "OIDC auth not enabled", http.StatusBadRequest)
		return
	}

	stateCookie, err := r.Cookie("oidc_state")
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}

	if r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return
	}

	// Exchange code for token
	oauth2Token, err := oauth2Config.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to exchange token: %v", err), http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token in response", http.StatusInternalServerError)
		return
	}

	idToken, err := oidcVerifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to verify id token: %v", err), http.StatusInternalServerError)
		return
	}

	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
	}

	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "failed to parse claims", http.StatusInternalServerError)
		return
	}

	user := User{
		ID:    claims.Subject,
		Email: claims.Email,
		Name:  claims.Name,
	}

	// Upsert user in database
	_, err = db.DB.Exec(`
		INSERT INTO users (id, email, name, updated_at) 
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name, updated_at = CURRENT_TIMESTAMP;
	`, user.ID, user.Email, user.Name)
	if err != nil {
		http.Error(w, "failed to save user info to database", http.StatusInternalServerError)
		return
	}

	// Generate session token
	tokenStr, err := GenerateToken(user)
	if err != nil {
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
	http.Redirect(w, r, frontendURL+"/", http.StatusFound)
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"authenticated":false}`)
		return
	}

	user, err := VerifyToken(cookie.Value)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"authenticated":false}`)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"authenticated":true,"user":{"id":"%s","email":"%s","name":"%s"}}`, user.ID, user.Email, user.Name)
}
