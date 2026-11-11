package service

import (
	"strings"
	"testing"

	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
)

func glItem(doc, line, docType, gl, cc, pc, supplier, dc string, amount float64, raw string, text string) model.GLLineItem {
	d := "2026-11-05"
	dd := "2026-11-03"
	return model.GLLineItem{Key: doc + line, CompanyCode: "1000", FiscalYear: "2026", AccountingDocument: doc, LedgerGLLineItem: line,
		AccountingDocumentType: docType, PostingDate: &d, DocumentDate: &dd, GLAccount: gl, CostCenter: cc, ProfitCenter: pc,
		Supplier: supplier, DebitCreditCode: dc, AmountInCompanyCodeCurrency: amount, CompanyCodeCurrency: "IDR",
		DocumentItemText: text, Raw: []byte(raw)}
}

func TestBuildMxInvoiceFromGL_InvoiceSheet(t *testing.T) {
	items := []model.GLLineItem{
		// Baris AP (supplier terisi) = sumber vendor, nilai, teks, assignment & GL.
		glItem("5100000123", "000002", "RE", "211001", "", "P1000", "VND-3035", "H", -16037400,
			`{"AccountingDocumentItem":"002","AmountInGlobalCurrency":"-1000.50","GlobalCurrency":"USD","AssignmentReference":"038/RI/VI/24","WithholdingTaxCode":"23"}`, "Impact Roller"),
		// Baris beban/GR-IR (supplier kosong) = sumber ITEMNUM, quantity & PO.
		glItem("5100000123", "000001", "RE", "114001", "43071", "P1000", "", "S", 16037400,
			`{"AccountingDocumentItem":"001","AmountInGlobalCurrency":"1000.50","GlobalCurrency":"USD","Quantity":"5","PurchasingDocument":"4500011147","PurchasingDocumentItem":"00010"}`, "Idler"),
	}
	msg, err := BuildMxInvoiceFromGL(repository.MaximoSourceInvoice, items)
	if err != nil {
		t.Fatal(err)
	}
	if msg.SourceKey != "1000/2026/5100000123" || len(msg.Rows) != 1 {
		t.Fatalf("unexpected %+v", msg)
	}
	r := msg.Rows[0]
	if r.InvoiceType != "INVOICE" || r.Vendor != "VND-3035" || r.InvoiceLineNum != 2 || r.ItemNum != "1" ||
		r.LineCost != 1000.5 || r.InvoiceQty != 5 || r.UnitCost != 200.1 || r.CurrencyCode != "USD" {
		t.Errorf("amount/line mapping wrong: %+v", r)
	}
	if r.PONum != "4500011147" || r.POLineNum == nil || *r.POLineNum != 10 || r.TaxCode2 != "23" || r.TaxCode1 != "" {
		t.Errorf("PO/tax mapping wrong: %+v", r)
	}
	if r.SiteID != "P1000" || r.BudgetCode != "43071" || r.BudgetNum != "430712026" || r.CostLineNum != "5100000123" || r.TransSeq != 0 {
		t.Errorf("site/budget/costline mapping wrong: %+v", r)
	}
	if r.BPIGRNNumber != "038/RI/VI/24" || r.GLDebitAcct != "211001" || r.InvoiceDesc != "Impact Roller" || r.Status != "POSTED" ||
		r.Paid != 0 || r.DueDate != nil || r.PaidDate.Format("2006-01-02") != "2026-11-05" || r.InvoiceDate.Format("2006-01-02") != "2026-11-03" {
		t.Errorf("header mapping wrong: %+v", r)
	}
	if len(msg.Warnings) != 1 || !strings.Contains(msg.Warnings[0], "INVOICENUM") {
		t.Errorf("expected INVOICENUM length warning only: %v", msg.Warnings)
	}
}

func TestBuildMxInvoiceFromGL_JournalSheet(t *testing.T) {
	items := []model.GLLineItem{
		glItem("10000012", "000002", "SA", "141000", "", "", "", "H", -1500, `{"AmountInGlobalCurrency":"-150"}`, ""),
		glItem("10000012", "000001", "SA", "513043", "10081", "P2000", "", "S", 1000, `{"AmountInGlobalCurrency":"100","GlobalCurrency":"USD"}`, "Bearing"),
		glItem("10000012", "000003", "SA", "513044", "10081", "", "", "S", 500, `{"AmountInGlobalCurrency":"50"}`, ""),
	}
	msg, err := BuildMxInvoiceFromGL(repository.MaximoSourceJournal, items)
	if err != nil {
		t.Fatal(err)
	}
	r := msg.Rows[0]
	if r.InvoiceType != "JOURNAL" || r.LineCost != 150 || r.UnitCost != 150 || r.InvoiceQty != 1 || r.InvoiceLineNum != 1 ||
		r.ItemNum != "1" || r.GLDebitAcct != "513043" || r.BudgetCode != "10081" || r.BudgetNum != "100812026" ||
		r.SiteID != "P2000" || r.Vendor != "" || r.InvoiceDesc != "Bearing" || r.CostLineNum != "10000012" || r.TransSeq != 0 || r.CurrencyCode != "USD" {
		t.Errorf("unexpected journal row %+v", r)
	}
	if len(msg.Warnings) != 0 {
		t.Errorf("no warnings expected: %v", msg.Warnings)
	}
}

func TestBuildMxInvoiceFromGL_Errors(t *testing.T) {
	noAP := []model.GLLineItem{glItem("1", "1", "KR", "516044", "60001", "P1", "", "S", 1, `{}`, "")}
	if _, err := BuildMxInvoiceFromGL(repository.MaximoSourceInvoice, noAP); err == nil {
		t.Error("invoice without supplier line must fail")
	}
	noCC := []model.GLLineItem{glItem("2", "1", "SA", "516044", "", "P1", "", "S", 1, `{}`, "")}
	if _, err := BuildMxInvoiceFromGL(repository.MaximoSourceJournal, noCC); err == nil {
		t.Error("journal without cost center line must fail")
	}
	msg, err := BuildMxInvoiceFromGL(repository.MaximoSourceInvoice, []model.GLLineItem{glItem("3", "1", "KR", "211001", "", "", "V1", "H", -10, `{}`, "")})
	if err != nil || msg.Rows[0].SiteID != "BTNG" || msg.Rows[0].LineCost != 10 || len(msg.Warnings) != 2 {
		t.Errorf("fallbacks wrong: %+v %v", msg, err)
	}
}

func TestMxInvoiceInsertSQL_MatchesMaximoColumns(t *testing.T) {
	sql := repository.MxInvoiceInsertSQL()
	// Kolom dbo.mxinvoice sesuai SELECT yang diberikan user (33 kolom, urutan sama).
	want := "currencycode, invoicedesc, invoicetype, duedate, glpostdate, invoicedate, invoicenum, paid, paiddate, siteid, status, vendor, budgetcode, budgetnum, bpichargecode, bpichargevalue, itemdesc, bpigrnnumber, invoicelinenum, invoiceqty, itemnum, linecost, linetype, polinenum, ponum, positeid, taxcode1, taxcode2, unitcost, costlinenum, gldebitacct, transid, transseq"
	if !strings.Contains(sql, "("+want+")") {
		t.Errorf("column list differs:\n%s", sql)
	}
	if !strings.Contains(sql, "@p33)") || strings.Contains(sql, "@p34") {
		t.Errorf("expected exactly 33 parameters: %s", sql)
	}
}
