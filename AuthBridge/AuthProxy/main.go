package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const (
	defaultTargetServiceURL = "http://auth-target-service:8081"
	proxyPort               = "0.0.0.0:8080"
)

var jwksCache *jwk.Cache

func main() {
	targetServiceURL := os.Getenv("TARGET_SERVICE_URL")
	if targetServiceURL == "" {
		targetServiceURL = defaultTargetServiceURL
	}

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
		proxyHandler(w, r, targetServiceURL, jwksURL, issuer, audience)
	})
	log.Printf("Auth proxy starting on port %s", proxyPort)
	log.Printf("JWKS URL: %s", jwksURL)
	log.Printf("Expected issuer: %s", issuer)
	log.Printf("Expected audience: %s", audience)
	log.Printf("Forwarding authorized requests to %s", targetServiceURL)
	log.Fatal(http.ListenAndServe(proxyPort, nil))
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

func proxyHandler(w http.ResponseWriter, r *http.Request, targetServiceURL, jwksURL, issuer, audience string) {
	// Extract and validate JWT token
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
		log.Printf("Unauthorized request (missing auth header): %s %s", r.Method, r.URL.Path)
		return
	}

	// Extract token from "Bearer <token>" format
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
		log.Printf("Unauthorized request (invalid auth format): %s %s", r.Method, r.URL.Path)
		return
	}

	// Validate JWT
	if err := validateJWT(tokenString, jwksURL, issuer, audience); err != nil {
		http.Error(w, fmt.Sprintf("Invalid token: %v", err), http.StatusUnauthorized)
		log.Printf("Unauthorized request (invalid token): %s %s - %v", r.Method, r.URL.Path, err)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	targetURL, err := url.Parse(targetServiceURL + r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid target URL", http.StatusInternalServerError)
		return
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
		return
	}

	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Failed to forward request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	w.Write(respBody)

	log.Printf("Forwarded %s %s - Status: %d", r.Method, r.URL.Path, resp.StatusCode)
}