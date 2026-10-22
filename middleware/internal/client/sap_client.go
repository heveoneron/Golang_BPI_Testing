package client

import (
	"encoding/json"
	"fmt"
	"golang-bpi-testing/internal/model"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"
)

type SAPClient struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client
	mu       sync.Mutex
	tokenMap map[string]string // key: username@url -> csrfToken
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
			Timeout:   25 * time.Second,
			Jar:       jar,
			Transport: transport,
		},
		tokenMap: make(map[string]string),
	}
}

// TestConnection menguji konektivitas & token CSRF handshake ke endpoint SAP spesifik
func (s *SAPClient) TestConnection(ep *model.SAPEndpoint) (string, int64, error) {
	start := time.Now()

	if ep.BaseURL == "" || ep.BaseURL == "MOCK" || s.BaseURL == "MOCK" {
		latency := time.Since(start).Milliseconds() + 35
		return "mock-csrf-token-handshake-ok", latency, nil
	}

	targetURL := ep.BaseURL
	if ep.CSRFPath != "" {
		targetURL += ep.CSRFPath
	} else if ep.EndpointPath != "" {
		targetURL += ep.EndpointPath
	}

	reqGet, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed create request: %w", err)
	}

	username := ep.Username
	if username == "" {
		username = s.Username
	}
	password := ep.Password
	if password == "" {
		password = s.Password
	}

	reqGet.SetBasicAuth(username, password)
	reqGet.Header.Set("x-csrf-token", "fetch")
	reqGet.Header.Set("Accept", "application/json")

	resp, err := s.HTTP.Do(reqGet)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return "", latency, fmt.Errorf("network connection error: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	csrfToken := resp.Header.Get("x-csrf-token")

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return csrfToken, latency, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if csrfToken == "" {
		csrfToken = "test-token"
	}

	return csrfToken, latency, nil
}

// ExecuteOData mengeksekusi request OData (v2 atau v4) dengan otomatis Fetch CSRF Token & Basic Auth per endpoint
func (s *SAPClient) ExecuteOData(ep *model.SAPEndpoint, method string, payloadJSON []byte) (int, string, string, error) {
	if ep.BaseURL == "" || ep.BaseURL == "MOCK" || s.BaseURL == "MOCK" {
		docNo := fmt.Sprintf("SAP-%s-%d", ep.ServiceCode, time.Now().Unix())
		mockResp := map[string]interface{}{
			"Service":          ep.ServiceName,
			"ODataVersion":     ep.ODataVersion,
			"PurchaseOrder":    docNo,
			"BusinessPartner":  docNo,
			"MaterialDocument": docNo,
			"Status":           "Created",
			"Timestamp":        time.Now().Format(time.RFC3339),
			"d": map[string]interface{}{
				"PurchaseOrder":    docNo,
				"BusinessPartner":  docNo,
				"MaterialDocument": docNo,
				"Status":           "Created",
			},
		}
		bytes, _ := json.Marshal(mockResp)
		return http.StatusCreated, docNo, string(bytes), nil
	}

	fullURL := ep.BaseURL + ep.EndpointPath
	csrfURL := fullURL
	if ep.CSRFPath != "" {
		csrfURL = ep.BaseURL + ep.CSRFPath
	}

	username := ep.Username
	if username == "" {
		username = s.Username
	}
	password := ep.Password
	if password == "" {
		password = s.Password
	}

	// 1. Fetch CSRF Token & Session Cookies
	reqGet, err := http.NewRequest("GET", csrfURL, nil)
	if err != nil {
		return 0, "", "", fmt.Errorf("failed create csrf request: %w", err)
	}
	reqGet.SetBasicAuth(username, password)
	reqGet.Header.Set("x-csrf-token", "fetch")
	reqGet.Header.Set("Accept", "application/json")

	respGet, err := s.HTTP.Do(reqGet)
	if err != nil {
		return 0, "", "", fmt.Errorf("network error fetching csrf token: %w", err)
	}
	defer respGet.Body.Close()

	csrfToken := respGet.Header.Get("x-csrf-token")
	if csrfToken == "" {
		csrfToken = "test-token"
	}

	// 2. Kirim Request Utama (POST / GET)
	var reqBody io.Reader
	if len(payloadJSON) > 0 {
		reqBody = strings.NewReader(string(payloadJSON))
	}

	reqMain, err := http.NewRequest(method, fullURL, reqBody)
	if err != nil {
		return 0, "", "", fmt.Errorf("failed create %s request: %w", method, err)
	}
	reqMain.SetBasicAuth(username, password)
	reqMain.Header.Set("Content-Type", "application/json")
	reqMain.Header.Set("Accept", "application/json")
	if method != "GET" {
		reqMain.Header.Set("x-csrf-token", csrfToken)
	}

	respMain, err := s.HTTP.Do(reqMain)
	if err != nil {
		return 0, "", "", fmt.Errorf("network error executing %s to SAP Grow: %w", method, err)
	}
	defer respMain.Body.Close()

	bodyBytes, err := io.ReadAll(respMain.Body)
	if err != nil {
		return respMain.StatusCode, "", "", fmt.Errorf("failed reading response body: %w", err)
	}
	respStr := string(bodyBytes)

	// 3. Ekstrak Document Number berdasarkan OData Version
	sapDocNo := fmt.Sprintf("SAP-%s-%d", ep.ServiceCode, time.Now().Unix())
	var respMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &respMap); err == nil {
		if ep.ODataVersion == "v2" {
			// OData v2 membungkus response di dalam objek "d"
			if d, ok := respMap["d"].(map[string]interface{}); ok {
				if bp, exists := d["BusinessPartner"].(string); exists && bp != "" {
					sapDocNo = bp
				} else if po, exists := d["PurchaseOrder"].(string); exists && po != "" {
					sapDocNo = po
				} else if mat, exists := d["MaterialDocument"].(string); exists && mat != "" {
					sapDocNo = mat
				}
			}
		} else {
			// OData v4 mengembalikan root JSON object langsung
			if po, exists := respMap["PurchaseOrder"].(string); exists && po != "" {
				sapDocNo = po
			} else if bp, exists := respMap["BusinessPartner"].(string); exists && bp != "" {
				sapDocNo = bp
			} else if id, exists := respMap["id"].(string); exists && id != "" {
				sapDocNo = id
			}
		}
	}

	if respMain.StatusCode != http.StatusCreated && respMain.StatusCode != http.StatusOK {
		return respMain.StatusCode, sapDocNo, respStr, fmt.Errorf("SAP Error status %d: %s", respMain.StatusCode, respStr)
	}

	return respMain.StatusCode, sapDocNo, respStr, nil
}

// PostODataDocument (Legacy fallback) mengeksekusi request OData dengan otomatis Fetch CSRF Token
func (s *SAPClient) PostODataDocument(endpointPath string, payloadJSON []byte) (string, error) {
	ep := &model.SAPEndpoint{
		ServiceCode:  "PO",
		ServiceName:  "Legacy PO",
		ODataVersion: "v4",
		BaseURL:      s.BaseURL,
		EndpointPath: endpointPath,
		Username:     s.Username,
		Password:     s.Password,
	}
	_, _, body, err := s.ExecuteOData(ep, "POST", payloadJSON)
	return body, err
}
