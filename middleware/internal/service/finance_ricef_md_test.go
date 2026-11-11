package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"golang-bpi-testing/internal/client"
	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
)

func TestBuildBTMUFile_MatchesSample(t *testing.T) {
	t.Setenv("FIN_BTMU_DEBIT_ACCOUNT", "3665IDRCUA5100015546")
	t.Setenv("FIN_BTMU_INSTRUCTION", "RRCR0142+")
	raw, _ := json.Marshal(map[string]any{
		"SupplierShortName": "PT. CAHAYA DEWA", "PayeeName": "PT. CAHAYA DEWATA NUSANTARA", "PayeeCityName": "JAKARTA",
		"BankName": "PT. BANK CENTRAL ASIA, TBK.", "BankAccount": "2886866868",
	})
	data, err := BuildBTMUFile([]model.PaymentProposal{{Supplier: "261001", Amount: 40090980, Currency: "IDR", Raw: raw}},
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(`3665IDRCUA5100015546,"261001","PT. CAHAYA DEWA","Domestic","IDR","40090980.00","","","","","PT. BANK CENTRAL ASIA, TBK.","","2886866868","PT. CAHAYA DEWATA NUSANTARA/JAKARTA","","03","RRCR0142+","OUR","","","","","","","","","","","",""`, `,"`, `;"`) + "\r\n"
	if string(data) != want {
		t.Fatalf("BTMU mismatch\n got: %s\nwant: %s", data, want)
	}
}

func TestBankFile_FieldNamesConfigurable(t *testing.T) {
	t.Setenv("FIN_BRI_DEBIT_ACCOUNT", "011101000305307")
	t.Setenv("FIN_PAYMENT_FIELD_PAYEE_NAME", "YY1_PayeeName_PAY")
	raw, _ := json.Marshal(map[string]any{"YY1_PayeeName_PAY": "PT. CONFIG", "PayeeName": "PT. DEFAULT"})
	data, err := BuildBRIFile([]model.PaymentProposal{{Amount: 1, Raw: raw}}, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))
	if err != nil || !strings.Contains(string(data), "|PT. CONFIG|") {
		t.Fatalf("configured field name not used: %q %v", data, err)
	}
}

func TestBuildBRIFile_MatchesSample(t *testing.T) {
	t.Setenv("FIN_BRI_DEBIT_ACCOUNT", "011101000305307")
	raw, _ := json.Marshal(map[string]any{"PayeeName": "PT. INDO KENCANA LESTARI", "PayeeCityName": "JAKARTA", "BankNumber": "002", "BankAccount": "011101000305307"})
	data, err := BuildBRIFile([]model.PaymentProposal{{Amount: 50000000, Currency: "IDR", Raw: raw}}, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := "011101000305307|021026|IDR|50000000.00|0000000000|PT. INDO KENCANA LESTARI|JAKARTA|002|011101000305307|PT BHIMASENA POWER INDONESIA|PEMBAYARAN TAGIHAN|1|0|0|0|\r\n"
	if string(data) != want {
		t.Fatalf("BRI mismatch\n got: %q\nwant: %q", data, want)
	}
	if n := len(strings.Split(strings.TrimSpace(string(data)), "|")); n != 16 {
		t.Fatalf("BRI harus 16 kolom, dapat %d", n)
	}
}

func TestBankFile_RequiresDebitAccount(t *testing.T) {
	t.Setenv("FIN_BTMU_DEBIT_ACCOUNT", "")
	if _, err := BuildBTMUFile(nil, time.Time{}); err == nil {
		t.Fatal("harus error bila rekening debit kosong")
	}
}

func TestGIInputFromStaging(t *testing.T) {
	t.Setenv("FIN_GI_ISSUETYPES", "")
	td := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	in, ok := GIInputFromStaging(repository.InventoryStagingRow{Source: "axinvissue", SourceID: 77, ItemNum: "SP001", Quantity: 3, TransDate: td, TransType: "ISSUE", RefWO: "WO123", InvUseNum: "IU-9", SiteID: "BTNG"})
	if !ok || in.Reference != "GI-77" || in.TransactionType != "GOODS_ISSUE" || in.TransDate != "2026-10-28" || in.Reference1 != "IU-9" {
		t.Fatalf("GI mapping salah: %+v ok=%v", in, ok)
	}
	if _, ok := GIInputFromStaging(repository.InventoryStagingRow{Source: "axinvissue", SourceID: 78, ItemNum: "SP001", Quantity: 3, TransType: "TRANSFER"}); ok {
		t.Fatal("issuetype di luar FIN_GI_ISSUETYPES tidak boleh diproses")
	}
	adj, ok := GIInputFromStaging(repository.InventoryStagingRow{Source: "axinvtrans", SourceID: 5, ItemNum: "SP002", Quantity: -2, TransDate: td, TransType: "ADJUST"})
	if !ok || adj.Reference != "ADJ-5" || adj.TransactionType != "STOCK_ADJUSTMENT" {
		t.Fatalf("ADJ mapping salah: %+v", adj)
	}
	if _, ok := GIInputFromStaging(repository.InventoryStagingRow{Source: "axinvtrans", SourceID: 6, ItemNum: "SP002", Quantity: 0}); ok {
		t.Fatal("quantity nol harus dilewati")
	}
}

func TestDetermineJournalEntry_ReturnAndScope(t *testing.T) {
	t.Setenv("FIN_COMPANY_CODE", "")
	svc := &FinanceService{}
	req, err := svc.DetermineJournalEntryFromGIOrAdj(t.Context(), model.GIOrAdjustmentInput{Reference: "GI-9", TransactionType: "GOODS_RETURN", ItemNum: "X", ValuationClass: "S001", Quantity: 1, UnitCost: 5, TransDate: "2026-10-28"})
	if err != nil || req.CompanyCode != "1000" || req.HeaderText != "Goods Return" {
		t.Fatalf("return header wrong: %+v %v", req, err)
	}
	if req.Items[0].GLAccount != "114001" || req.Items[0].DebitCredit != "S" || req.Items[0].CostCenter != "" ||
		req.Items[1].GLAccount != "512033" || req.Items[1].DebitCredit != "H" || req.Items[1].CostCenter != "71098" {
		t.Fatalf("RETURN must reverse GI debit/credit: %+v", req.Items)
	}
	_, err = svc.DetermineJournalEntryFromGIOrAdj(t.Context(), model.GIOrAdjustmentInput{Reference: "GI-10", TransactionType: "GI", ItemNum: "X", ValuationClass: "3040", Quantity: 1, UnitCost: 5})
	if !errors.Is(err, ErrValuationClassOutOfScope) {
		t.Fatalf("3040 must be out of scope, got %v", err)
	}
	if in, ok := GIInputFromStaging(repository.InventoryStagingRow{Source: "axinvissue", SourceID: 11, ItemNum: "SP", Quantity: 2, TransType: "RETURN"}); !ok || in.TransactionType != "GOODS_RETURN" {
		t.Fatalf("RETURN staging row must be posted: %+v %v", in, ok)
	}
}

func TestResolveReimbursementGL_CostCenterFromBudgetCode(t *testing.T) {
	rows := []model.SupplierInvoiceRow{
		{RowNumber: 2, InvoicingParty: "VND-0009", PolicyName: "Domestic", CIPFlag: "CIP"},
		{RowNumber: 3, InvoicingParty: "VND-0009", PolicyName: "Domestic", CIPFlag: "CIP", CostCenter: "51101101"},
	}
	gl := func(policy, flag string) (string, string, bool, error) { return "511018", "19031", true, nil }
	cip := func(string) (string, bool, error) { return "CIP", true, nil }
	errs, err := ResolveReimbursementGL(rows, gl, cip)
	if err != nil || len(errs) != 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if rows[0].CostCenter != "19031" || rows[0].GLAccount != "511018" || rows[1].CostCenter != "51101101" {
		t.Fatalf("cost center wrong: %+v", rows)
	}
}

func TestNormalizeSAPBaseURL(t *testing.T) {
	ok := map[string]string{
		"https://my446174-api.s4hana.cloud.sap/": "https://my446174-api.s4hana.cloud.sap",
		"HTTPS://MY445160-API.S4HANA.CLOUD.SAP":  "https://my445160-api.s4hana.cloud.sap",
		"mock":                                   "MOCK",
	}
	for in, want := range ok {
		if got, err := NormalizeSAPBaseURL(in); err != nil || got != want {
			t.Errorf("%s -> %s %v", in, got, err)
		}
	}
	for _, bad := range []string{"http://my446174-api.s4hana.cloud.sap", "https://evil.example.com", "https://my446174-api.s4hana.cloud.sap/sap/opu", "https://u:p@my446174-api.s4hana.cloud.sap"} {
		if _, err := NormalizeSAPBaseURL(bad); err == nil {
			t.Errorf("%s must be rejected", bad)
		}
	}
}

func TestDetermineJournalEntry_NoFakeAmountOrCompany(t *testing.T) {
	t.Setenv("FIN_COMPANY_CODE", "5110")
	svc := &FinanceService{}
	_, err := svc.DetermineJournalEntryFromGIOrAdj(t.Context(), model.GIOrAdjustmentInput{Reference: "GI-1", TransactionType: "GI", ItemNum: "X", ValuationClass: "S001", Quantity: 0, UnitCost: 10})
	if err == nil {
		t.Fatal("nominal nol harus ditolak, bukan diganti 1000")
	}
	req, err := svc.DetermineJournalEntryFromGIOrAdj(t.Context(), model.GIOrAdjustmentInput{Reference: "GI-2", TransactionType: "GI", ItemNum: "X", ValuationClass: "S001", Quantity: 2, UnitCost: 10, TransDate: "2026-10-28"})
	if err != nil || req.CompanyCode != "5110" || req.Items[0].Amount != 20 {
		t.Fatalf("hasil salah: %+v err=%v", req, err)
	}
}

func TestParseProductValuation(t *testing.T) {
	body := `{"d":{"results":[{"ValuationClass":"S002","InventoryValuationProcedure":"V","StandardPrice":"0","MovingAveragePrice":"250.00","PriceUnitQty":"10","Currency":"idr"}]}}`
	v, err := client.ParseProductValuation([]byte(body), "SP001")
	if err != nil || v.Class != "S002" || v.UnitPrice != 25 || v.Currency != "IDR" {
		t.Fatalf("valuation=%+v err=%v", v, err)
	}
}
