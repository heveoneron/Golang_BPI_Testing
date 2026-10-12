package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang-bpi-testing/internal/controller"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRouter_SwaggerRedirect(t *testing.T) {
	ctrl := controller.NewWebController(nil, nil)
	r := SetupRouter(ctrl)

	// Test 1: GET / should redirect to /panel
	reqRoot, _ := http.NewRequest("GET", "/", nil)
	wRoot := httptest.NewRecorder()
	r.ServeHTTP(wRoot, reqRoot)

	if wRoot.Code != http.StatusMovedPermanently {
		t.Errorf("GET /: expected %d, got %d", http.StatusMovedPermanently, wRoot.Code)
	}
	if loc := wRoot.Header().Get("Location"); loc != "/panel" {
		t.Errorf("GET /: expected redirect to '/panel', got %q", loc)
	}

	// Test 2: GET /swagger should redirect to /swagger/index.html
	reqSwagger, _ := http.NewRequest("GET", "/swagger", nil)
	wSwagger := httptest.NewRecorder()
	r.ServeHTTP(wSwagger, reqSwagger)

	if wSwagger.Code != http.StatusMovedPermanently {
		t.Errorf("GET /swagger: expected %d, got %d", http.StatusMovedPermanently, wSwagger.Code)
	}
}

func TestHandlerCheck(t *testing.T) {
	t.Logf("swaggerFiles.Handler is: %#v", swaggerFiles.Handler)
}

func TestRouter_SwaggerIndexHTML(t *testing.T) {
	ctrl := controller.NewWebController(nil, nil)
	r := SetupRouter(ctrl)

	for _, rt := range r.Routes() {
		t.Logf("Route: %s %s", rt.Method, rt.Path)
	}

	req, _ := http.NewRequest("GET", "/swagger/index.html", nil)
	req.RequestURI = "/swagger/index.html"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /swagger/index.html: expected %d, got %d", http.StatusOK, w.Code)
	}
	if !strings.Contains(w.Body.String(), "swagger-ui") {
		t.Errorf("expected body to contain 'swagger-ui'")
	}
}

func TestRouter_SwaggerDocJSON(t *testing.T) {
	ctrl := controller.NewWebController(nil, nil)
	r := SetupRouter(ctrl)

	req, _ := http.NewRequest("GET", "/swagger/doc.json", nil)
	req.RequestURI = "/swagger/doc.json"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /swagger/doc.json: expected %d, got %d", http.StatusOK, w.Code)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("failed to parse swagger doc json: %v", err)
	}

	if doc["swagger"] != "2.0" {
		t.Errorf("expected swagger version '2.0', got %v", doc["swagger"])
	}

	info, ok := doc["info"].(map[string]interface{})
	if !ok || info["title"] == nil {
		t.Errorf("expected valid info.title in swagger doc, got %v", doc["info"])
	}
}

func TestRouter_HealthCheck(t *testing.T) {
	ctrl := controller.NewWebController(nil, nil)
	r := SetupRouter(ctrl)

	req, _ := http.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/health: expected %d, got %d", http.StatusOK, w.Code)
	}

	var res controller.HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}

	if res.Status != "running" {
		t.Errorf("expected status 'running', got %q", res.Status)
	}
}

func TestRouter_GetDocuments_NoDB(t *testing.T) {
	ctrl := controller.NewWebController(nil, nil)
	r := SetupRouter(ctrl)

	req, _ := http.NewRequest("GET", "/api/v1/documents", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/documents: expected %d, got %d", http.StatusOK, w.Code)
	}

	var res controller.GetDocumentsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode documents response: %v", err)
	}

	if res.Total != 0 {
		t.Errorf("expected total 0 when DB is nil, got %d", res.Total)
	}
}

func TestRouter_IngestManualPO_InvalidPayload(t *testing.T) {
	ctrl := controller.NewWebController(nil, nil)
	r := SetupRouter(ctrl)

	// Service unavailable when DB is nil
	req, _ := http.NewRequest("POST", "/api/v1/ingest/po", strings.NewReader("{invalid-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable && w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/v1/ingest/po: expected 503 or 400, got %d", w.Code)
	}
}
