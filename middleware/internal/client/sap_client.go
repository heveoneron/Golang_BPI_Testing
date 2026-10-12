package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

type SAPClient struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client
}

func NewSAPClient(baseURL, user, pass string) *SAPClient {
	jar, _ := cookiejar.New(nil)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxConnsPerHost = 100
	transport.MaxIdleConnsPerHost = 100

	return &SAPClient{
		BaseURL:  baseURL,
		Username: user,
		Password: pass,
		HTTP: &http.Client{
			Timeout:   15 * time.Second,
			Jar:       jar,
			Transport: transport,
		},
	}
}

// PostODataDocument mengeksekusi request OData dengan otomatis Fetch CSRF Token
func (s *SAPClient) PostODataDocument(endpointPath string, payloadJSON []byte) (string, error) {
	if s.BaseURL == "" || s.BaseURL == "MOCK" {
		// Mock Mode: Berguna saat testing di lokal tanpa server SAP aktif
		mockResponse := map[string]interface{}{
			"d": map[string]interface{}{
				"PurchaseOrder":    fmt.Sprintf("SAP-PO-%d", time.Now().Unix()),
				"MaterialDocument": fmt.Sprintf("SAP-DOC-%d", time.Now().Unix()),
				"Status":           "Created",
			},
		}
		bytes, _ := json.Marshal(mockResponse)
		return string(bytes), nil
	}

	fullURL := s.BaseURL + endpointPath

	// 1. Fetch CSRF Token
	reqGet, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed create csrf request: %w", err)
	}
	reqGet.SetBasicAuth(s.Username, s.Password)
	reqGet.Header.Set("x-csrf-token", "fetch")

	respGet, err := s.HTTP.Do(reqGet)
	if err != nil {
		return "", fmt.Errorf("network error fetching csrf token: %w", err)
	}
	defer respGet.Body.Close()

	csrfToken := respGet.Header.Get("x-csrf-token")
	if csrfToken == "" {
		csrfToken = "test-token" // Fallback jika SAP sandbox tidak mewajibkan CSRF
	}

	// 2. POST OData Data
	reqPost, err := http.NewRequest("POST", fullURL, strings.NewReader(string(payloadJSON)))
	if err != nil {
		return "", fmt.Errorf("failed create post request: %w", err)
	}
	reqPost.SetBasicAuth(s.Username, s.Password)
	reqPost.Header.Set("Content-Type", "application/json")
	reqPost.Header.Set("Accept", "application/json")
	reqPost.Header.Set("x-csrf-token", csrfToken)

	respPost, err := s.HTTP.Do(reqPost)
	if err != nil {
		return "", fmt.Errorf("network error POST to SAP Grow: %w", err)
	}
	defer respPost.Body.Close()

	bodyBytes, err := io.ReadAll(respPost.Body)
	if err != nil {
		return "", fmt.Errorf("failed reading response body: %w", err)
	}

	if respPost.StatusCode != http.StatusCreated && respPost.StatusCode != http.StatusOK {
		return string(bodyBytes), fmt.Errorf("SAP Error status %d: %s", respPost.StatusCode, string(bodyBytes))
	}

	return string(bodyBytes), nil
}
