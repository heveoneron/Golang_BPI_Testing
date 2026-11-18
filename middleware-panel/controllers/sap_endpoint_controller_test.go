package controllers

import (
	"bytes"
	"encoding/json"
	"middleware-panel/models"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

type mockSAPEndpointService struct {
	endpoints     []models.SAPEndpoint
	globalBaseURL string
	savedEndpoint *models.SAPEndpoint
	updatedCount  int64
}

func (m *mockSAPEndpointService) GetEndpoints() ([]models.SAPEndpoint, error) {
	return m.endpoints, nil
}

func (m *mockSAPEndpointService) GetEndpointByCode(code string) (models.SAPEndpoint, error) {
	for _, ep := range m.endpoints {
		if ep.ServiceCode == code {
			return ep, nil
		}
	}
	return models.SAPEndpoint{}, nil
}

func (m *mockSAPEndpointService) GetGlobalBaseURL() (string, error) {
	return m.globalBaseURL, nil
}

func (m *mockSAPEndpointService) UpdateGlobalBaseURL(baseURL string) (int64, error) {
	m.globalBaseURL = baseURL
	m.updatedCount = int64(len(m.endpoints))
	return m.updatedCount, nil
}

func (m *mockSAPEndpointService) SaveEndpoint(ep *models.SAPEndpoint) error {
	m.savedEndpoint = ep
	return nil
}

func (m *mockSAPEndpointService) TestHandshake(code string) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "success"}, nil
}

func (m *mockSAPEndpointService) ExecuteSandbox(code, method, customPath string, payload interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "success"}, nil
}

func (m *mockSAPEndpointService) GetMaterialDocumentDetail(docNo, year string) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "success"}, nil
}

func (m *mockSAPEndpointService) DeleteEndpoint(code string) error {
	return nil
}

func setupSAPTestRouter(svc *mockSAPEndpointService) *gin.Engine {
	r := gin.New()
	store := cookie.NewStore([]byte("secret_key_test"))
	r.Use(sessions.Sessions("test_session", store))
	r.Use(func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("user_id", "admin")
		s.Set("role", "admin")
		s.Save()
		c.Next()
	})

	ctrl := NewSAPEndpointController(svc)
	r.POST("/api/sap-endpoints", ctrl.HandleSaveEndpoint)
	r.POST("/api/sap-endpoints/global-base-url", ctrl.HandleUpdateGlobalBaseURL)
	return r
}

func TestHandleSaveEndpoint_Success(t *testing.T) {
	mockSvc := &mockSAPEndpointService{}
	r := setupSAPTestRouter(mockSvc)

	body, _ := json.Marshal(models.SAPEndpoint{
		ServiceCode:  "PRODUCT",
		ServiceName:  "Product Master",
		ScenarioID:   "SAP_COM_0009",
		ODataVersion: "v2",
		BaseURL:      "https://my446066-api.s4hana.cloud.sap",
		EndpointPath: "/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product",
		Username:     "CU_SAP_COM_0009",
	})

	req, _ := http.NewRequest("POST", "/api/sap-endpoints", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if mockSvc.savedEndpoint == nil || mockSvc.savedEndpoint.ServiceCode != "PRODUCT" {
		t.Fatalf("endpoint was not saved to service")
	}
	if mockSvc.savedEndpoint.BaseURL != "https://my446066-api.s4hana.cloud.sap" {
		t.Fatalf("expected BaseURL https://my446066-api.s4hana.cloud.sap, got %s", mockSvc.savedEndpoint.BaseURL)
	}
}

func TestHandleUpdateGlobalBaseURL_Success(t *testing.T) {
	mockSvc := &mockSAPEndpointService{
		endpoints: []models.SAPEndpoint{
			{ServiceCode: "PO"},
			{ServiceCode: "PRODUCT"},
		},
	}
	r := setupSAPTestRouter(mockSvc)

	body, _ := json.Marshal(map[string]string{
		"base_url": "https://my446066-api.s4hana.cloud.sap",
	})

	req, _ := http.NewRequest("POST", "/api/sap-endpoints/global-base-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if mockSvc.globalBaseURL != "https://my446066-api.s4hana.cloud.sap" {
		t.Fatalf("expected globalBaseURL https://my446066-api.s4hana.cloud.sap, got %s", mockSvc.globalBaseURL)
	}
}
