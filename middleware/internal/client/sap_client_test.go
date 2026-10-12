package client

import (
	"encoding/json"
	"testing"
)

func TestSAPClient_MockMode(t *testing.T) {
	c := NewSAPClient("MOCK", "test_user", "test_pass")
	if c == nil {
		t.Fatal("expected SAPClient to not be nil")
	}

	payload := []byte(`{"dummy": "data"}`)
	resp, err := c.PostODataDocument("/test/endpoint", payload)
	if err != nil {
		t.Fatalf("unexpected error in mock mode: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("failed to parse mock response json: %v", err)
	}

	d, ok := parsed["d"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'd' field in mock response, got: %v", parsed)
	}

	if status, exists := d["Status"]; !exists || status != "Created" {
		t.Errorf("expected Status 'Created', got %v", status)
	}

	if po, exists := d["PurchaseOrder"]; !exists || po == "" {
		t.Errorf("expected non-empty PurchaseOrder in mock response")
	}
}
