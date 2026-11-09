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
	if sapPO.PurchaseOrderType != "ZBPI" && sapPO.PurchaseOrderType != "NB" {
		t.Errorf("expected PO Type 'ZBPI' or 'NB', got %q", sapPO.PurchaseOrderType)
	}
	if len(sapPO.PurchaseOrderItem) != 1 {
		t.Fatalf("expected 1 item in PurchaseOrderItem, got %d", len(sapPO.PurchaseOrderItem))
	}

	item := sapPO.PurchaseOrderItem[0]
	if item.PurchaseOrderItem != "1" {
		t.Errorf("expected item 1, got %q", item.PurchaseOrderItem)
	}
	if item.Material != "BEARING-TURBINE-6204" {
		t.Errorf("expected material BEARING-TURBINE-6204, got %q", item.Material)
	}
	if item.Plant != "5110" {
		t.Errorf("expected Plant '5110', got %q", item.Plant)
	}
	if item.NetPriceAmount != 250000 {
		t.Errorf("expected NetPriceAmount 250000, got %f", item.NetPriceAmount)
	}
	if item.TaxCode != "V0" {
		t.Errorf("expected TaxCode 'V0' for normal PO without tax, got %q", item.TaxCode)
	}
	if len(item.PurOrdPricingElement) != 0 {
		t.Errorf("expected no PricingElement for normal PO, got %d", len(item.PurOrdPricingElement))
	}
}

func TestSyncService_TransformPO_Type1_Normal(t *testing.T) {
	svc := &SyncService{}
	raw := []byte(`{
		"PONUM": "4700012063",
		"VENDOR": "VND-001",
		"ORDERDATE": "2026-01-01",
		"CURRENCYCODE": "IDR",
		"LINES": [
			{
				"POLINENUM": 1,
				"ITEMNUM": "SP006",
				"ORDERUNIT": "EA",
				"QTY": 10,
				"UNITCOST": 100000,
				"DESCRIPTION": "Normal PO item description"
			}
		]
	}`)

	bytes, err := svc.TransformPO(context.Background(), raw)
	if err != nil {
		t.Fatalf("TransformPO failed: %v", err)
	}

	var po model.SAPGrowPOPayload
	_ = json.Unmarshal(bytes, &po)

	if po.PurchaseOrderType != "ZCTR" {
		t.Errorf("expected prefix 47 to map to ZCTR, got %s", po.PurchaseOrderType)
	}
	if po.DownPaymentType != "" {
		t.Errorf("expected empty DownPaymentType for normal PO, got %s", po.DownPaymentType)
	}

	item := po.PurchaseOrderItem[0]
	if item.TaxCode != "V0" {
		t.Errorf("expected TaxCode V0, got %s", item.TaxCode)
	}
	if len(item.PurOrdPricingElement) != 0 {
		t.Errorf("expected 0 PurOrdPricingElement, got %d", len(item.PurOrdPricingElement))
	}
	if item.NetPriceAmount != 100000 {
		t.Errorf("expected NetPriceAmount 100000, got %f", item.NetPriceAmount)
	}
}

func TestSyncService_TransformPO_Type2_WithTax(t *testing.T) {
	svc := &SyncService{}
	raw := []byte(`{
		"PONUM": "4500001371",
		"VENDOR": "51300082",
		"ORDERDATE": "2026-01-01",
		"CURRENCYCODE": "IDR",
		"LINES": [
			{
				"POLINENUM": 1,
				"ITEMNUM": "SP006",
				"ORDERUNIT": "PC",
				"QTY": 20.0,
				"UNITCOST": 1235678,
				"TAXCODE1": "PPN11",
				"DESCRIPTION": "Item with PPN 11"
			}
		]
	}`)

	bytes, err := svc.TransformPO(context.Background(), raw)
	if err != nil {
		t.Fatalf("TransformPO failed: %v", err)
	}

	var po model.SAPGrowPOPayload
	_ = json.Unmarshal(bytes, &po)

	if po.PurchaseOrderType != "ZPRC" {
		t.Errorf("expected prefix 45 to map to ZPRC, got %s", po.PurchaseOrderType)
	}

	item := po.PurchaseOrderItem[0]
	if item.TaxCode != "" {
		t.Errorf("expected empty TaxCode when tax element is used, got %s", item.TaxCode)
	}
	if len(item.PurOrdPricingElement) != 2 {
		t.Fatalf("expected 2 PurOrdPricingElement (DRG1 & PMP0), got %d", len(item.PurOrdPricingElement))
	}

	drg1 := item.PurOrdPricingElement[0]
	if drg1.ConditionType != "DRG1" || drg1.ConditionRateAmount != 11 || drg1.ConditionRateRatioUnit != "%" {
		t.Errorf("DRG1 mismatch: %+v", drg1)
	}

	pmp0 := item.PurOrdPricingElement[1]
	if pmp0.ConditionType != "PMP0" || pmp0.ConditionRateAmount != 1235678 {
		t.Errorf("PMP0 mismatch: %+v", pmp0)
	}

	if item.NetPriceAmount != 1235678 {
		t.Errorf("expected NetPriceAmount 1235678 from UnitCost, got %f", item.NetPriceAmount)
	}
}

func TestSyncService_TransformPO_Type3_WithDP(t *testing.T) {
	svc := &SyncService{}
	raw := []byte(`{
		"PONUM": "4100000272",
		"VENDOR": "VND-001",
		"ORDERDATE": "2026-01-01",
		"CURRENCYCODE": "USD",
		"DOWNPAYMENTPERCENTAGE": 20.0,
		"DOWNPAYMENTDUEDATE": "2026-11-30",
		"LINES": [
			{
				"POLINENUM": 1,
				"ITEMNUM": "SP006",
				"ORDERUNIT": "EA",
				"QTY": 10,
				"UNITCOST": 12346
			}
		]
	}`)

	bytes, err := svc.TransformPO(context.Background(), raw)
	if err != nil {
		t.Fatalf("TransformPO failed: %v", err)
	}

	var po model.SAPGrowPOPayload
	_ = json.Unmarshal(bytes, &po)

	if po.DownPaymentType != "V" {
		t.Errorf("expected DownPaymentType 'V', got %s", po.DownPaymentType)
	}
	if po.DownPaymentPercentageOfTotAmt == nil || *po.DownPaymentPercentageOfTotAmt != 20.0 {
		t.Errorf("expected DownPaymentPercentageOfTotAmt 20.0, got %v", po.DownPaymentPercentageOfTotAmt)
	}
	if po.DownPaymentDueDate != "2026-11-30" {
		t.Errorf("expected DownPaymentDueDate '2026-11-30', got %s", po.DownPaymentDueDate)
	}

	item := po.PurchaseOrderItem[0]
	if item.TaxCode != "V0" {
		t.Errorf("expected TaxCode V0, got %s", item.TaxCode)
	}
	if len(item.PurOrdPricingElement) != 0 {
		t.Errorf("expected 0 PurOrdPricingElement, got %d", len(item.PurOrdPricingElement))
	}
}

func TestSyncService_TransformPO_Type4_Tax_And_DP(t *testing.T) {
	svc := &SyncService{}
	raw := []byte(`{
		"PONUM": "4100001372",
		"VENDOR": "51300082",
		"ORDERDATE": "2026-01-01",
		"CURRENCYCODE": "IDR",
		"DOWNPAYMENTPERCENTAGE": 20.0,
		"DOWNPAYMENTDUEDATE": "2026-11-30",
		"LINES": [
			{
				"POLINENUM": 1,
				"ITEMNUM": "SP006",
				"ORDERUNIT": "PC",
				"QTY": 20.0,
				"UNITCOST": 1235678,
				"TAXCODE1": "PPN11",
				"DESCRIPTION": "Barang Deskripsi Lengkap Maximo"
			}
		]
	}`)

	bytes, err := svc.TransformPO(context.Background(), raw)
	if err != nil {
		t.Fatalf("TransformPO failed: %v", err)
	}

	var po model.SAPGrowPOPayload
	_ = json.Unmarshal(bytes, &po)

	// Verifikasi Header DP
	if po.DownPaymentType != "V" {
		t.Errorf("expected DownPaymentType 'V', got %s", po.DownPaymentType)
	}
	if po.DownPaymentPercentageOfTotAmt == nil || *po.DownPaymentPercentageOfTotAmt != 20.0 {
		t.Errorf("expected DownPaymentPercentageOfTotAmt 20.0, got %v", po.DownPaymentPercentageOfTotAmt)
	}
	if po.DownPaymentDueDate != "2026-11-30" {
		t.Errorf("expected DownPaymentDueDate '2026-11-30', got %s", po.DownPaymentDueDate)
	}

	// Verifikasi Item
	item := po.PurchaseOrderItem[0]
	if item.NetPriceAmount != 1235678 {
		t.Errorf("expected NetPriceAmount 1235678 from UnitCost, got %f", item.NetPriceAmount)
	}
	if item.TaxCode != "" {
		t.Errorf("expected empty TaxCode when tax element is used, got %s", item.TaxCode)
	}

	// Verifikasi Item Tax Pricing Elements
	if len(item.PurOrdPricingElement) != 2 {
		t.Fatalf("expected 2 PurOrdPricingElement, got %d", len(item.PurOrdPricingElement))
	}
	if item.PurOrdPricingElement[0].ConditionType != "DRG1" || item.PurOrdPricingElement[0].ConditionRateAmount != 11 {
		t.Errorf("DRG1 mismatch: %+v", item.PurOrdPricingElement[0])
	}
	if item.PurOrdPricingElement[1].ConditionType != "PMP0" || item.PurOrdPricingElement[1].ConditionRateAmount != 1235678 {
		t.Errorf("PMP0 mismatch: %+v", item.PurOrdPricingElement[1])
	}

	// Verifikasi Note & Account Assignment
	if len(item.PurchaseOrderItemNote) != 1 || item.PurchaseOrderItemNote[0].PlainLongText != "Barang Deskripsi Lengkap Maximo" {
		t.Errorf("Note mismatch: %+v", item.PurchaseOrderItemNote)
	}
	if len(item.PurOrdAccountAssignment) != 1 || item.PurOrdAccountAssignment[0].CostCenter != "51101101" {
		t.Errorf("AccountAssignment mismatch: %+v", item.PurOrdAccountAssignment)
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

func TestSyncService_ValidatePOChange_Calculation(t *testing.T) {
	calc := func(items []model.SAPMatDocItemSummary) (bool, float64) {
		var sum101, sum102, sum122 float64
		for _, it := range items {
			switch strings.TrimSpace(it.GoodsMovementType) {
			case "101":
				sum101 += it.QuantityInEntryUnit
			case "102":
				sum102 += it.QuantityInEntryUnit
			case "122":
				sum122 += it.QuantityInEntryUnit
			}
		}
		hasil := sum101 - sum102 - sum122
		return hasil <= 0, hasil
	}

	// Case 1: Active GRN exists (101: 10 items) -> Cannot change (hasil = 10 > 0)
	case1 := []model.SAPMatDocItemSummary{
		{GoodsMovementType: "101", QuantityInEntryUnit: 10},
	}
	canChange, hasil := calc(case1)
	if canChange || hasil != 10 {
		t.Errorf("Case 1 expected canChange=false, hasil=10, got canChange=%v, hasil=%f", canChange, hasil)
	}

	// Case 2: GRN exists but canceled with 102 (101: 10, 102: 10) -> Can change (hasil = 0)
	case2 := []model.SAPMatDocItemSummary{
		{GoodsMovementType: "101", QuantityInEntryUnit: 10},
		{GoodsMovementType: "102", QuantityInEntryUnit: 10},
	}
	canChange, hasil = calc(case2)
	if !canChange || hasil != 0 {
		t.Errorf("Case 2 expected canChange=true, hasil=0, got canChange=%v, hasil=%f", canChange, hasil)
	}

	// Case 3: GRN exists but returned with 122 (101: 10, 122: 10) -> Can change (hasil = 0)
	case3 := []model.SAPMatDocItemSummary{
		{GoodsMovementType: "101", QuantityInEntryUnit: 10},
		{GoodsMovementType: "122", QuantityInEntryUnit: 10},
	}
	canChange, hasil = calc(case3)
	if !canChange || hasil != 0 {
		t.Errorf("Case 3 expected canChange=true, hasil=0, got canChange=%v, hasil=%f", canChange, hasil)
	}

	// Case 4: Partial return (101: 10, 122: 4) -> Cannot change (hasil = 6 > 0)
	case4 := []model.SAPMatDocItemSummary{
		{GoodsMovementType: "101", QuantityInEntryUnit: 10},
		{GoodsMovementType: "122", QuantityInEntryUnit: 4},
	}
	canChange, hasil = calc(case4)
	if canChange || hasil != 6 {
		t.Errorf("Case 4 expected canChange=false, hasil=6, got canChange=%v, hasil=%f", canChange, hasil)
	}
}



func TestSyncService_ConvertMaximoPOToSAP(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"4500005678", "450005678"},
		{"4500016528", "450016528"},
		{"4700000012-23", "470012023"},
		{"4700000123-1", "470123001"},
		{"4700000012", "470012"},
		{"4100001372", "4100001372"},
		{"4100001372-99", "4100001372-99"},
		{"", ""},
	}

	for _, tc := range testCases {
		got := ConvertMaximoPOToSAP(tc.input)
		if got != tc.expected {
			t.Errorf("ConvertMaximoPOToSAP(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestSyncService_ItemMaster_AmpersandToN(t *testing.T) {
	svc := &SyncService{}
	raw := []byte(`{
		"ITEMNUM": "SP&001-TURBINE",
		"DESCRIPTION": "Turbine spare part & filter",
		"ITEMTYPE": "ITEM",
		"STOCKTYPE": "Stock",
		"ISSUEUNIT": "PCS",
		"ORDERUNIT": "BOX"
	}`)

	payloadBytes, err := svc.transformItem(context.Background(), raw)
	if err != nil {
		t.Fatalf("transformItem error: %v", err)
	}

	var prod model.SAPProductPayload
	if err := json.Unmarshal(payloadBytes, &prod); err != nil {
		t.Fatalf("failed to unmarshal SAPProductPayload: %v", err)
	}

	// Verify '&' converted to 'N'
	if prod.Product != "SPN001-TURBINE" {
		t.Errorf("expected Product 'SPN001-TURBINE', got %q", prod.Product)
	}
	if strings.Contains(prod.Product, "&") {
		t.Errorf("Product code must not contain '&', got %q", prod.Product)
	}
}

func TestSyncService_GRN_DatesAndSiteID(t *testing.T) {
	svc := &SyncService{}
	raw := []byte(`{
		"BPIGRNUMBER": "2026/GRN-09999",
		"PACKINGSLIPNUM": "DO-999",
		"ISSUETYPE": "RECEIPT",
		"PONUM": "4500005678",
		"ACTUALDATE": "2026-10-30",
		"SITEID": "JKT",
		"LINES": [
			{
				"POLINENUM": 1,
				"ITEMNUM": "SP006",
				"QTY": 5,
				"PONUM": "4500005678",
				"SITEID": "BTNG"
			}
		]
	}`)

	payloadBytes, err := svc.transformGRN(context.Background(), raw)
	if err != nil {
		t.Fatalf("transformGRN error: %v", err)
	}

	var grn model.SAPGrowMaterialDocumentPayload
	if err := json.Unmarshal(payloadBytes, &grn); err != nil {
		t.Fatalf("failed to unmarshal SAPGrowMaterialDocumentPayload: %v", err)
	}

	// Verify Dates format
	if !strings.HasPrefix(grn.DocumentDate, "/Date(") || !strings.HasSuffix(grn.DocumentDate, ")/") {
		t.Errorf("DocumentDate invalid format: %q", grn.DocumentDate)
	}
	if !strings.HasPrefix(grn.PostingDate, "/Date(") || !strings.HasSuffix(grn.PostingDate, ")/") {
		t.Errorf("PostingDate invalid format: %q", grn.PostingDate)
	}

	// Verify Line conversion
	if len(grn.ToMaterialDocumentItem) != 1 {
		t.Fatalf("expected 1 item, got %d", len(grn.ToMaterialDocumentItem))
	}
	item := grn.ToMaterialDocumentItem[0]

	// Line SiteID BTNG -> 2000
	if item.Plant != "2000" {
		t.Errorf("expected Plant '2000' from BTNG, got %q", item.Plant)
	}

	// PO converted from 4500005678 -> 450005678
	if item.PurchaseOrder != "450005678" {
		t.Errorf("expected PurchaseOrder '450005678', got %q", item.PurchaseOrder)
	}
}