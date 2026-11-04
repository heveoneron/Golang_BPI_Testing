package service

import (
	"context"
	"encoding/json"
	"os"
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

func TestSyncService_TransformGRN_Receipt(t *testing.T) {
	svc := &SyncService{}

	receiptJSON := []byte(`{
		"BPIGRNUMBER": "2026/GRN-03581",
		"PACKINGSLIPNUM": "3215/TSL/09/26",
		"ISSUETYPE": "RECEIPT",
		"PONUM": "4500016528",
		"ACTUALDATE": "2026-10-29",
		"STATUS": "COMP",
		"LINES": [
			{
				"POLINENUM": 1,
				"ITEMNUM": "SP006",
				"QTY": 5.0,
				"PONUM": "4100001372"
			}
		]
	}`)

	sapPayloadBytes, err := svc.transformGRN(context.Background(), receiptJSON)
	if err != nil {
		t.Fatalf("unexpected transformGRN error: %v", err)
	}

	var sapMatDoc model.SAPGrowMaterialDocumentPayload
	if err := json.Unmarshal(sapPayloadBytes, &sapMatDoc); err != nil {
		t.Fatalf("failed to unmarshal transformed SAP Material Document: %v", err)
	}

	if sapMatDoc.MaterialDocumentHeaderText != "2026/GRN-03581" {
		t.Errorf("expected MaterialDocumentHeaderText '2026/GRN-03581', got %q", sapMatDoc.MaterialDocumentHeaderText)
	}
	if sapMatDoc.ReferenceDocument != "3215/TSL/09/26" {
		t.Errorf("expected ReferenceDocument '3215/TSL/09/26', got %q", sapMatDoc.ReferenceDocument)
	}
	if sapMatDoc.GoodsMovementCode != "01" {
		t.Errorf("expected GoodsMovementCode '01', got %q", sapMatDoc.GoodsMovementCode)
	}
	if sapMatDoc.VersionForPrintingSlip != "3" {
		t.Errorf("expected VersionForPrintingSlip '3', got %q", sapMatDoc.VersionForPrintingSlip)
	}
	if len(sapMatDoc.ToMaterialDocumentItem) != 1 {
		t.Fatalf("expected 1 item, got %d", len(sapMatDoc.ToMaterialDocumentItem))
	}

	it := sapMatDoc.ToMaterialDocumentItem[0]
	if it.Material != "SP006" {
		t.Errorf("expected Material 'SP006', got %q", it.Material)
	}
	if it.Plant != "5110" {
		t.Errorf("expected Plant '5110', got %q", it.Plant)
	}
	if it.GoodsMovementType != "101" {
		t.Errorf("expected GoodsMovementType '101', got %q", it.GoodsMovementType)
	}
	if it.PurchaseOrder != "4100001372" {
		t.Errorf("expected PurchaseOrder '4100001372', got %q", it.PurchaseOrder)
	}
	if it.PurchaseOrderItem != "1" {
		t.Errorf("expected PurchaseOrderItem '1', got %q", it.PurchaseOrderItem)
	}
	if it.GoodsMovementRefDocType != "B" {
		t.Errorf("expected GoodsMovementRefDocType 'B', got %q", it.GoodsMovementRefDocType)
	}
	if it.QuantityInEntryUnit != "5" {
		t.Errorf("expected QuantityInEntryUnit '5', got %q", it.QuantityInEntryUnit)
	}
}

func TestSyncService_TransformGRN_Return(t *testing.T) {
	svc := &SyncService{}

	returnJSON := []byte(`{
		"BPIGRNUMBER": "2026/GRN-03102",
		"PACKINGSLIPNUM": "3215/TSL/09/26 BOH not recorded",
		"ISSUETYPE": "RETURN",
		"PONUM": "4100001372",
		"ACTUALDATE": "2026-10-29",
		"RECEIPTREF": "48045",
		"REF_BPIGRNUMBER": "2026/GRN-03102",
		"STATUS": "COMP",
		"LINES": [
			{
				"POLINENUM": 1,
				"ITEMNUM": "SP006",
				"QTY": -1.0,
				"RECEIPTREF": "48045"
			}
		]
	}`)

	sapPayloadBytes, err := svc.transformGRN(context.Background(), returnJSON)
	if err != nil {
		t.Fatalf("unexpected transformGRN error: %v", err)
	}

	var sapMatDoc model.SAPGrowMaterialDocumentPayload
	if err := json.Unmarshal(sapPayloadBytes, &sapMatDoc); err != nil {
		t.Fatalf("failed to unmarshal transformed SAP Material Document: %v", err)
	}

	if sapMatDoc.GoodsMovementCode != "01" {
		t.Errorf("expected GoodsMovementCode '01', got %q", sapMatDoc.GoodsMovementCode)
	}
	if len(sapMatDoc.ToMaterialDocumentItem) != 1 {
		t.Fatalf("expected 1 item, got %d", len(sapMatDoc.ToMaterialDocumentItem))
	}

	it := sapMatDoc.ToMaterialDocumentItem[0]
	if it.GoodsMovementType != "122" {
		t.Errorf("expected GoodsMovementType '122', got %q", it.GoodsMovementType)
	}
	if it.GoodsMovementReasonCode != "1" {
		t.Errorf("expected GoodsMovementReasonCode '1', got %q", it.GoodsMovementReasonCode)
	}
	// Verify Qty is positive 1 despite -1 in Maximo
	if it.QuantityInEntryUnit != "1" {
		t.Errorf("expected QuantityInEntryUnit '1', got %q", it.QuantityInEntryUnit)
	}
	if it.MaterialDocumentItemText != "Goods Receipt from Maximo" {
		t.Errorf("expected MaterialDocumentItemText 'Goods Receipt from Maximo', got %q", it.MaterialDocumentItemText)
	}
}

func TestSyncService_TransformGRN_VoidReturn(t *testing.T) {
	svc := &SyncService{}

	voidJSON := []byte(`{
		"BPIGRNUMBER": "2026/GRN-03102",
		"ISSUETYPE": "VOIDRETURN",
		"ACTUALDATE": "2026-10-29",
		"RECEIPTREF": "48045",
		"REF_BPIGRNUMBER": "2026/GRN-03102"
	}`)

	sapPayloadBytes, err := svc.transformGRN(context.Background(), voidJSON)
	if err != nil {
		t.Fatalf("unexpected transformGRN error: %v", err)
	}

	var cancelParam model.SAPCancelMaterialDocPayload
	if err := json.Unmarshal(sapPayloadBytes, &cancelParam); err != nil {
		t.Fatalf("failed to unmarshal cancel payload: %v", err)
	}

	if cancelParam.MaterialDocumentYear != "2026" {
		t.Errorf("expected MaterialDocumentYear '2026', got %q", cancelParam.MaterialDocumentYear)
	}
}

func TestSyncService_TransformItem(t *testing.T) {
	os.Setenv("SAP_PRODUCT_PLANTS", "1000,2000")
	defer os.Unsetenv("SAP_PRODUCT_PLANTS")

	svc := &SyncService{}

	itemJSON := []byte(`{
		"ITEMNUM": "DANDRUFFAH",
		"ITEMGROUPID": "IN-01",
		"ITEMTYPE": "ITEM",
		"STOCKTYPE": "STOCK",
		"ISSUEUNIT": "PC",
		"ORDERUNIT": "BAG",
		"DESCRIPTION": "DANDRUFFFFF"
	}`)

	sapBytes, err := svc.transformItem(context.Background(), itemJSON)
	if err != nil {
		t.Fatalf("unexpected transformItem error: %v", err)
	}

	var product model.SAPProductPayload
	if err := json.Unmarshal(sapBytes, &product); err != nil {
		t.Fatalf("failed to unmarshal SAP product payload: %v", err)
	}

	if product.Product != "DANDRUFFAH" {
		t.Errorf("expected Product 'DANDRUFFAH', got %q", product.Product)
	}
	if product.ProductType != "ZITM" {
		t.Errorf("expected ProductType 'ZITM', got %q", product.ProductType)
	}
	if product.ProductGroup != "STOCK" {
		t.Errorf("expected ProductGroup 'STOCK', got %q", product.ProductGroup)
	}
	if product.BaseUnit != "PC" {
		t.Errorf("expected BaseUnit 'PC', got %q", product.BaseUnit)
	}
	if product.PurchaseOrderQuantityUnit != "BAG" {
		t.Errorf("expected PurchaseOrderQuantityUnit 'BAG', got %q", product.PurchaseOrderQuantityUnit)
	}

	// Descriptions
	if len(product.ToDescription.Results) != 1 {
		t.Fatalf("expected 1 description, got %d", len(product.ToDescription.Results))
	}
	if product.ToDescription.Results[0].Language != "EN" {
		t.Errorf("expected Language 'EN', got %q", product.ToDescription.Results[0].Language)
	}
	if product.ToDescription.Results[0].ProductDescription != "DANDRUFFFFF" {
		t.Errorf("expected ProductDescription 'DANDRUFFFFF', got %q", product.ToDescription.Results[0].ProductDescription)
	}

	// Plants (configured 1000 and 2000)
	if len(product.ToPlant.Results) != 2 {
		t.Fatalf("expected 2 plants, got %d", len(product.ToPlant.Results))
	}
	if product.ToPlant.Results[0].Plant != "1000" || product.ToPlant.Results[1].Plant != "2000" {
		t.Errorf("expected Plants 1000 and 2000, got %v and %v", product.ToPlant.Results[0].Plant, product.ToPlant.Results[1].Plant)
	}

	// Valuation (configured 1000 and 2000 with Procedure V)
	if len(product.ToValuation.Results) != 2 {
		t.Fatalf("expected 2 valuations, got %d", len(product.ToValuation.Results))
	}
	if product.ToValuation.Results[0].ValuationArea != "1000" || product.ToValuation.Results[1].ValuationArea != "2000" {
		t.Errorf("expected ValuationAreas 1000 and 2000, got %v and %v", product.ToValuation.Results[0].ValuationArea, product.ToValuation.Results[1].ValuationArea)
	}
	if product.ToValuation.Results[0].InventoryValuationProcedure != "V" {
		t.Errorf("expected Procedure 'V', got %q", product.ToValuation.Results[0].InventoryValuationProcedure)
	}

	// Units of measure (when ORDERUNIT != ISSUEUNIT)
	if product.ToProductUnitsOfMeasure == nil || len(product.ToProductUnitsOfMeasure.Results) != 1 {
		t.Fatalf("expected 1 UOM, got %v", product.ToProductUnitsOfMeasure)
	}
	uom := product.ToProductUnitsOfMeasure.Results[0]
	if uom.AlternativeUnit != "BAG" || uom.BaseUnit != "PC" {
		t.Errorf("expected AlternativeUnit BAG and BaseUnit PC, got %s and %s", uom.AlternativeUnit, uom.BaseUnit)
	}
	if uom.QuantityNumerator != "1" || uom.QuantityDenominator != "1" {
		t.Errorf("expected 1:1 ratio, got %s:%s", uom.QuantityNumerator, uom.QuantityDenominator)
	}

	// Test case: when ORDERUNIT == ISSUEUNIT, to_ProductUnitsOfMeasure should be omitted to prevent SAP error M3/332
	sameUOMJSON := []byte(`{
		"ITEMNUM": "ITEM-SAME-UOM",
		"ISSUEUNIT": "PC",
		"ORDERUNIT": "PC"
	}`)
	sameBytes, err := svc.transformItem(context.Background(), sameUOMJSON)
	if err != nil {
		t.Fatalf("unexpected transformItem error: %v", err)
	}
	var sameProduct model.SAPProductPayload
	if err := json.Unmarshal(sameBytes, &sameProduct); err != nil {
		t.Fatalf("failed unmarshal: %v", err)
	}
	if sameProduct.ToProductUnitsOfMeasure != nil {
		t.Errorf("expected to_ProductUnitsOfMeasure to be nil when units match, got %+v", sameProduct.ToProductUnitsOfMeasure)
	}
	if sameProduct.PurchaseOrderQuantityUnit != "" {
		t.Errorf("expected empty PurchaseOrderQuantityUnit when units match, got %q", sameProduct.PurchaseOrderQuantityUnit)
	}
}

