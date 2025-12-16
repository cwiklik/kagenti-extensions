package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const targetPort = "0.0.0.0:8081"

var jwksCache *jwk.Cache

func main() {
	jwksURL := os.Getenv("JWKS_URL")
	if jwksURL == "" {
		log.Fatal("JWKS_URL environment variable is required")
	}

	issuer := os.Getenv("ISSUER")
	if issuer == "" {
		log.Fatal("ISSUER environment variable is required")
	}

	audience := os.Getenv("AUDIENCE")
	if audience == "" {
		log.Fatal("AUDIENCE environment variable is required")
	}

	// Initialize JWKS cache
	ctx := context.Background()
	jwksCache = jwk.NewCache(ctx)
	if err := jwksCache.Register(jwksURL); err != nil {
		log.Fatalf("Failed to register JWKS URL: %v", err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r, jwksURL, issuer, audience)
	})
	log.Printf("Target service starting on port %s", targetPort)
	log.Printf("JWKS URL: %s", jwksURL)
	log.Printf("Expected issuer: %s", issuer)
	log.Printf("Expected audience: %s", audience)
	log.Fatal(http.ListenAndServe(targetPort, nil))
}

func validateJWT(tokenString, jwksURL, expectedIssuer, expectedAudience string) error {
	ctx := context.Background()

	// Fetch JWKS from cache
	keySet, err := jwksCache.Get(ctx, jwksURL)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	// Parse and validate the token
	token, err := jwt.Parse([]byte(tokenString), jwt.WithKeySet(keySet), jwt.WithValidate(true))
	if err != nil {
		return fmt.Errorf("failed to parse/validate token: %w", err)
	}

	// Validate issuer claim
	if token.Issuer() != expectedIssuer {
		return fmt.Errorf("invalid issuer: expected %s, got %s", expectedIssuer, token.Issuer())
	}

	// Validate audience claim
	audiences := token.Audience()
	validAudience := false
	for _, aud := range audiences {
		if aud == expectedAudience {
			validAudience = true
			break
		}
	}
	if !validAudience {
		return fmt.Errorf("invalid audience: expected %s, got %v", expectedAudience, audiences)
	}

	// Log JWT claims for debugging
	log.Printf("[JWT Debug] Successfully validated token")
	log.Printf("[JWT Debug] Issuer: %s", token.Issuer())
	log.Printf("[JWT Debug] Audience: %v", audiences)

	// Extract and log scope claim if present
	if scopeClaim, ok := token.Get("scope"); ok {
		log.Printf("[JWT Debug] Scope: %v", scopeClaim)
	} else {
		log.Printf("[JWT Debug] Scope: <not present>")
	}

	return nil
}

func authHandler(w http.ResponseWriter, r *http.Request, jwksURL, issuer, audience string) {
	authHeader := r.Header.Get("Authorization")

	if authHeader == "" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized: missing Authorization header"))
		log.Printf("Unauthorized request (missing auth header): %s %s", r.Method, r.URL.Path)
		return
	}

	// Extract token from "Bearer <token>" format
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized: invalid Authorization header format"))
		log.Printf("Unauthorized request (invalid auth format): %s %s", r.Method, r.URL.Path)
		return
	}

	// Validate JWT
	if err := validateJWT(tokenString, jwksURL, issuer, audience); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
		log.Printf("Unauthorized request (invalid token): %s %s - %v", r.Method, r.URL.Path, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("authorized"))
	log.Printf("Authorized request: %s %s", r.Method, r.URL.Path)
}