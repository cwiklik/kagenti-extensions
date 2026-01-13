package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Configuration for token exchange
type Config struct {
	ClientID       string
	ClientSecret   string
	TokenURL       string
	TargetAudience string
	TargetScopes   string
	mu             sync.RWMutex
}

var globalConfig = &Config{}

type processor struct {
	v3.UnimplementedExternalProcessorServer
}

type tokenExchangeResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// readFileContent reads the content of a file, trimming whitespace
func readFileContent(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

// loadConfig loads configuration from environment variables or files.
// For dynamic credentials from client-registration, it reads from /shared/ files.
// Retries loading credentials from files if they're not immediately available.
func loadConfig() {
	globalConfig.mu.Lock()
	defer globalConfig.mu.Unlock()

	// Static configuration from environment variables
	globalConfig.TokenURL = os.Getenv("TOKEN_URL")
	globalConfig.TargetAudience = os.Getenv("TARGET_AUDIENCE")
	globalConfig.TargetScopes = os.Getenv("TARGET_SCOPES")

	// For CLIENT_ID and CLIENT_SECRET, prefer files from /shared/ (dynamic credentials)
	// This allows AuthProxy to use the same credentials as the auto-registered client
	clientIDFile := os.Getenv("CLIENT_ID_FILE")
	if clientIDFile == "" {
		clientIDFile = "/shared/client-id.txt"
	}
	clientSecretFile := os.Getenv("CLIENT_SECRET_FILE")
	if clientSecretFile == "" {
		clientSecretFile = "/shared/client-secret.txt"
	}

	// Try to load from files first (preferred for SPIFFE-based dynamic credentials)
	if clientID, err := readFileContent(clientIDFile); err == nil && clientID != "" {
		globalConfig.ClientID = clientID
		log.Printf("[Config] Loaded CLIENT_ID from file: %s", clientIDFile)
	} else if envClientID := os.Getenv("CLIENT_ID"); envClientID != "" {
		// Fall back to environment variable
		globalConfig.ClientID = envClientID
		log.Printf("[Config] Using CLIENT_ID from environment variable")
	}

	if clientSecret, err := readFileContent(clientSecretFile); err == nil && clientSecret != "" {
		globalConfig.ClientSecret = clientSecret
		log.Printf("[Config] Loaded CLIENT_SECRET from file: %s", clientSecretFile)
	} else if envClientSecret := os.Getenv("CLIENT_SECRET"); envClientSecret != "" {
		// Fall back to environment variable
		globalConfig.ClientSecret = envClientSecret
		log.Printf("[Config] Using CLIENT_SECRET from environment variable")
	}

	log.Printf("[Config] Configuration loaded:")
	log.Printf("[Config]   CLIENT_ID: %s", globalConfig.ClientID)
	log.Printf("[Config]   CLIENT_SECRET: [REDACTED, length=%d]", len(globalConfig.ClientSecret))
	log.Printf("[Config]   TOKEN_URL: %s", globalConfig.TokenURL)
	log.Printf("[Config]   TARGET_AUDIENCE: %s", globalConfig.TargetAudience)
	log.Printf("[Config]   TARGET_SCOPES: %s", globalConfig.TargetScopes)
}

// waitForCredentials waits for credential files to be available
// This handles the case where client-registration hasn't finished yet
func waitForCredentials(maxWait time.Duration) bool {
	clientIDFile := os.Getenv("CLIENT_ID_FILE")
	if clientIDFile == "" {
		clientIDFile = "/shared/client-id.txt"
	}
	clientSecretFile := os.Getenv("CLIENT_SECRET_FILE")
	if clientSecretFile == "" {
		clientSecretFile = "/shared/client-secret.txt"
	}

	log.Printf("[Config] Waiting for credential files (max %v)...", maxWait)
	deadline := time.Now().Add(maxWait)
	
	for time.Now().Before(deadline) {
		// Check if both files exist and have content
		clientID, err1 := readFileContent(clientIDFile)
		clientSecret, err2 := readFileContent(clientSecretFile)
		
		if err1 == nil && err2 == nil && clientID != "" && clientSecret != "" {
			log.Printf("[Config] Credential files are ready")
			return true
		}
		
		log.Printf("[Config] Credentials not ready yet, waiting...")
		time.Sleep(2 * time.Second)
	}
	
	log.Printf("[Config] Timeout waiting for credentials, will use environment variables if available")
	return false
}

// getConfig returns the current configuration
func getConfig() (clientID, clientSecret, tokenURL, targetAudience, targetScopes string) {
	globalConfig.mu.RLock()
	defer globalConfig.mu.RUnlock()
	return globalConfig.ClientID, globalConfig.ClientSecret, globalConfig.TokenURL, globalConfig.TargetAudience, globalConfig.TargetScopes
}

// exchangeToken performs OAuth 2.0 Token Exchange (RFC 8693).
// Exchanges the subject token for a new token with the specified audience.
// Requires the exchanging client to be in the subject token's audience.
// When using dynamic credentials from /shared/, this works because the token's
// audience matches the auto-registered client's SPIFFE ID.
func exchangeToken(clientID, clientSecret, tokenURL, subjectToken, audience, scopes string) (string, error) {
	log.Printf("[Token Exchange] Starting token exchange")
	log.Printf("[Token Exchange] Token URL: %s", tokenURL)
	log.Printf("[Token Exchange] Client ID: %s", clientID)
	log.Printf("[Token Exchange] Audience: %s", audience)
	log.Printf("[Token Exchange] Scopes: %s", scopes)

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("requested_token_type", "urn:ietf:params:oauth:token-type:access_token")
	data.Set("subject_token", subjectToken)
	data.Set("subject_token_type", "urn:ietf:params:oauth:token-type:access_token")
	data.Set("audience", audience)
	data.Set("scope", scopes)

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		log.Printf("[Token Exchange] Failed to make request: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[Token Exchange] Failed to read response: %v", err)
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Token Exchange] Failed with status %d: %s", resp.StatusCode, string(body))
		return "", status.Errorf(codes.Internal, "token exchange failed: %s", string(body))
	}

	var tokenResp tokenExchangeResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		log.Printf("[Token Exchange] Failed to parse response: %v", err)
		return "", err
	}

	log.Printf("[Token Exchange] Successfully exchanged token")
	return tokenResp.AccessToken, nil
}

func getHeaderValue(headers []*core.HeaderValue, key string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Key, key) {
			return string(header.RawValue)
		}
	}
	return ""
}

func (p *processor) Process(stream v3.ExternalProcessor_ProcessServer) error {
	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := stream.Recv()
		if err != nil {
			return status.Errorf(codes.Unknown, "cannot receive stream request: %v", err)
		}

		resp := &v3.ProcessingResponse{}

		switch r := req.Request.(type) {
		case *v3.ProcessingRequest_RequestHeaders:
			log.Println("=== Request Headers ===")
			headers := r.RequestHeaders.Headers
			if headers != nil {
				for _, header := range headers.Headers {
					// Don't log sensitive headers
					if !strings.EqualFold(header.Key, "authorization") &&
						!strings.EqualFold(header.Key, "x-client-secret") {
						log.Printf("%s: %s", header.Key, string(header.RawValue))
					}
				}
			}

			// Get configuration (from files or env vars)
			clientID, clientSecret, tokenURL, targetAudience, targetScopes := getConfig()

			// Check if we have all required config
			if clientID != "" && clientSecret != "" && tokenURL != "" && targetAudience != "" && targetScopes != "" {
				log.Println("[Token Exchange] Configuration loaded, attempting token exchange")
				log.Printf("[Token Exchange] Client ID: %s", clientID)
				log.Printf("[Token Exchange] Target Audience: %s", targetAudience)
				log.Printf("[Token Exchange] Target Scopes: %s", targetScopes)

				// Extract current JWT from Authorization header
				authHeader := getHeaderValue(headers.Headers, "authorization")
				if authHeader != "" {
					// Extract token from "Bearer <token>" format
					subjectToken := strings.TrimPrefix(authHeader, "Bearer ")
					subjectToken = strings.TrimPrefix(subjectToken, "bearer ")

					if subjectToken != authHeader {
						// Perform token exchange
						newToken, err := exchangeToken(clientID, clientSecret, tokenURL, subjectToken, targetAudience, targetScopes)
						if err == nil {
							log.Printf("[Token Exchange] Successfully exchanged token, replacing Authorization header")
							// Create header mutation to replace the Authorization header
							resp = &v3.ProcessingResponse{
								Response: &v3.ProcessingResponse_RequestHeaders{
									RequestHeaders: &v3.HeadersResponse{
										Response: &v3.CommonResponse{
											HeaderMutation: &v3.HeaderMutation{
												SetHeaders: []*core.HeaderValueOption{
													{
														Header: &core.HeaderValue{
															Key:      "authorization",
															RawValue: []byte("Bearer " + newToken),
														},
													},
												},
											},
										},
									},
								},
							}
						} else {
							log.Printf("[Token Exchange] Failed to exchange token: %v", err)
							resp = &v3.ProcessingResponse{
								Response: &v3.ProcessingResponse_RequestHeaders{
									RequestHeaders: &v3.HeadersResponse{},
								},
							}
						}
					} else {
						log.Printf("[Token Exchange] Invalid Authorization header format")
						resp = &v3.ProcessingResponse{
							Response: &v3.ProcessingResponse_RequestHeaders{
								RequestHeaders: &v3.HeadersResponse{},
							},
						}
					}
				} else {
					log.Printf("[Token Exchange] No Authorization header found")
					resp = &v3.ProcessingResponse{
						Response: &v3.ProcessingResponse_RequestHeaders{
							RequestHeaders: &v3.HeadersResponse{},
						},
					}
				}
			} else {
				log.Println("[Token Exchange] Missing configuration, skipping token exchange")
				log.Printf("[Token Exchange] CLIENT_ID present: %v, CLIENT_SECRET present: %v, TOKEN_URL present: %v",
					clientID != "", clientSecret != "", tokenURL != "")
				log.Printf("[Token Exchange] TARGET_AUDIENCE present: %v, TARGET_SCOPES present: %v",
					targetAudience != "", targetScopes != "")
				resp = &v3.ProcessingResponse{
					Response: &v3.ProcessingResponse_RequestHeaders{
						RequestHeaders: &v3.HeadersResponse{},
					},
				}
			}

		case *v3.ProcessingRequest_ResponseHeaders:
			log.Println("=== Response Headers ===")
			headers := r.ResponseHeaders.Headers
			if headers != nil {
				for _, header := range headers.Headers {
					log.Printf("%s: %s", header.Key, string(header.RawValue))
				}
			}
			resp = &v3.ProcessingResponse{
				Response: &v3.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: &v3.HeadersResponse{},
				},
			}

		default:
			log.Printf("Unknown request type: %T\n", r)
		}

		if err := stream.Send(resp); err != nil {
			return status.Errorf(codes.Unknown, "cannot send stream response: %v", err)
		}
	}
}

func main() {
	log.Println("=== Go External Processor Starting ===")

	// Wait for credential files from client-registration (up to 60 seconds)
	// This handles the startup race condition with client-registration container
	waitForCredentials(60 * time.Second)

	// Load configuration from files (or environment variables as fallback)
	loadConfig()

	// Start gRPC server
	port := ":9090"
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	v3.RegisterExternalProcessorServer(grpcServer, &processor{})

	log.Printf("Starting Go external processor on %s", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
