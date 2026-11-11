package service

import "testing"

func TestPaymentProposalFieldOverride(t *testing.T) {
	body := []byte(`{"d":{"results":[{"__metadata":{"uri":"https://my446174-api.s4hana.cloud.sap/sap/opu/odata/sap/ZPAY_CDS/ZPaymentProp('K1')"},
		"ZRunId":"R01","PaymentRunID":"IGNORED","ZAmt":"125.50","PaymentCurrency":"IDR","Supplier":"V001"}]}}`)

	// default: kandidat bawaan, key entity default tidak dipotong
	items, err := ParsePaymentProposals(body)
	if err != nil || len(items) != 1 || items[0].PaymentRunID != "IGNORED" || items[0].Amount != 0 || items[0].Supplier != "V001" {
		t.Fatalf("default mapping: %+v %v", items, err)
	}

	t.Setenv("FIN_PAYMENT_FIELD_RUN_ID", "ZRunId")
	t.Setenv("FIN_PAYMENT_FIELD_AMOUNT", "ZAmt, PaidAmountInPaytCurrency")
	t.Setenv("FIN_PAYMENT_ENTITY_SET", "ZPaymentProp")
	items, err = ParsePaymentProposals(body)
	if err != nil || len(items) != 1 {
		t.Fatal(err)
	}
	p := items[0]
	if p.PaymentRunID != "R01" || p.Amount != 125.5 || p.Currency != "IDR" {
		t.Fatalf("override mapping: %+v", p)
	}
	if p.RecordKey != "ZPaymentProp('K1')" {
		t.Fatalf("record key must be host-independent: %q", p.RecordKey)
	}
	if got := SAPFieldMapping()["FIN_PAYMENT_FIELD_AMOUNT"]; len(got) != 2 || got[0] != "ZAmt" {
		t.Fatalf("effective mapping: %v", got)
	}
}

func TestFixedAssetFieldOverride(t *testing.T) {
	body := []byte(`{"value":[{"CompanyCode":"1000","MasterFixedAsset":"5001","FixedAsset":"0","ZDesc":"Pompa","FixedAssetDescription":"old"}]}`)
	t.Setenv("FIN_ASSET_FIELD_DESCRIPTION", "ZDesc")
	items, err := ParseFixedAssets(body)
	if err != nil || len(items) != 1 || items[0].Description != "Pompa" {
		t.Fatalf("asset override: %+v %v", items, err)
	}
}
