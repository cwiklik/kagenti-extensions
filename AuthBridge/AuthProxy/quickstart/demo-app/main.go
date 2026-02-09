package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const (
	httpPort  = "0.0.0.0:8081"
	httpsPort = "0.0.0.0:8443"
)

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

	// HTTP server on port 8081 with JWT validation
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r, jwksURL, issuer, audience)
	})

	// HTTPS server on port 8443 — simple echo, no JWT validation.
	// This port is used to verify TLS passthrough through Envoy works.
	httpsMux := http.NewServeMux()
	httpsMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("tls-ok"))
		log.Printf("HTTPS request served: %s %s", r.Method, r.URL.Path)
	})

	tlsCert, err := generateSelfSignedCert()
	if err != nil {
		log.Fatalf("Failed to generate self-signed TLS certificate: %v", err)
	}

	httpsServer := &http.Server{
		Addr:    httpsPort,
		Handler: httpsMux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		},
	}

	log.Printf("Demo app HTTP  starting on %s (JWT validation enabled)", httpPort)
	log.Printf("Demo app HTTPS starting on %s (echo only, no JWT validation)", httpsPort)
	log.Printf("JWKS URL: %s", jwksURL)
	log.Printf("Expected issuer: %s", issuer)
	log.Printf("Expected audience: %s", audience)

	// Start HTTPS listener in a goroutine
	go func() {
		// TLSConfig already has the cert; pass empty strings to use it
		if err := httpsServer.ListenAndServeTLS("", ""); err != nil {
			log.Fatalf("HTTPS server failed: %v", err)
		}
	}()

	log.Fatal(http.ListenAndServe(httpPort, httpMux))
}

// generateSelfSignedCert creates an in-memory self-signed TLS certificate.
func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate serial: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "demo-app"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"demo-app-service", "localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, nil
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
	log.Printf("[JWT Debug] Subject: %s", token.Subject())
	log.Printf("[JWT Debug] Audience: %v", audiences)

	// Extract and log preferred_username if present (shows the actual username)
	if preferredUsername, ok := token.Get("preferred_username"); ok {
		log.Printf("[JWT Debug] Preferred Username: %v", preferredUsername)
	}

	// Extract and log azp (authorized party) if present
	if azp, ok := token.Get("azp"); ok {
		log.Printf("[JWT Debug] Authorized Party (azp): %v", azp)
	}

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
