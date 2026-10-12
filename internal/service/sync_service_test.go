package service

import (
	"context"
	"encoding/json"
	"testing"

	"golang-bpi-testing/internal/model"
)

func TestSyncService_TransformPO_Structure(t *testing.T) {
	svc := &SyncService{}

	maximoPOJSON := []byte(`{
		"PONUM": "PO-BPI-TEST-001",
		"VENDOR": "VEND-PLTU-TEST",
		"ORDERDATE": "2026-10-12",
		"CURRENCYCODE": "IDR",
		"STATUS": "APPR",
		"REVISIONNUM": 0,
		"LINES": [
			{
				"POLINENUM": 1,
				"ITEMNUM": "BEARING-TURBINE-6204",
				"ORDERUNIT": "PCS",
				"QTY": 10,
				"UNITCOST": 250000,
				"BUDGETCODE": "",
				"DEPARTMENT": "PLTU-MAIN",
				"MAINACCOUNT": "513043"
			}
		]
	}`)

	sapPayloadBytes, err := svc.transformPO(context.Background(), maximoPOJSON)
	if err != nil {
		t.Fatalf("unexpected transformPO error: %v", err)
	}

	var sapPO model.SAPGrowPOPayload
	if err := json.Unmarshal(sapPayloadBytes, &sapPO); err != nil {
		t.Fatalf("failed to unmarshal transformed SAP PO: %v", err)
	}

	if sapPO.CorrespncExternalRefDoc != "PO-BPI-TEST-001" {
		t.Errorf("expected external ref doc 'PO-BPI-TEST-001', got %q", sapPO.CorrespncExternalRefDoc)
	}
	if sapPO.Supplier != "VEND-PLTU-TEST" {
		t.Errorf("expected supplier 'VEND-PLTU-TEST', got %q", sapPO.Supplier)
	}
	if len(sapPO.ToPurchaseOrderItem) != 1 {
		t.Fatalf("expected 1 item in ToPurchaseOrderItem, got %d", len(sapPO.ToPurchaseOrderItem))
	}

	item := sapPO.ToPurchaseOrderItem[0]
	if item.PurchaseOrderItem != "10" {
		t.Errorf("expected item 10, got %q", item.PurchaseOrderItem)
	}
	if item.Material != "BEARING-TURBINE-6204" {
		t.Errorf("expected material BEARING-TURBINE-6204, got %q", item.Material)
	}
	if item.AccountAssignmentCategory != "K" {
		t.Errorf("expected AccountAssignmentCategory 'K', got %q", item.AccountAssignmentCategory)
	}
}

func TestSyncService_TransformPO_InvalidJSON(t *testing.T) {
	svc := &SyncService{}

	_, err := svc.transformPO(context.Background(), []byte(`{invalid-json`))
	if err == nil {
		t.Errorf("expected error for invalid json, got nil")
	}
}
