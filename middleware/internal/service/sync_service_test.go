package service

import (
	"context"
	"encoding/json"
	"strings"
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

	if sapPO.Supplier != "VEND-PLTU-TEST" {
		t.Errorf("expected supplier 'VEND-PLTU-TEST', got %q", sapPO.Supplier)
	}
	if sapPO.PurchaseOrderType != "NB" {
		t.Errorf("expected PO Type 'NB', got %q", sapPO.PurchaseOrderType)
	}
	if len(sapPO.PurchaseOrderItem) != 1 {
		t.Fatalf("expected 1 item in PurchaseOrderItem, got %d", len(sapPO.PurchaseOrderItem))
	}

	item := sapPO.PurchaseOrderItem[0]
	if item.PurchaseOrderItem != "10" {
		t.Errorf("expected item 10, got %q", item.PurchaseOrderItem)
	}
	if item.Material != "BEARING-TURBINE-6204" {
		t.Errorf("expected material BEARING-TURBINE-6204, got %q", item.Material)
	}
	if item.Plant != "5110" {
		t.Errorf("expected Plant '5110', got %q", item.Plant)
	}
}

func TestSyncService_TransformPO_InvalidJSON(t *testing.T) {
	svc := &SyncService{}

	_, err := svc.transformPO(context.Background(), []byte(`{invalid-json`))
	if err == nil {
		t.Errorf("expected error for invalid json, got nil")
	}
}

func TestSyncService_TransformVendor_Structure(t *testing.T) {
	svc := &SyncService{}

	vendorJSON := []byte(`{
		"BPIVENDORGROUP": "01",
		"COMPANY": "VND-UATF",
		"CURRENCYCODE": "USD",
		"NAME": "Vendor UAT Final Testing (2)",
		"ORGID": "BPI"
	}`)

	sapPayloadBytes, err := svc.transformVendor(context.Background(), vendorJSON)
	if err != nil {
		t.Fatalf("unexpected transformVendor error: %v", err)
	}

	var sapBP model.SAPGrowBPPayload
	if err := json.Unmarshal(sapPayloadBytes, &sapBP); err != nil {
		t.Fatalf("failed to unmarshal transformed SAP BP: %v", err)
	}

	if sapBP.BusinessPartner != "VND-UATF" {
		t.Errorf("expected BusinessPartner 'VND-UATF', got %q", sapBP.BusinessPartner)
	}
	if sapBP.BusinessPartnerCategory != "2" {
		t.Errorf("expected BusinessPartnerCategory '2', got %q", sapBP.BusinessPartnerCategory)
	}
	if sapBP.BusinessPartnerGrouping != "Z001" {
		t.Errorf("expected BusinessPartnerGrouping 'Z001', got %q", sapBP.BusinessPartnerGrouping)
	}
	if sapBP.FormOfAddress != "0003" {
		t.Errorf("expected FormOfAddress '0003', got %q", sapBP.FormOfAddress)
	}
	if sapBP.OrganizationBPName1 != "Vendor UAT Final Testing (2)" {
		t.Errorf("expected OrganizationBPName1 'Vendor UAT Final Testing (2)', got %q", sapBP.OrganizationBPName1)
	}
	if sapBP.OrganizationBPName2 != "" {
		t.Errorf("expected OrganizationBPName2 '', got %q", sapBP.OrganizationBPName2)
	}
	if sapBP.BusinessPartnerIsBlocked != false {
		t.Errorf("expected BusinessPartnerIsBlocked false, got %v", sapBP.BusinessPartnerIsBlocked)
	}

	// Roles
	if len(sapBP.ToBusinessPartnerRole) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(sapBP.ToBusinessPartnerRole))
	}
	if sapBP.ToBusinessPartnerRole[0].BusinessPartnerRole != "FLVN00" || sapBP.ToBusinessPartnerRole[1].BusinessPartnerRole != "FLVN01" {
		t.Errorf("expected roles FLVN00 and FLVN01, got %v", sapBP.ToBusinessPartnerRole)
	}

	// Address
	if len(sapBP.ToBusinessPartnerAddress) != 1 {
		t.Fatalf("expected 1 address, got %d", len(sapBP.ToBusinessPartnerAddress))
	}
	if sapBP.ToBusinessPartnerAddress[0].Country != "ID" || sapBP.ToBusinessPartnerAddress[0].Language != "EN" {
		t.Errorf("expected Country ID and Language EN, got %v", sapBP.ToBusinessPartnerAddress[0])
	}

	// Supplier
	if sapBP.ToSupplier == nil {
		t.Fatalf("expected to_Supplier not nil")
	}
	if len(sapBP.ToSupplier.ToSupplierCompany) != 1 {
		t.Fatalf("expected 1 company in to_SupplierCompany, got %d", len(sapBP.ToSupplier.ToSupplierCompany))
	}
	if sapBP.ToSupplier.ToSupplierCompany[0].CompanyCode != "5110" {
		t.Errorf("expected CompanyCode '5110', got %q", sapBP.ToSupplier.ToSupplierCompany[0].CompanyCode)
	}
	if sapBP.ToSupplier.ToSupplierCompany[0].ReconciliationAccount != "211001" {
		t.Errorf("expected ReconAccount '211001', got %q", sapBP.ToSupplier.ToSupplierCompany[0].ReconciliationAccount)
	}

	// Purchasing Org
	if len(sapBP.ToSupplier.ToSupplierPurchasingOrg) != 1 {
		t.Fatalf("expected 1 purchasing org, got %d", len(sapBP.ToSupplier.ToSupplierPurchasingOrg))
	}
	if sapBP.ToSupplier.ToSupplierPurchasingOrg[0].PurchasingOrganization != "5110" {
		t.Errorf("expected PurchasingOrganization '5110', got %q", sapBP.ToSupplier.ToSupplierPurchasingOrg[0].PurchasingOrganization)
	}
	if sapBP.ToSupplier.ToSupplierPurchasingOrg[0].PurchaseOrderCurrency != "USD" {
		t.Errorf("expected PurchaseOrderCurrency 'USD', got %q", sapBP.ToSupplier.ToSupplierPurchasingOrg[0].PurchaseOrderCurrency)
	}
}

func TestSyncService_TransformVendor_NameSplit(t *testing.T) {
	svc := &SyncService{}

	// Single long word of 90 characters
	longName := "1234567890123456789012345678901234567890ABCDEFGHIJ1234567890123456789012345678901234567890EXTRA"
	vendorJSON := []byte(`{
		"BPIVENDORGROUP": "01",
		"COMPANY": "VND-LONG",
		"CURRENCYCODE": "IDR",
		"NAME": "` + longName + `",
		"ORGID": "BPI"
	}`)

	sapPayloadBytes, err := svc.transformVendor(context.Background(), vendorJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sapBP model.SAPGrowBPPayload
	_ = json.Unmarshal(sapPayloadBytes, &sapBP)

	if len([]rune(sapBP.OrganizationBPName1)) != 40 {
		t.Errorf("expected Name1 len 40, got %d (%q)", len([]rune(sapBP.OrganizationBPName1)), sapBP.OrganizationBPName1)
	}
	if len([]rune(sapBP.OrganizationBPName2)) != 40 {
		t.Errorf("expected Name2 len 40, got %d (%q)", len([]rune(sapBP.OrganizationBPName2)), sapBP.OrganizationBPName2)
	}
	if sapBP.OrganizationBPName3 != "1234567890EXTRA" {
		t.Errorf("expected Name3 '1234567890EXTRA', got %q", sapBP.OrganizationBPName3)
	}
	if sapBP.OrganizationBPName4 != "" {
		t.Errorf("expected Name4 '', got %q", sapBP.OrganizationBPName4)
	}
}

func TestSyncService_TransformVendor_WordWrap(t *testing.T) {
	name := "Vendor masi teesting Berhasil ga yakk tapi gw juga ga tau ini bakal sukses atp engga bro, jadinya gas dahhh testing-testing teros ampe berhasil semua"
	n1, n2, n3, n4 := splitBPName(name)

	for idx, part := range []string{n1, n2, n3, n4} {
		if len([]rune(part)) > 40 {
			t.Errorf("slot %d exceeds 40 chars: %d (%q)", idx+1, len([]rune(part)), part)
		}
	}

	// Verify words are kept whole
	if strings.HasSuffix(n1, "ta") || strings.HasPrefix(n2, "pi ") {
		t.Errorf("word 'tapi' was cut across lines: n1=%q, n2=%q", n1, n2)
	}
	if strings.HasSuffix(n2, "e") || strings.HasPrefix(n3, "ngga") {
		t.Errorf("word 'engga' was cut across lines: n2=%q, n3=%q", n2, n3)
	}
	if strings.HasSuffix(n3, "testing-test") || strings.HasPrefix(n4, "ing") {
		t.Errorf("word 'testing-testing' was cut across lines: n3=%q, n4=%q", n3, n4)
	}
}

