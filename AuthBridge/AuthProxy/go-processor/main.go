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

// loadConfig loads configuration from environment variables
// Falls back to files if env vars are not set (for dynamic credentials)
func loadConfig() {
	globalConfig.mu.Lock()
	defer globalConfig.mu.Unlock()

	// Prioritize environment variables (for static agent credentials)
	if envClientID := os.Getenv("CLIENT_ID"); envClientID != "" {
		globalConfig.ClientID = envClientID
		log.Printf("[Config] Using CLIENT_ID from environment variable")
	} else {
		// Fall back to file (for dynamic credentials from client-registration)
		clientIDFile := os.Getenv("CLIENT_ID_FILE")
		if clientIDFile == "" {
			clientIDFile = "/shared/client-id.txt"
		}
		if clientID, err := readFileContent(clientIDFile); err == nil && clientID != "" {
			globalConfig.ClientID = clientID
			log.Printf("[Config] Loaded CLIENT_ID from file: %s", clientIDFile)
		}
	}

	// Prioritize environment variables
	if envClientSecret := os.Getenv("CLIENT_SECRET"); envClientSecret != "" {
		globalConfig.ClientSecret = envClientSecret
		log.Printf("[Config] Using CLIENT_SECRET from environment variable")
	} else {
		// Fall back to file
		clientSecretFile := os.Getenv("CLIENT_SECRET_FILE")
		if clientSecretFile == "" {
			clientSecretFile = "/shared/client-secret.txt"
		}
		if clientSecret, err := readFileContent(clientSecretFile); err == nil && clientSecret != "" {
			globalConfig.ClientSecret = clientSecret
			log.Printf("[Config] Loaded CLIENT_SECRET from file: %s", clientSecretFile)
		}
	}

	// These are typically static and come from env vars
	globalConfig.TokenURL = os.Getenv("TOKEN_URL")
	globalConfig.TargetAudience = os.Getenv("TARGET_AUDIENCE")
	globalConfig.TargetScopes = os.Getenv("TARGET_SCOPES")

	log.Printf("[Config] Configuration loaded:")
	log.Printf("[Config]   CLIENT_ID: %s", globalConfig.ClientID)
	log.Printf("[Config]   CLIENT_SECRET: [REDACTED, length=%d]", len(globalConfig.ClientSecret))
	log.Printf("[Config]   TOKEN_URL: %s", globalConfig.TokenURL)
	log.Printf("[Config]   TARGET_AUDIENCE: %s", globalConfig.TargetAudience)
	log.Printf("[Config]   TARGET_SCOPES: %s", globalConfig.TargetScopes)
}

// getConfig returns the current configuration
func getConfig() (clientID, clientSecret, tokenURL, targetAudience, targetScopes string) {
	globalConfig.mu.RLock()
	defer globalConfig.mu.RUnlock()
	return globalConfig.ClientID, globalConfig.ClientSecret, globalConfig.TokenURL, globalConfig.TargetAudience, globalConfig.TargetScopes
}

// getActorToken obtains a token for the agent client itself using client_credentials grant.
// This token is used as the actor_token in token exchange, proving agent's identity.
func getActorToken(clientID, clientSecret, tokenURL string) (string, error) {
	log.Printf("[Actor Token] Getting actor token for %s", clientID)

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("grant_type", "client_credentials")

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		log.Printf("[Actor Token] Failed to make request: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[Actor Token] Failed to read response: %v", err)
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Actor Token] Failed with status %d: %s", resp.StatusCode, string(body))
		return "", status.Errorf(codes.Internal, "actor token request failed: %s", string(body))
	}

	var tokenResp tokenExchangeResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		log.Printf("[Actor Token] Failed to parse response: %v", err)
		return "", err
	}

	log.Printf("[Actor Token] Successfully obtained actor token")
	return tokenResp.AccessToken, nil
}

// exchangeToken performs OAuth 2.0 Token Exchange (RFC 8693) with actor token support.
// The actor token proves that agent is authorized to perform the exchange,
// allowing it to exchange tokens that don't have agent in their audience.
func exchangeToken(clientID, clientSecret, tokenURL, subjectToken, audience, scopes string) (string, error) {
	log.Printf("[Token Exchange] Starting token exchange with actor token")
	log.Printf("[Token Exchange] Token URL: %s", tokenURL)
	log.Printf("[Token Exchange] Client ID: %s", clientID)
	log.Printf("[Token Exchange] Audience: %s", audience)
	log.Printf("[Token Exchange] Scopes: %s", scopes)

	// Step 1: Get actor token for agent
	actorToken, err := getActorToken(clientID, clientSecret, tokenURL)
	if err != nil {
		log.Printf("[Token Exchange] Failed to get actor token: %v", err)
		// Fall back to exchange without actor token
		log.Printf("[Token Exchange] Falling back to exchange without actor token")
		return exchangeTokenWithoutActor(clientID, clientSecret, tokenURL, subjectToken, audience, scopes)
	}

	// Step 2: Perform token exchange with actor token
	log.Printf("[Token Exchange] Performing token exchange with actor token")
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("requested_token_type", "urn:ietf:params:oauth:token-type:access_token")
	data.Set("subject_token", subjectToken)
	data.Set("subject_token_type", "urn:ietf:params:oauth:token-type:access_token")
	data.Set("actor_token", actorToken)
	data.Set("actor_token_type", "urn:ietf:params:oauth:token-type:access_token")
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

	log.Printf("[Token Exchange] Successfully exchanged token with actor token")
	return tokenResp.AccessToken, nil
}

// exchangeTokenWithoutActor performs token exchange without actor token (original behavior).
// Used as a fallback if actor token cannot be obtained.
func exchangeTokenWithoutActor(clientID, clientSecret, tokenURL, subjectToken, audience, scopes string) (string, error) {
	log.Printf("[Token Exchange] Performing token exchange without actor token")

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

	// Load configuration from environment variables (or files as fallback)
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
