package main

import (
	"bytes"
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	defaultTargetServiceURL      = "http://demo-app-service:8081"
	defaultTargetServiceHTTPSURL = "https://demo-app-service:8443"
	proxyPort                    = "0.0.0.0:8080"
	tlsTestPrefix                = "/tls-test"
)

func main() {
	targetServiceURL := os.Getenv("TARGET_SERVICE_URL")
	if targetServiceURL == "" {
		targetServiceURL = defaultTargetServiceURL
	}

	targetServiceHTTPSURL := os.Getenv("TARGET_SERVICE_HTTPS_URL")
	if targetServiceHTTPSURL == "" {
		targetServiceHTTPSURL = defaultTargetServiceHTTPSURL
	}

	// Client for HTTPS target (self-signed cert)
	httpsClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if rest, ok := strings.CutPrefix(r.URL.Path, tlsTestPrefix); ok {
			// Forward to the HTTPS target with the prefix stripped
			r.URL.Path = rest
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
			proxyHandlerWithClient(w, r, targetServiceHTTPSURL, httpsClient)
		} else {
			proxyHandler(w, r, targetServiceURL)
		}
	})
	log.Printf("Auth proxy starting on port %s", proxyPort)
	log.Printf("Forwarding HTTP  requests to %s", targetServiceURL)
	log.Printf("Forwarding HTTPS requests (/tls-test) to %s", targetServiceHTTPSURL)
	log.Printf("JWT validation is handled by the inbound ext proc")
	log.Fatal(http.ListenAndServe(proxyPort, nil))
}

var defaultClient = &http.Client{}

func proxyHandler(w http.ResponseWriter, r *http.Request, targetServiceURL string) {
	proxyHandlerWithClient(w, r, targetServiceURL, defaultClient)
}

func proxyHandlerWithClient(w http.ResponseWriter, r *http.Request, targetServiceURL string, client *http.Client) {
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

	log.Printf("Forwarded %s %s -> %s - Status: %d", r.Method, r.URL.Path, targetServiceURL, resp.StatusCode)
}
