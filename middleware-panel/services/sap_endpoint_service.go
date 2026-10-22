package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"middleware-panel/models"
	"middleware-panel/repository"
	"net/http"
	"os"
	"time"
)

type SAPEndpointService interface {
	GetEndpoints() ([]models.SAPEndpoint, error)
	GetEndpointByCode(code string) (models.SAPEndpoint, error)
	SaveEndpoint(ep *models.SAPEndpoint) error
	TestHandshake(code string) (map[string]interface{}, error)
	ExecuteSandbox(code, method string, payload interface{}) (map[string]interface{}, error)
}

type sapEndpointService struct {
	repo repository.SAPEndpointRepository
}

func NewSAPEndpointService(repo repository.SAPEndpointRepository) SAPEndpointService {
	return &sapEndpointService{repo: repo}
}

func (s *sapEndpointService) GetEndpoints() ([]models.SAPEndpoint, error) {
	return s.repo.GetAll()
}

func (s *sapEndpointService) GetEndpointByCode(code string) (models.SAPEndpoint, error) {
	return s.repo.GetByCode(code)
}

func (s *sapEndpointService) SaveEndpoint(ep *models.SAPEndpoint) error {
	return s.repo.Save(ep)
}

func (s *sapEndpointService) getMiddlewareAPIURL() string {
	apiURL := os.Getenv("MIDDLEWARE_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8085/api/v1"
	}
	return apiURL
}

func (s *sapEndpointService) TestHandshake(code string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/sap/endpoints/test", s.getMiddlewareAPIURL())
	reqBody, _ := json.Marshal(map[string]string{"service_code": code})

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("gagal menghubungi middleware engine: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return map[string]interface{}{
			"status":   "error",
			"message":  string(bodyBytes),
			"http_code": resp.StatusCode,
		}, nil
	}

	return result, nil
}

func (s *sapEndpointService) ExecuteSandbox(code, method string, payload interface{}) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/sap/execute-test", s.getMiddlewareAPIURL())
	reqBody, _ := json.Marshal(map[string]interface{}{
		"service_code": code,
		"method":       method,
		"payload":      payload,
	})

	client := &http.Client{Timeout: 35 * time.Second}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("gagal eksekusi ke middleware engine: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return map[string]interface{}{
			"status":   "error",
			"message":  string(bodyBytes),
			"http_code": resp.StatusCode,
		}, nil
	}

	return result, nil
}
