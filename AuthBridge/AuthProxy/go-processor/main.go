package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type processor struct {
	v3.UnimplementedExternalProcessorServer
}

type tokenExchangeResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

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
					log.Printf("%s: %s", header.Key, string(header.RawValue))
				}
			}

			// Check for token exchange environment variables in headers
			clientID := getHeaderValue(headers.Headers, "x-client-id")
			clientSecret := getHeaderValue(headers.Headers, "x-client-secret")
			tokenURL := getHeaderValue(headers.Headers, "x-token-url")
			targetAudience := getHeaderValue(headers.Headers, "x-target-audience")
			targetScopes := getHeaderValue(headers.Headers, "x-target-scopes")

			// If all 5 variables are present, perform token exchange
			if clientID != "" && clientSecret != "" && tokenURL != "" && targetAudience != "" && targetScopes != "" {
				log.Println("[Token Exchange] All required headers present, attempting token exchange")

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
							log.Printf("[Token Exchange] Replacing token in Authorization header")
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
				log.Println("[Token Exchange] Not all required headers present, skipping token exchange")
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
