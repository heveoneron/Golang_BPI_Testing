package service

import (
	"strings"
	"testing"

	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
)

// sapInvoiceJSON meniru response A_SupplierInvoice (OData V2) dengan $expand item PO, item GL, dan WHT.
const sapInvoiceJSON = `{"d":{"results":[{
	"__metadata":{"uri":"x"},
	"SupplierInvoice":"5105600101","FiscalYear":"2026","CompanyCode":"5110",
	"DocumentDate":"/Date(1793232000000)/","PostingDate":"/Date(1793836800000)/","DueCalculationBaseDate":"/Date(1793836800000)/",
	"InvoicingParty":"VND-3035","DocumentCurrency":"IDR","InvoiceGrossAmount":"160374000.00",
	"DocumentHeaderText":"Rollent Indonesia: Impact Roller",
	"to_SuplrInvcItemPurOrdRef":{"results":[
		{"SupplierInvoiceItem":"000002","PurchaseOrder":"4500011147","PurchaseOrderItem":"2","SupplierInvoiceItemAmount":"107670000.00","QuantityInPurchaseOrderUnit":"111","SupplierInvoiceItemText":"IDLER, IMPACT; DIA: 159","ReferenceDocument":"5000000097"},
		{"SupplierInvoiceItem":"000001","PurchaseOrder":"4500011147","PurchaseOrderItem":"1","SupplierInvoiceItemAmount":"52704000.00","QuantityInPurchaseOrderUnit":"72","SupplierInvoiceItemText":"IDLER, IMPACT; DIA: 133","ReferenceDocument":"5000000096"}
	]},
	"to_SupplierInvoiceItemGLAcct":{"results":[
		{"SupplierInvoiceItem":"3","GLAccount":"516044","CostCenter":"60001","SupplierInvoiceItemAmount":"25000.00","DebitCreditCode":"S","SupplierInvoiceItemText":"Biaya kirim","AssignmentReference":"DO-1"}
	]},
	"to_SupplierInvoiceWhldgTax":{"results":[{"WithholdingTaxType":"P2","WithholdingTaxCode":"23"}]}
}]}}`

func TestParseAndBuildMxInvoiceFromSupplierInvoice(t *testing.T) {
	recs, err := ParseSAPSupplierInvoices([]byte(sapInvoiceJSON))
	if err != nil || len(recs) != 1 {
		t.Fatalf("parse: %v %d", err, len(recs))
	}
	if recs[0].SupplierInvoice != "5105600101" || *recs[0].PostingDate != "2026-11-05" {
		t.Errorf("unexpected record %+v", recs[0])
	}
	lk := InvoiceLookups{
		POLine: func(po string, line int) (*repository.POLineInfo, error) {
			if po == "4500011147" && line == 1 {
				return &repository.POLineInfo{ItemNum: "68-00045", BudgetCode: "43071", MainAccount: "114001"}, nil
			}
			return nil, nil
		},
		GRN: func(md string) (string, error) {
			if md == "5000000096" {
				return "038/RI/VI/24", nil
			}
			return "", nil
		},
	}
	msg, err := BuildMxInvoiceFromSupplierInvoice(recs[0].Raw, lk)
	if err != nil {
		t.Fatal(err)
	}
	if msg.SourceType != "INVOICE" || msg.SourceKey != "5105600101/2026" || len(msg.Rows) != 3 {
		t.Fatalf("unexpected message %+v", msg)
	}
	r1, r2, r3 := msg.Rows[0], msg.Rows[1], msg.Rows[2]
	// Baris PO diurutkan per nomor item SAP.
	if r1.InvoiceLineNum != 1 || *r1.POLineNum != 1 || r1.ItemNum != "68-00045" || r1.BudgetCode != "43071" || r1.GLDebitAcct != "114001-43071" {
		t.Errorf("row1 mapping wrong: %+v", r1)
	}
	if r1.InvoiceQty != 72 || r1.UnitCost != 732000 || r1.LineCost != 52704000 || r1.BPIGRNNumber != "038/RI/VI/24" {
		t.Errorf("row1 amounts/GRN wrong: %+v", r1)
	}
	if r1.InvoiceNum != "5105600101" || r1.Vendor != "VND-3035" || r1.Status != "POSTED" || r1.InvoiceType != "INVOICE" ||
		r1.BudgetNum != "2026" || r1.SiteID != "BTNG" || r1.POSiteID != "BTNG" || r1.TaxCode1 != "" || r1.TaxCode2 != "23" ||
		r1.LineType != "SERVICE" || r1.CostLineNum != 1 || r1.TransSeq != 1 || r1.Paid != 0 {
		t.Errorf("row1 header fields wrong: %+v", r1)
	}
	if r1.InvoiceDesc != "Rollent Indonesia: Impact Roller" || r1.ItemDesc != "IDLER, IMPACT; DIA: 133" {
		t.Errorf("row1 descriptions wrong: %+v", r1)
	}
	if r1.GLPostDate.Format("2006-01-02") != "2026-11-05" || r1.InvoiceDate.Format("2006-01-02") != "2026-10-29" || r1.PaidDate.Format("2006-01-02") != "2026-11-05" {
		t.Errorf("row1 dates wrong: post %v inv %v paid %v", r1.GLPostDate, r1.InvoiceDate, r1.PaidDate)
	}
	if r2.ItemNum != "" || r2.BPIGRNNumber != "5000000097" {
		t.Errorf("row2 without lookup must keep SAP matdoc: %+v", r2)
	}
	if len(msg.Warnings) != 1 || !strings.Contains(msg.Warnings[0], "line 2") {
		t.Errorf("expected warning for unknown PO line 2: %v", msg.Warnings)
	}
	if r3.InvoiceLineNum != 3 || r3.POLineNum != nil || r3.PONum != "" || r3.InvoiceQty != 1 || r3.UnitCost != 25000 ||
		r3.BudgetCode != "60001" || r3.GLDebitAcct != "516044-60001" || r3.BPIGRNNumber != "DO-1" {
		t.Errorf("GL row mapping wrong: %+v", r3)
	}
}

func TestBuildMxInvoiceFromJournal(t *testing.T) {
	d := "2026-11-05"
	items := []model.GLLineItem{
		{CompanyCode: "5110", FiscalYear: "2026", AccountingDocument: "100000123", LedgerGLLineItem: "000002", DebitCreditCode: "H", GLAccount: "141000", AmountInTransactionCurrency: -1500, TransactionCurrency: "IDR", PostingDate: &d},
		{CompanyCode: "5110", FiscalYear: "2026", AccountingDocument: "100000123", LedgerGLLineItem: "000001", DebitCreditCode: "S", GLAccount: "513043", CostCenter: "10081", AmountInTransactionCurrency: 1000, TransactionCurrency: "IDR", DocumentItemText: "Bearing", PostingDate: &d},
		{CompanyCode: "5110", FiscalYear: "2026", AccountingDocument: "100000123", LedgerGLLineItem: "000003", DebitCreditCode: "S", GLAccount: "513044", AmountInTransactionCurrency: 500, TransactionCurrency: "IDR", PostingDate: &d},
	}
	msg, err := BuildMxInvoiceFromJournal(items)
	if err != nil {
		t.Fatal(err)
	}
	if msg.SourceKey != "5110/2026/100000123" || len(msg.Rows) != 1 {
		t.Fatalf("unexpected %+v", msg)
	}
	r := msg.Rows[0]
	if r.InvoiceType != "JOURNAL" || r.LineCost != 1500 || r.UnitCost != 1500 || r.InvoiceQty != 1 || r.InvoiceLineNum != 1 ||
		r.GLDebitAcct != "513043-10081" || r.BudgetCode != "10081" || r.ItemDesc != "Bearing" || r.InvoiceNum != "100000123" || r.DueDate == nil {
		t.Errorf("unexpected journal row %+v", r)
	}
}

func TestBuildSupplierInvoiceFilter(t *testing.T) {
	got := BuildSupplierInvoiceFilter(InvoicePullOptions{CompanyCode: "5110", FromDate: "2026-11-01", ToDate: "2026-11-05"}, "5")
	want := "CompanyCode eq '5110' and PostingDate ge datetime'2026-11-01T00:00:00' and PostingDate le datetime'2026-11-05T23:59:59' and SupplierInvoiceStatus eq '5'"
	if got != want {
		t.Errorf("got %s", got)
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
