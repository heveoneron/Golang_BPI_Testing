package service

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"strings"
	"testing"
	"time"

	"golang-bpi-testing/internal/client"
	"golang-bpi-testing/internal/model"
)

func validJournal() model.JournalEntryRequest {
	return model.JournalEntryRequest{
		Reference:    "JE-TEST-001",
		DocumentDate: "2026-11-05",
		PostingDate:  "2026-11-05",
		HeaderText:   "Pemakaian material & sparepart turbin",
		Items: []model.JournalEntryItem{
			{GLAccount: "513043", Amount: 1500000, DebitCredit: "s", CostCenter: "51101101", BudgetCode: "10081", ItemText: "Bearing <6204>"},
			{GLAccount: "141000", Amount: 1500000, DebitCredit: "H"},
		},
	}
}

func TestValidateJournalEntry_DefaultsAndBalance(t *testing.T) {
	req := validJournal()
	if err := ValidateJournalEntry(&req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.CompanyCode != "5110" || req.DocumentType != "SA" || req.Currency != "IDR" {
		t.Errorf("defaults not applied: %+v", req)
	}
	if req.Items[0].DebitCredit != "S" {
		t.Errorf("debit_credit should be upper-cased, got %q", req.Items[0].DebitCredit)
	}
}

func TestValidateJournalEntry_Rejects(t *testing.T) {
	cases := map[string]func(r *model.JournalEntryRequest){
		"unbalanced":    func(r *model.JournalEntryRequest) { r.Items[1].Amount = 1000 },
		"single line":   func(r *model.JournalEntryRequest) { r.Items = r.Items[:1] },
		"bad date":      func(r *model.JournalEntryRequest) { r.PostingDate = "05/11/2026" },
		"bad dc":        func(r *model.JournalEntryRequest) { r.Items[0].DebitCredit = "X" },
		"pipe in ref":   func(r *model.JournalEntryRequest) { r.Reference = "A|B" },
		"long ref1":     func(r *model.JournalEntryRequest) { r.Reference1 = strings.Repeat("X", 21) },
		"missing ref":   func(r *model.JournalEntryRequest) { r.Reference = " " },
		"zero amount":   func(r *model.JournalEntryRequest) { r.Items[0].Amount = 0; r.Items[1].Amount = 0 },
		"no gl account": func(r *model.JournalEntryRequest) { r.Items[1].GLAccount = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := validJournal()
			mutate(&req)
			err := ValidateJournalEntry(&req)
			var vErr *ValidationError
			if !errors.As(err, &vErr) {
				t.Fatalf("expected ValidationError, got %v", err)
			}
		})
	}
}

func TestBuildJournalEntrySOAP(t *testing.T) {
	req := validJournal()
	if err := ValidateJournalEntry(&req); err != nil {
		t.Fatal(err)
	}
	xmlStr, err := BuildJournalEntrySOAP(req, time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:sfin="http://sap.com/xi/SAPSCORE/SFIN">`,
		"<sfin:JournalEntryBulkCreateRequest>",
		"<CreationDateTime>2026-11-05T08:00:00Z</CreationDateTime>",
		"<CompanyCode>5110</CompanyCode>",
		"<AccountingDocumentType>SA</AccountingDocumentType>",
		`<AmountInTransactionCurrency currencyCode="IDR">1500000.00</AmountInTransactionCurrency>`,
		`<AmountInTransactionCurrency currencyCode="IDR">-1500000.00</AmountInTransactionCurrency>`,
		"<CostCenter>51101101</CostCenter>",
		"Bearing &lt;6204&gt;",
		"<DocumentHeaderText>Pemakaian material &amp; spar</DocumentHeaderText>",
	} {
		if !strings.Contains(xmlStr, want) {
			t.Errorf("SOAP envelope missing %q\n%s", want, xmlStr)
		}
	}
	compact := strings.Join(strings.Fields(xmlStr), "")
	for _, want := range []string{
		"<OriginalReferenceDocument></OriginalReferenceDocument><OriginalReferenceDocumentLogicalSystem></OriginalReferenceDocumentLogicalSystem>",
		"<PostingDate>2026-11-05</PostingDate><Reference1InDocumentHeader>JE-TEST-001</Reference1InDocumentHeader><Item>",
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("SOAP envelope missing %q", want)
		}
	}
	if strings.Contains(xmlStr, "10081") {
		t.Errorf("budget_code must not be sent to SAP")
	}
	// Baris kredit tanpa cost center tidak boleh punya AccountAssignment.
	if strings.Count(xmlStr, "<AccountAssignment>") != 1 {
		t.Errorf("expected exactly 1 AccountAssignment")
	}
}

// elementSequence mengembalikan urutan nama elemen (local name) di dokumen XML.
func elementSequence(t *testing.T, doc string) []string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(doc))
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			out = append(out, se.Name.Local)
		}
	}
	return out
}

// TestBuildJournalEntrySOAP_MatchesBPITemplate memastikan urutan & nama elemen sama persis dengan
// "BPI - Payload API Journal Entry BPI.txt".
func TestBuildJournalEntrySOAP_MatchesBPITemplate(t *testing.T) {
	const template = `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:sfin="http://sap.com/xi/SAPSCORE/SFIN"><soapenv:Header/><soapenv:Body><sfin:JournalEntryBulkCreateRequest>
	<MessageHeader><ID>MSG_2018-10-31</ID><CreationDateTime>2018-10-31T12:00:00.1234567Z</CreationDateTime></MessageHeader>
	<JournalEntryCreateRequest><MessageHeader><ID>SUB_MSG_2018-10-31</ID><CreationDateTime>2018-10-31T12:00:00.1234567Z</CreationDateTime></MessageHeader>
	<JournalEntry><OriginalReferenceDocumentType>BKPFF</OriginalReferenceDocumentType><OriginalReferenceDocument/><OriginalReferenceDocumentLogicalSystem/>
	<BusinessTransactionType>RFBU</BusinessTransactionType><AccountingDocumentType>SA</AccountingDocumentType><DocumentReferenceID>123</DocumentReferenceID>
	<DocumentHeaderText>Test JE Posting</DocumentHeaderText><CreatedByUser>CB9980000003</CreatedByUser><CompanyCode>5110</CompanyCode>
	<DocumentDate>2026-10-28</DocumentDate><PostingDate>2026-10-28</PostingDate><Reference1InDocumentHeader>12345678901234567890</Reference1InDocumentHeader>
	<Reference2InDocumentHeader>1234567890</Reference2InDocumentHeader>
	<Item><ReferenceDocumentItem>1</ReferenceDocumentItem><GLAccount>65003000</GLAccount><AmountInTransactionCurrency currencyCode="USD">1000</AmountInTransactionCurrency><DebitCreditCode>S</DebitCreditCode><DocumentItemText>Test Item 1</DocumentItemText><AccountAssignment><CostCenter>10201</CostCenter></AccountAssignment></Item>
	<Item><ReferenceDocumentItem>2</ReferenceDocumentItem><GLAccount>13100002</GLAccount><AmountInTransactionCurrency currencyCode="USD">-1000</AmountInTransactionCurrency><DebitCreditCode>H</DebitCreditCode><DocumentItemText>Test Item 2</DocumentItemText><AccountAssignment><CostCenter>10201</CostCenter></AccountAssignment></Item>
	</JournalEntry></JournalEntryCreateRequest></sfin:JournalEntryBulkCreateRequest></soapenv:Body></soapenv:Envelope>`

	t.Setenv("FIN_JE_CREATED_BY_USER", "CB9980000003")
	req := model.JournalEntryRequest{
		Reference: "123", CompanyCode: "5110", DocumentDate: "2026-10-28", PostingDate: "2026-10-28", Currency: "USD",
		HeaderText: "Test JE Posting", Reference1: "12345678901234567890", Reference2: "1234567890",
		Items: []model.JournalEntryItem{
			{GLAccount: "65003000", Amount: 1000, DebitCredit: "S", CostCenter: "10201", ItemText: "Test Item 1"},
			{GLAccount: "13100002", Amount: 1000, DebitCredit: "H", CostCenter: "10201", ItemText: "Test Item 2"},
		},
	}
	if err := ValidateJournalEntry(&req); err != nil {
		t.Fatal(err)
	}
	got, err := BuildJournalEntrySOAP(req, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want, have := elementSequence(t, template), elementSequence(t, got)
	if strings.Join(want, ",") != strings.Join(have, ",") {
		t.Errorf("element sequence differs from template:\nwant %v\n got %v", want, have)
	}
	for _, v := range []string{">CB9980000003<", ">12345678901234567890<", ">1234567890<", `currencyCode="USD">-1000.00<`} {
		if !strings.Contains(got, v) {
			t.Errorf("missing %s", v)
		}
	}
}

func TestJournalBudgetLines_OnlyDebitWithBudget(t *testing.T) {
	req := validJournal()
	_ = ValidateJournalEntry(&req)
	req.Items = append(req.Items,
		model.JournalEntryItem{GLAccount: "513043", Amount: 500, DebitCredit: "S", BudgetCode: "10081"},
		model.JournalEntryItem{GLAccount: "513044", Amount: 700, DebitCredit: "S", CostCenter: "51101101", BudgetCode: "10081"})
	lines := journalBudgetLines(req)
	// Kombinasi = [GL account]-[budget code]; cost center tidak ikut menentukan.
	if len(lines) != 2 || lines["513043-10081"] != 1500500 || lines["513044-10081"] != 700 {
		t.Errorf("unexpected budget lines: %v", lines)
	}
}

func TestNormalizePolicyNameAndCIP(t *testing.T) {
	same := []string{"Glasses – Frame & Lense", "glasses - frame & lense", "Glasses � Frame  &  Lense", "GLASSES—Frame & Lense"}
	for _, s := range same {
		if model.NormalizePolicyName(s) != model.NormalizePolicyName(same[0]) {
			t.Errorf("%q normalized to %q, want %q", s, model.NormalizePolicyName(s), model.NormalizePolicyName(same[0]))
		}
	}
	for in, want := range map[string]string{"CIP": "CIP", " cip ": "CIP", "Non CIP": "NON_CIP", "NON-CIP": "NON_CIP", "non_cip": "NON_CIP"} {
		if got, ok := model.NormalizeCIPFlag(in); !ok || got != want {
			t.Errorf("NormalizeCIPFlag(%q) = %q,%v", in, got, ok)
		}
	}
	if _, ok := model.NormalizeCIPFlag("maybe"); ok {
		t.Error("unknown flag must fail")
	}
}

const policyCSV = "InvoiceRef,DocumentDate,PostingDate,InvoicingParty,GrossAmount,PolicyName,CIP,CostCenter,Amount,ItemText\n" +
	"MED-001,2026-11-05,2026-11-05,EMP-CIP,300000,Medical Claim – Outpatient,,51101101,300000,Klinik\n" +
	"BT-002,2026-11-05,2026-11-05,EMP-X,2000000,Domestic,Non CIP,51101101,2000000,Tiket\n" +
	"MED-003,2026-11-05,2026-11-05,EMP-UNKNOWN,100000,Maternity,,,100000,RS\n" +
	"MED-004,2026-11-05,2026-11-05,EMP-CIP,100000,Kacamata Emas,,,100000,X\n"

func TestResolveReimbursementGL(t *testing.T) {
	rows, rowErrs, err := ParseSupplierInvoiceFile("p.csv", []byte(policyCSV))
	if err != nil || len(rowErrs) > 0 {
		t.Fatalf("unexpected parse errors: %v %v", err, rowErrs)
	}
	mapping := map[string][2]string{
		model.NormalizePolicyName("Medical Claim - Outpatient") + "|CIP": {"511006", "10081"},
		model.NormalizePolicyName("Domestic") + "|NON_CIP":               {"522234", "19031"},
		model.NormalizePolicyName("Maternity") + "|CIP":                  {"511006", "10081"},
	}
	vendorCalls := 0
	errs, err := ResolveReimbursementGL(rows,
		func(p, c string) (string, string, bool, error) {
			v, ok := mapping[model.NormalizePolicyName(p)+"|"+c]
			return v[0], v[1], ok, nil
		},
		func(v string) (string, bool, error) {
			vendorCalls++
			if v == "EMP-CIP" {
				return "CIP", true, nil
			}
			return "", false, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].GLAccount != "511006" || rows[0].BudgetCode != "10081" || rows[0].CIPFlag != "CIP" {
		t.Errorf("row 1 not resolved from vendor CIP: %+v", rows[0])
	}
	if rows[1].GLAccount != "522234" || rows[1].BudgetCode != "19031" {
		t.Errorf("row 2 not resolved from CIP column: %+v", rows[1])
	}
	if len(errs) != 2 || errs[0].Row != 4 || errs[0].Field != "InvoicingParty" || errs[1].Row != 5 || errs[1].Field != "PolicyName" {
		t.Errorf("unexpected resolve errors: %+v", errs)
	}
	if vendorCalls != 2 {
		t.Errorf("vendor lookup must be cached per vendor, got %d calls", vendorCalls)
	}
}

func TestParseSupplierInvoiceFile_RequiresGLOrPolicy(t *testing.T) {
	_, _, err := ParseSupplierInvoiceFile("x.csv", []byte("InvoiceRef,DocumentDate,PostingDate,InvoicingParty,GrossAmount,Amount\nA,2026-11-05,2026-11-05,E,1,1\n"))
	if err == nil || !strings.Contains(err.Error(), "glaccount atau policyname") {
		t.Errorf("expected missing GL/policy column error, got %v", err)
	}
	_, rowErrs, err := ParseSupplierInvoiceFile("x.csv", []byte("InvoiceRef,DocumentDate,PostingDate,InvoicingParty,GrossAmount,Amount,PolicyName,CIP\nA,2026-11-05,2026-11-05,E,1,1,,maybe\n"))
	if err != nil || len(rowErrs) != 2 {
		t.Errorf("expected GL-or-policy + CIP row errors, got %v %v", rowErrs, err)
	}
}

func TestParseEmployeeVendorFile(t *testing.T) {
	csv := "Vendor Code,Comment (CIP/Non CIP),Nama\nVND-1,CIP,Andi\nVND-2,Non CIP,Budi\n,CIP,\nVND-3,lainnya,\nVND-1,CIP,dup\n"
	items, errs, err := ParseEmployeeVendorFile("v.csv", []byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[1].CIPFlag != "NON_CIP" || items[0].Description != "Andi" {
		t.Errorf("unexpected items %+v", items)
	}
	if len(errs) != 3 {
		t.Errorf("expected 3 row errors (empty code, bad flag, duplicate), got %+v", errs)
	}
	if _, _, err := ParseEmployeeVendorFile("v.csv", []byte("Vendor,Name\nA,B\n")); err == nil {
		t.Error("missing CIP column must fail")
	}
}

func TestExecuteSOAPDocument_MockPostsAndParses(t *testing.T) {
	svc := &SyncService{SAPClient: client.NewSAPClient("MOCK", "", "")}
	ep := &model.SAPEndpoint{ServiceCode: "JOURNAL_GI", BaseURL: "MOCK", ODataVersion: "soap"}
	payload, _ := json.Marshal("<soapenv:Envelope/>")
	code, docNo, body, err := svc.executeSOAPDocument(ep, payload)
	if err != nil || code != 200 {
		t.Fatalf("unexpected error: %v (code %d)", err, code)
	}
	if !strings.Contains(docNo, "/") || body == "" {
		t.Errorf("expected AccountingDocument/FiscalYear, got %q", docNo)
	}
	if _, _, _, err := svc.executeSOAPDocument(ep, json.RawMessage(`{"not":"string"}`)); err == nil {
		t.Errorf("expected error for non-string SOAP payload")
	}
}

func TestToJSONResponse(t *testing.T) {
	if string(toJSONResponse(`{"a":1}`)) != `{"a":1}` {
		t.Error("valid JSON must pass through")
	}
	var s string
	if err := json.Unmarshal(toJSONResponse(`<xml/>`), &s); err != nil || s != "<xml/>" {
		t.Errorf("XML must be wrapped as JSON string, got %s", toJSONResponse(`<xml/>`))
	}
	if toJSONResponse("  ") != nil {
		t.Error("empty body must be nil")
	}
}

func TestBudgetRefType(t *testing.T) {
	if budgetRefType("AXMXPO") != "PO" || budgetRefType("journal_gi") != "JOURNAL_GI" {
		t.Error("unexpected ref type mapping")
	}
}

// ===================== Pull parsing =====================

func TestParseGLLineItems_V2(t *testing.T) {
	body := []byte(`{"d":{"results":[{
		"__metadata":{"uri":"https://host/sap/opu/odata/sap/API_GLACCOUNTLINEITEM/GLAccountLineItem('.1~0L.2~5110.3~2026.4~1900000010.5~000001')"},
		"CompanyCode":"5110","FiscalYear":"2026","AccountingDocument":"1900000010","LedgerGLLineItem":"000001",
		"AccountingDocumentType":"SA","PostingDate":"/Date(1793836800000)/","DocumentDate":"/Date(1793836800000)/",
		"GLAccount":"513043","CostCenter":"51101101","DebitCreditCode":"S",
		"AmountInCompanyCodeCurrency":"1500000.00","CompanyCodeCurrency":"IDR",
		"AmountInTransactionCurrency":"1500000.00","TransactionCurrency":"IDR","DocumentItemText":"Bearing",
		"to_JournalEntry":{"__deferred":{"uri":"x"}}
	}]}}`)
	items, err := ParseGLLineItems(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	it := items[0]
	if it.Key != ".1~0L.2~5110.3~2026.4~1900000010.5~000001" {
		t.Errorf("unexpected key %q", it.Key)
	}
	if it.PostingDate == nil || *it.PostingDate != "2026-11-05" {
		t.Errorf("unexpected posting date %v", it.PostingDate)
	}
	if it.AmountInCompanyCodeCurrency != 1500000 {
		t.Errorf("unexpected amount %v", it.AmountInCompanyCodeCurrency)
	}
	if strings.Contains(string(it.Raw), "__metadata") || strings.Contains(string(it.Raw), "__deferred") {
		t.Errorf("raw must not contain metadata/deferred: %s", it.Raw)
	}
}

func TestParseFixedAssets_V4(t *testing.T) {
	body := []byte(`{"value":[
		{"CompanyCode":"5110","MasterFixedAsset":"50000000","FixedAsset":"0","FixedAssetDescription":"Pompa sirkulasi","AssetClass":"3000","AssetCapitalizationDate":"2026-01-15"},
		{"CompanyCode":"","MasterFixedAsset":"X"}
	]}`)
	items, err := ParseFixedAssets(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected incomplete record to be skipped, got %d", len(items))
	}
	if items[0].Description != "Pompa sirkulasi" || *items[0].CapitalizationDate != "2026-01-15" {
		t.Errorf("unexpected asset %+v", items[0])
	}
}

func TestParsePaymentProposals_KeyFromMetadata(t *testing.T) {
	body := []byte(`{"d":{"results":[
		{"__metadata":{"uri":"https://h/sap/opu/odata/sap/YY1_PAYMENTPROPOSAL_CDS/YY1_PaymentProposal(guid'abc')"},"PaymentRunID":"F110A","PaymentRunDate":"/Date(1793836800000)/","Supplier":"V001","PaidAmountInPaytCurrency":"250000.50","PaymentCurrency":"IDR"},
		{"PaymentRunID":"F110B"}
	]}}`)
	items, err := ParsePaymentProposals(body)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].RecordKey != "YY1_PaymentProposal(guid'abc')" {
		t.Errorf("unexpected key %q", items[0].RecordKey)
	}
	if items[0].Amount != 250000.5 || items[0].Supplier != "V001" || *items[0].PaymentRunDate != "2026-11-05" {
		t.Errorf("unexpected proposal %+v", items[0])
	}
	if len(items[1].RecordKey) != 40 {
		t.Errorf("record without metadata must get sha1 key, got %q", items[1].RecordKey)
	}
}

func TestBuildGLFilter(t *testing.T) {
	f := BuildGLFilter(GLSyncOptions{CompanyCode: "51'10", FromDate: "2026-11-01", ToDate: "2026-11-05", DocTypes: []string{"SA", "KR"}})
	want := "CompanyCode eq '51''10' and Ledger eq '0L' and PostingDate ge datetime'2026-11-01T00:00:00' and PostingDate le datetime'2026-11-05T23:59:59' and (AccountingDocumentType eq 'SA' or AccountingDocumentType eq 'KR')"
	if f != want {
		t.Errorf("unexpected filter:\n got %s\nwant %s", f, want)
	}
	if odataQueryEscape("a eq 'b'") != "a%20eq%20%27b%27" {
		t.Errorf("unexpected escape %s", odataQueryEscape("a eq 'b'"))
	}
}

func TestDocTypesFor(t *testing.T) {
	t.Setenv("FIN_JE_DOC_TYPES", "sa, ab")
	if got := DocTypesFor("journal"); strings.Join(got, ",") != "SA,AB" {
		t.Errorf("unexpected %v", got)
	}
	if got := DocTypesFor(""); got == nil || len(got) != 0 {
		t.Errorf("unknown category must return empty non-nil slice, got %#v", got)
	}
}

// ===================== RICEF 6 =====================

func TestBuildExchangeRatePayload(t *testing.T) {
	docNo, p, err := BuildExchangeRatePayload(model.ExchangeRateRecord{CurrencyCode: "jpy", Rate: 11000, Unit: 100, ValidFrom: "2026-11-05"})
	if err != nil {
		t.Fatal(err)
	}
	if docNo != "EXR-2026-11-05-JPY" {
		t.Errorf("unexpected doc no %s", docNo)
	}
	if p.SourceCurrency != "JPY" || p.TargetCurrency != "IDR" || p.ExchangeRate != 11000 || p.NumberOfSourceCurrencyUnits != 100 || p.ExchangeRateType != "M" {
		t.Errorf("unexpected payload %+v", p)
	}
	if _, _, err := BuildExchangeRatePayload(model.ExchangeRateRecord{CurrencyCode: "USD", Rate: 0}); err == nil {
		t.Error("zero rate must be rejected")
	}
}

// ===================== RICEF 7 =====================

func TestParseAmountAndDate(t *testing.T) {
	amounts := map[string]float64{"1500000": 1500000, "1.500.000,50": 1500000.5, "1,500,000.50": 1500000.5, "1500000,5": 1500000.5, "Rp 1.500.000": 1500000, "250.75": 250.75, "500.000": 500000, "12.5": 12.5}
	for in, want := range amounts {
		if got, ok := parseAmount(in); !ok || got != want {
			t.Errorf("parseAmount(%q) = %v,%v want %v", in, got, ok, want)
		}
	}
	dates := map[string]string{"2026-11-05": "2026-11-05", "05/11/2026": "2026-11-05", "05-11-2026": "2026-11-05", "46331": "2026-11-05"}
	for in, want := range dates {
		if got, ok := parseFlexibleDate(in); !ok || got != want {
			t.Errorf("parseFlexibleDate(%q) = %v,%v want %v", in, got, ok, want)
		}
	}
	if _, ok := parseFlexibleDate("besok"); ok {
		t.Error("invalid date must fail")
	}
}

const invoiceCSV = "InvoiceRef;DocumentDate;PostingDate;InvoicingParty;GrossAmount;GLAccount;CostCenter;Amount;ItemText\n" +
	"BT-001;05/11/2026;2026-11-05;EMP-1;1.500.000;613100;51101101;1.000.000;Tiket\n" +
	"BT-001;05/11/2026;2026-11-05;EMP-1;1.500.000;613200;51101101;500.000;Uang harian\n" +
	"RB-002;2026-11-05;2026-11-05;EMP-2;250000;613300;;250000;Taksi\n"

func TestParseAndBuildSupplierInvoices_CSV(t *testing.T) {
	rows, rowErrs, err := ParseSupplierInvoiceFile("upload.csv", append([]byte{0xEF, 0xBB, 0xBF}, invoiceCSV...))
	if err != nil || len(rowErrs) > 0 {
		t.Fatalf("unexpected errors: %v %v", err, rowErrs)
	}
	if len(rows) != 3 || rows[0].RowNumber != 2 || rows[0].CompanyCode != "5110" || rows[0].DocType != "" || rows[0].TaxCode != "V0" {
		t.Fatalf("unexpected rows %+v", rows)
	}
	drafts, errs := BuildSupplierInvoices(rows)
	if len(errs) > 0 {
		t.Fatalf("unexpected group errors %v", errs)
	}
	if len(drafts) != 2 || drafts[0].InvoiceRef != "BT-001" {
		t.Fatalf("unexpected drafts %+v", drafts)
	}
	p := drafts[0].Payload
	if p.InvoiceGrossAmount != "1500000.00" || len(p.ToSupplierInvoiceItemGLAcct) != 2 || p.DocumentDate != "2026-11-05T00:00" {
		t.Errorf("unexpected payload %+v", p)
	}
	if p.ToSupplierInvoiceItemGLAcct[1].SupplierInvoiceItem != "2" || p.TaxIsCalculatedAutomatically || p.PaymentTerms != "NT15" || p.SupplierInvoiceStatus != "A" {
		t.Errorf("unexpected payload defaults %+v", p)
	}
}

// keySet mengembalikan kumpulan nama field (rekursif, dengan prefix) dari dokumen JSON.
func keySet(t *testing.T, v interface{}, prefix string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, vv := range x {
			out[prefix+k] = true
			keySet(t, vv, prefix+k+".", out)
		}
	case []interface{}:
		if len(x) > 0 {
			keySet(t, x[0], prefix, out)
		}
	}
}

// TestSupplierInvoicePayload_MatchesBPITemplate membandingkan struktur payload dengan
// "BPI - Payload API BT & Reimbursement.txt" (bagian GL; referensi PO tidak dipakai untuk reimbursement).
func TestSupplierInvoicePayload_MatchesBPITemplate(t *testing.T) {
	template := `{"CompanyCode":"5110","DocumentDate":"2026-10-28T00:00","PostingDate":"2026-10-28T00:00","TaxDeterminationDate":"2026-10-28T00:00",
		"SupplierInvoiceIDByInvcgParty":"VND-5072","InvoicingParty":"VND-5072","DocumentCurrency":"USD","InvoiceGrossAmount":"5520.00",
		"DocumentHeaderText":"PO Invoice + GL Cost","SupplierInvoiceStatus":"A","TaxIsCalculatedAutomatically":false,"PaymentTerms":"NT15",
		"DueCalculationBaseDate":"2026-10-28T00:00",
		"to_SupplierInvoiceItemGLAcct":[{"SupplierInvoiceItem":"1","DocumentCurrency":"USD","SupplierInvoiceItemAmount":"5520.00","DebitCreditCode":"S",
		"GLAccount":"61003000","CostCenter":"51101101","TaxCode":"V0","SupplierInvoiceItemText":"Additional GL Cost","AssignmentReference":"TEST"}]}`
	var tmpl interface{}
	if err := json.Unmarshal([]byte(template), &tmpl); err != nil {
		t.Fatal(err)
	}
	drafts, errs := BuildSupplierInvoices([]model.SupplierInvoiceRow{{
		RowNumber: 2, InvoiceRef: "VND-5072", CompanyCode: "5110", DocumentDate: "2026-10-28", PostingDate: "2026-10-28",
		InvoicingParty: "VND-5072", Currency: "USD", GrossAmount: 5520, HeaderText: "PO Invoice + GL Cost",
		GLAccount: "61003000", CostCenter: "51101101", Amount: 5520, TaxCode: "V0", ItemText: "Additional GL Cost", AssignmentRef: "TEST",
	}})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	b, _ := json.Marshal(drafts[0].Payload)
	var got interface{}
	_ = json.Unmarshal(b, &got)

	want, have := map[string]bool{}, map[string]bool{}
	keySet(t, tmpl, "", want)
	keySet(t, got, "", have)
	for k := range want {
		if !have[k] {
			t.Errorf("payload missing template field %s", k)
		}
	}
	for k := range have {
		if !want[k] {
			t.Errorf("payload has extra field %s not in template", k)
		}
	}
	if string(b) != strings.Join(strings.Fields(template), "") && !jsonEqual(t, b, []byte(template)) {
		t.Errorf("payload values differ from template:\n got %s", b)
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	var x, y interface{}
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}

func TestBuildSupplierInvoices_Mismatch(t *testing.T) {
	csv := strings.Replace(invoiceCSV, "Uang harian", "Uang harian", 1)
	csv = strings.Replace(csv, "613200;51101101;500.000", "613200;51101101;400.000", 1)
	rows, _, err := ParseSupplierInvoiceFile("x.csv", []byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	_, errs := BuildSupplierInvoices(rows)
	if len(errs) != 1 || errs[0].Field != "GrossAmount" {
		t.Errorf("expected gross mismatch error, got %v", errs)
	}
}

func TestParseSupplierInvoiceFile_RowErrorsAndMissingColumns(t *testing.T) {
	bad := "InvoiceRef,DocumentDate,PostingDate,InvoicingParty,GrossAmount,GLAccount,Amount\n" +
		",2026-13-40,2026-11-05,,abc,,0\n"
	_, rowErrs, err := ParseSupplierInvoiceFile("x.csv", []byte(bad))
	if err != nil {
		t.Fatal(err)
	}
	if len(rowErrs) < 6 {
		t.Errorf("expected many row errors, got %v", rowErrs)
	}
	_, _, err = ParseSupplierInvoiceFile("x.csv", []byte("InvoiceRef,Amount\nA,1\n"))
	var vErr *ValidationError
	if !errors.As(err, &vErr) || !strings.Contains(err.Error(), "glaccount") {
		t.Errorf("expected missing column error, got %v", err)
	}
	if _, _, err := ParseSupplierInvoiceFile("x.pdf", nil); err == nil {
		t.Error("unsupported extension must fail")
	}
}

// buildXLSX membuat file xlsx minimal (shared string + inline string + angka) untuk pengujian.
func buildXLSX(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	write("xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	write("xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="worksheet" Target="worksheets/data.xml"/></Relationships>`)
	write("xl/sharedStrings.xml", `<sst><si><t>InvoiceRef</t></si><si><t>DocumentDate</t></si><si><t>PostingDate</t></si><si><t>InvoicingParty</t></si><si><t>GrossAmount</t></si><si><t>GLAccount</t></si><si><t>Amount</t></si><si><r><t>BT-</t></r><r><t>XL1</t></r></si></sst>`)
	write("xl/worksheets/data.xml", `<worksheet><sheetData>
		<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c><c r="E1" t="s"><v>4</v></c><c r="F1" t="s"><v>5</v></c><c r="G1" t="s"><v>6</v></c></row>
		<row r="3"><c r="A3" t="s"><v>7</v></c><c r="B3"><v>46331</v></c><c r="C3"><v>46331</v></c><c r="D3" t="inlineStr"><is><t>EMP-9</t></is></c><c r="E3"><v>750000</v></c><c r="F3" t="inlineStr"><is><t>613100</t></is></c><c r="G3"><v>750000</v></c></row>
	</sheetData></worksheet>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseSupplierInvoiceFile_XLSX(t *testing.T) {
	rows, rowErrs, err := ParseSupplierInvoiceFile("upload.XLSX", buildXLSX(t))
	if err != nil || len(rowErrs) > 0 {
		t.Fatalf("unexpected errors: %v %v", err, rowErrs)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.RowNumber != 3 || r.InvoiceRef != "BT-XL1" || r.DocumentDate != "2026-11-05" || r.InvoicingParty != "EMP-9" || r.Amount != 750000 {
		t.Errorf("unexpected row %+v", r)
	}
}

func TestColumnIndex(t *testing.T) {
	if columnIndex("A1") != 0 || columnIndex("Z9") != 25 || columnIndex("AB12") != 27 {
		t.Error("unexpected column index")
	}
}

// ===================== RICEF 8 =====================

func TestValidateAssetAcquisition(t *testing.T) {
	req := model.AssetAcquisitionRequest{Reference: "AQ-1", MasterFixedAsset: "50000000", DocumentDate: "2026-11-05", PostingDate: "2026-11-05",
		Amount: 1000, OffsettingAccount: "199999", ParkedBy: "maker"}
	if err := ValidateAssetAcquisition(&req); err != nil {
		t.Fatal(err)
	}
	if req.FixedAsset != "0" || req.CompanyCode != "5110" || req.AssetValueDate != "2026-11-05" || req.Currency != "IDR" {
		t.Errorf("defaults not applied: %+v", req)
	}
	bad := model.AssetAcquisitionRequest{Reference: "AQ-2", DocumentDate: "x"}
	err := ValidateAssetAcquisition(&bad)
	var vErr *ValidationError
	if !errors.As(err, &vErr) || len(vErr.Messages) < 5 {
		t.Errorf("expected multiple validation errors, got %v", err)
	}
}

func TestBuildAssetAcquisitionPayload(t *testing.T) {
	p := BuildAssetAcquisitionPayload(model.AssetAcquisition{Reference: "AQ-2026-000000001", CompanyCode: "5110", MasterFixedAsset: "50000000", FixedAsset: "0",
		DocumentDate: "2026-11-05", PostingDate: "2026-11-05", AssetValueDate: "2026-11-01", Amount: 1234.567, Currency: "IDR", OffsettingAccount: "199999"})
	if p.AcqnAmtInTransactionCurrency != 1234.57 || p.DocumentReferenceID != "AQ-2026-00000000" || p.AssetValueDate != "2026-11-01" {
		t.Errorf("unexpected payload %+v", p)
	}
}

func TestTruncateRunesIsUTF8Safe(t *testing.T) {
	if got := truncateRunes("Pengadaan é-catalog", 13); got != "Pengadaan é-c" {
		t.Errorf("unexpected %q", got)
	}
}
