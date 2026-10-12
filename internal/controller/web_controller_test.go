package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
