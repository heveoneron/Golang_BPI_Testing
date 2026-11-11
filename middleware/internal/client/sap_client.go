package client

import (
	"encoding/json"
	"fmt"
	"golang-bpi-testing/internal/model"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
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
	clients  map[string]*http.Client
}

// httpFor mengembalikan http.Client dengan cookie jar terpisah per technical user. Token CSRF SAP
// terikat ke session cookie, sehingga beberapa communication user pada host yang sama tidak boleh
// berbagi satu jar.
func (s *SAPClient) httpFor(username string) *http.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: 25 * time.Second}
	}
	if s.clients == nil {
		s.clients = make(map[string]*http.Client)
	}
	if c, ok := s.clients[username]; ok {
		return c
	}
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Timeout: s.HTTP.Timeout, Transport: s.HTTP.Transport, Jar: jar}
	s.clients[username] = c
	return c
}

// credentials memilih kredensial endpoint, atau default client bila kosong.
func (s *SAPClient) credentials(ep *model.SAPEndpoint) (string, string) {
	username := ep.Username
	if username == "" {
		username = s.Username
	}
	password := ep.Password
	if password == "" {
		password = s.Password
	}
	return username, password
}

// IsMock bernilai true bila endpoint diarahkan ke mode simulasi.
func IsMock(ep *model.SAPEndpoint) bool {
	return ep == nil || ep.BaseURL == "" || strings.EqualFold(ep.BaseURL, "MOCK")
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

	if ep.BaseURL == "" || strings.EqualFold(ep.BaseURL, "MOCK") {
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

	resp, err := s.httpFor(username).Do(reqGet)
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
	if ep.BaseURL == "" || strings.EqualFold(ep.BaseURL, "MOCK") {
		docNo := fmt.Sprintf("SAP-%s-%d", ep.ServiceCode, time.Now().Unix())
		mockResp := map[string]interface{}{
			"Service":          ep.ServiceName,
			"ODataVersion":     ep.ODataVersion,
			"PurchaseOrder":    docNo,
			"BusinessPartner":  docNo,
			"MaterialDocument": docNo,
			"Product":          docNo,
			"Status":           "Created",
			"Timestamp":        time.Now().Format(time.RFC3339),
			"d": map[string]interface{}{
				"PurchaseOrder":    docNo,
				"BusinessPartner":  docNo,
				"MaterialDocument": docNo,
				"Product":          docNo,
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

	var csrfToken string
	if method != "GET" {
		// 1. Fetch CSRF Token & Session Cookies
		reqGet, err := http.NewRequest("GET", csrfURL, nil)
		if err != nil {
			return 0, "", "", fmt.Errorf("failed create csrf request: %w", err)
		}
		reqGet.SetBasicAuth(username, password)
		reqGet.Header.Set("x-csrf-token", "fetch")
		reqGet.Header.Set("Accept", "application/json")

		respGet, err := s.httpFor(username).Do(reqGet)
		if err != nil {
			return 0, "", "", fmt.Errorf("network error fetching csrf token: %w", err)
		}
		defer respGet.Body.Close()
		if respGet.StatusCode == http.StatusUnauthorized || respGet.StatusCode == http.StatusForbidden {
			body, _ := io.ReadAll(io.LimitReader(respGet.Body, 2000))
			return respGet.StatusCode, "", string(body), fmt.Errorf("SAP menolak kredensial saat fetch CSRF token (HTTP %d) untuk user %s", respGet.StatusCode, username)
		}

		csrfToken = respGet.Header.Get("x-csrf-token")
		if csrfToken == "" {
			csrfToken = "test-token"
		}
	}

	// 2. Kirim Request Utama (POST / GET / PATCH)
	var reqBody io.Reader
	if len(payloadJSON) > 0 {
		reqBody = strings.NewReader(string(payloadJSON))
	} else if method == "POST" || method == "PATCH" || method == "PUT" {
		reqBody = strings.NewReader("")
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

	respMain, err := s.httpFor(username).Do(reqMain)
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
	// Kosong bila SAP tidak mengembalikan nomor dokumen (mis. PATCH 204). Nomor sintetis tidak dibuat
	// karena tersimpan sebagai sap_document_no dan dipakai sebagai referensi Return/Cancel.
	sapDocNo := ""
	var respMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &respMap); err == nil {
		// Kandidat field nomor dokumen: SAP_RESPONSE_DOCNO_FIELDS_V2 / SAP_RESPONSE_DOCNO_FIELDS_V4.
		if ep.ODataVersion == "v2" {
			// OData v2 membungkus response di dalam objek "d"
			if d, ok := respMap["d"].(map[string]interface{}); ok {
				if no := extractDocNo(d, DocNoFields("v2"), true); no != "" {
					sapDocNo = no
				} else if cancel, exists := d["Cancel"].(map[string]interface{}); exists {
					if mat, ok := cancel["MaterialDocument"].(string); ok && mat != "" {
						sapDocNo = mat
					}
				}
			}
		} else if no := extractDocNo(respMap, DocNoFields(ep.ODataVersion), false); no != "" {
			// OData v4 mengembalikan root JSON object langsung
			sapDocNo = no
		}
	}

	if respMain.StatusCode != http.StatusCreated && respMain.StatusCode != http.StatusOK && respMain.StatusCode != http.StatusNoContent && respMain.StatusCode != http.StatusAccepted {
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

// GetMaterialDocumentByMaximoDocNo mencari nomor Material Document SAP berdasarkan nomor dokumen Maximo (MaterialDocumentHeaderText atau ReferenceDocument)
func (s *SAPClient) GetMaterialDocumentByMaximoDocNo(ep *model.SAPEndpoint, maximoDocNo string) (string, string, error) {
	if maximoDocNo == "" {
		return "", "", fmt.Errorf("nomor dokumen maximo kosong")
	}

	if ep.BaseURL == "" || strings.EqualFold(ep.BaseURL, "MOCK") {
		// Mock fallback
		return "5000000041", "2026", nil
	}

	// Filter berdasarkan MaterialDocumentHeaderText atau ReferenceDocument
	safeDocNo := strings.ReplaceAll(maximoDocNo, "'", "''")
	filterQuery := fmt.Sprintf("(MaterialDocumentHeaderText eq '%s' or ReferenceDocument eq '%s')", safeDocNo, safeDocNo)

	// Root layanan diambil dari endpoint_path (default .../API_MATERIAL_DOCUMENT_SRV).
	basePath, err := ServicePath(ep, "A_MaterialDocumentHeader")
	if err != nil {
		return "", "", err
	}
	queryURL := fmt.Sprintf("%s%s?$filter=%s&$select=MaterialDocument,MaterialDocumentYear,MaterialDocumentHeaderText&$top=1",
		ep.BaseURL, basePath, url.QueryEscape(filterQuery))

	req, err := http.NewRequest("GET", queryURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed create lookup request: %w", err)
	}

	username := ep.Username
	if username == "" {
		username = s.Username
	}
	password := ep.Password
	if password == "" {
		password = s.Password
	}

	req.SetBasicAuth(username, password)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpFor(username).Do(req)
	if err != nil {
		return "", "", fmt.Errorf("network error during lookup: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed read lookup response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("SAP lookup HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var respMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &respMap); err != nil {
		return "", "", fmt.Errorf("failed parse lookup json: %w", err)
	}

	// OData v2 membungkus array di d.results
	if d, ok := respMap["d"].(map[string]interface{}); ok {
		if results, ok := d["results"].([]interface{}); ok && len(results) > 0 {
			if first, ok := results[0].(map[string]interface{}); ok {
				matDoc, _ := first["MaterialDocument"].(string)
				docYear, _ := first["MaterialDocumentYear"].(string)
				if matDoc != "" {
					return matDoc, docYear, nil
				}
			}
		}
	}

	return "", "", fmt.Errorf("dokumen SAP tidak ditemukan untuk nomor Maximo: %s", maximoDocNo)
}

// GetMaterialDocumentItemsByPO mengambil daftar pergerakan barang (MaterialDocumentItem) di SAP berdasarkan nomor PurchaseOrder
func (s *SAPClient) GetMaterialDocumentItemsByPO(ep *model.SAPEndpoint, poNum string) ([]model.SAPMatDocItemSummary, error) {
	if poNum == "" {
		return nil, fmt.Errorf("nomor PurchaseOrder kosong")
	}

	if ep.BaseURL == "" || strings.EqualFold(ep.BaseURL, "MOCK") {
		return []model.SAPMatDocItemSummary{}, nil
	}

	safePO := strings.ReplaceAll(poNum, "'", "''")
	filterQuery := fmt.Sprintf("PurchaseOrder eq '%s'", safePO)

	basePath, err := ServicePath(ep, "A_MaterialDocumentItem")
	if err != nil {
		return nil, err
	}
	queryURL := fmt.Sprintf("%s%s?$filter=%s&$select=MaterialDocument,MaterialDocumentYear,GoodsMovementType,QuantityInEntryUnit",
		ep.BaseURL, basePath, url.QueryEscape(filterQuery))

	req, err := http.NewRequest("GET", queryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed create matdoc items query: %w", err)
	}

	username := ep.Username
	if username == "" {
		username = s.Username
	}
	password := ep.Password
	if password == "" {
		password = s.Password
	}

	req.SetBasicAuth(username, password)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpFor(username).Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error querying matdoc items: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed read matdoc items response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SAP matdoc items query HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var respMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &respMap); err != nil {
		return nil, fmt.Errorf("failed parse matdoc items json: %w", err)
	}

	var items []model.SAPMatDocItemSummary
	if d, ok := respMap["d"].(map[string]interface{}); ok {
		if results, ok := d["results"].([]interface{}); ok {
			for _, itemRaw := range results {
				if itemMap, ok := itemRaw.(map[string]interface{}); ok {
					matDoc, _ := itemMap["MaterialDocument"].(string)
					matYear, _ := itemMap["MaterialDocumentYear"].(string)
					movementType, _ := itemMap["GoodsMovementType"].(string)

					var qty float64
					switch v := itemMap["QuantityInEntryUnit"].(type) {
					case float64:
						qty = v
					case string:
						qty, _ = strconv.ParseFloat(v, 64)
					}

					items = append(items, model.SAPMatDocItemSummary{
						MaterialDocument:     matDoc,
						MaterialDocumentYear: matYear,
						GoodsMovementType:    movementType,
						QuantityInEntryUnit:  qty,
					})
				}
			}
		}
	}

	return items, nil
}

// GetProductValuationClass mengambil nilai ValuationClass dari SAP S/4HANA Grow untuk Item tertentu
// Endpoint: <root endpoint_path PRODUCT>/A_Product('<ItemNum>')/to_Valuation?$format=json
func (s *SAPClient) GetProductValuationClass(ep *model.SAPEndpoint, itemNum string) (string, error) {
	v, err := s.GetProductValuation(ep, itemNum)
	return v.Class, err
}

// ProductValuation adalah hasil A_Product/to_Valuation; UnitPrice dalam mata uang Currency (mata uang company code).
type ProductValuation struct {
	Class     string
	UnitPrice float64
	Currency  string
}

// GetProductValuation mengembalikan ValuationClass dan harga per unit dasar
// (StandardPrice bila prosedur "S", selain itu MovingAveragePrice, dibagi PriceUnitQty).
func (s *SAPClient) GetProductValuation(ep *model.SAPEndpoint, itemNum string) (ProductValuation, error) {
	cleanItem := strings.TrimSpace(itemNum)
	if cleanItem == "" {
		return ProductValuation{}, fmt.Errorf("item number kosong")
	}

	if ep.BaseURL == "" || strings.EqualFold(ep.BaseURL, "MOCK") {
		return ProductValuation{Class: "S001", UnitPrice: 100}, nil
	}
	username, password := s.credentials(ep)

	cleanBase := strings.TrimRight(ep.BaseURL, "/")
	path, err := ServicePath(ep, fmt.Sprintf("A_Product('%s')/to_Valuation?$format=json",
		url.PathEscape(strings.ReplaceAll(cleanItem, "'", "''"))))
	if err != nil {
		return ProductValuation{}, err
	}
	targetURL := cleanBase + path

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return ProductValuation{}, fmt.Errorf("failed creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := s.httpFor(username).Do(req)
	if err != nil {
		return ProductValuation{}, fmt.Errorf("request to SAP failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return ProductValuation{}, fmt.Errorf("SAP error %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return ParseProductValuation(bodyBytes, cleanItem)
}

// ParseProductValuation membaca respons OData v2 A_ProductValuation.
// Bila ada beberapa valuation area, area pertama yang dipakai.
func ParseProductValuation(body []byte, itemNum string) (ProductValuation, error) {
	var parsed struct {
		D struct {
			Results []struct {
				ValuationClass              string `json:"ValuationClass"`
				InventoryValuationProcedure string `json:"InventoryValuationProcedure"`
				StandardPrice               string `json:"StandardPrice"`
				MovingAveragePrice          string `json:"MovingAveragePrice"`
				PriceUnitQty                string `json:"PriceUnitQty"`
				Currency                    string `json:"Currency"`
			} `json:"results"`
		} `json:"d"`
	}
	notFound := fmt.Errorf("ValuationClass tidak ditemukan untuk item '%s'", itemNum)
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.D.Results) == 0 {
		return ProductValuation{}, notFound
	}
	v := parsed.D.Results[0]
	out := ProductValuation{Class: strings.TrimSpace(v.ValuationClass), Currency: strings.ToUpper(strings.TrimSpace(v.Currency))}
	if out.Class == "" {
		return ProductValuation{}, notFound
	}
	priceStr := v.MovingAveragePrice
	if strings.EqualFold(strings.TrimSpace(v.InventoryValuationProcedure), "S") {
		priceStr = v.StandardPrice
	}
	out.UnitPrice, _ = strconv.ParseFloat(strings.TrimSpace(priceStr), 64)
	if unit, err := strconv.ParseFloat(strings.TrimSpace(v.PriceUnitQty), 64); err == nil && unit > 0 {
		out.UnitPrice = out.UnitPrice / unit
	}
	return out, nil
}
