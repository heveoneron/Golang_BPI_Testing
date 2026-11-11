package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang-bpi-testing/internal/repository"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestWebController_HealthCheck(t *testing.T) {
	ctrl := NewWebController(nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ctrl.HealthCheck(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "running" {
		t.Errorf("expected status 'running', got %q", resp.Status)
	}
}

func TestWebController_TriggerPush_NilService(t *testing.T) {
	ctrl := NewWebController(nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ctrl.TriggerPush(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when SyncSvc is nil, got %d", w.Code)
	}
}

func TestWebController_IngestManualPO_NilDB(t *testing.T) {
	ctrl := NewWebController(nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req, _ := http.NewRequest("POST", "/api/v1/ingest/po", bytes.NewBufferString(`{}`))
	c.Request = req

	ctrl.IngestManualPO(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when PGRepo is nil, got %d", w.Code)
	}
}

func TestWebController_SaveSAPEndpoint_HostAllowlist(t *testing.T) {
	ctrl := NewWebController(repository.NewPostgresRepository(nil), nil)
	post := func(body string) int {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/api/v1/sap/endpoints", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		ctrl.SaveSAPEndpoint(c)
		return w.Code
	}
	if code := post(`{"service_code":"PO","username":"u","base_url":"https://evil.example.com"}`); code != http.StatusBadRequest {
		t.Fatalf("host outside allowlist must be rejected, got %d", code)
	}
	if code := post(`{"service_code":"PO","username":"u","base_url":"https://my446174-api.s4hana.cloud.sap"}`); code != http.StatusOK {
		t.Fatalf("allowed host must be accepted, got %d", code)
	}
	if code := post(`{"service_code":"PO","username":"u","base_url":""}`); code != http.StatusOK {
		t.Fatalf("empty base_url (MOCK) must be accepted, got %d", code)
	}
}

func TestWebController_GetDocuments_NilDB(t *testing.T) {
	ctrl := NewWebController(nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ctrl.GetDocuments(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with empty list, got %d", w.Code)
	}

	var resp GetDocumentsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.Total != 0 || len(resp.Data) != 0 {
		t.Errorf("expected total 0 and empty data, got total %d", resp.Total)
	}
}
