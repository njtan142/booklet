package auth

import (
	"context"
	"crypto/rand"
	"log"
	"os"
	"time"

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
