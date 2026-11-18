package service

import (
	"os"
	"strings"
	"testing"
	"time"

	"golang-bpi-testing/internal/repository"
)

func TestNormalizeTalentaFiles_BothFiles(t *testing.T) {
	file1 := `C:/Users/muham/.gemini/antigravity/brain/ae390c5d-9da1-458c-aabe-35e88570f562/.user_uploaded/media_1794896965065_0e09e10e.xlsx`
	data1, err := os.ReadFile(file1)
	if err != nil {
		t.Fatal(err)
	}
	table1, err := readXLSX(data1)
	if err != nil {
		t.Fatal(err)
	}
	out1, ok1 := NormalizeTalentaTable(table1, time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC))
	t.Logf("File 1 Normalize ok=%v, rows=%d", ok1, len(out1))
	for i := 0; i < len(out1) && i < 10; i++ {
		t.Logf("Out1 R%d: %v", i, out1[i])
	}

	file2 := `C:/Users/muham/.gemini/antigravity/brain/ae390c5d-9da1-458c-aabe-35e88570f562/.user_uploaded/media_1794896965065_836339b1.xlsx`
	data2, err := os.ReadFile(file2)
	if err != nil {
		t.Fatal(err)
	}
	table2, err := readXLSX(data2)
	if err != nil {
		t.Fatal(err)
	}
	out2, ok2 := NormalizeTalentaTable(table2, time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC))
	t.Logf("File 2 Normalize ok=%v, rows=%d", ok2, len(out2))
	for i := 0; i < len(out2) && i < 10; i++ {
		t.Logf("Out2 R%d: %v", i, out2[i])
	}

	rows1, rowErrs1, err1 := ParseSupplierInvoiceFile("Reimb.xlsx", data1)
	if err1 != nil || len(rowErrs1) > 0 {
		t.Fatalf("File 1 ParseSupplierInvoiceFile: len=%d, errs=%v, err=%v", len(rows1), rowErrs1, err1)
	}

	rows2, rowErrs2, err2 := ParseSupplierInvoiceFile("BT.xlsx", data2)
	if err2 != nil || len(rowErrs2) > 0 {
		t.Fatalf("File 2 ParseSupplierInvoiceFile: len=%d, errs=%v, err=%v", len(rows2), rowErrs2, err2)
	}

	mockGL := func(policy, cip string) (string, string, bool, error) {
		for _, m := range repository.DefaultReimbursementMappings {
			if strings.EqualFold(m.PolicyName, policy) && strings.EqualFold(m.CIPFlag, cip) {
				return m.GLAccount, m.BudgetCode, true, nil
			}
		}
		return "", "", false, nil
	}
	mockCIP := func(vendor string) (string, bool, error) {
		return "NON_CIP", true, nil
	}

	glErrs1, err := ResolveReimbursementGL(rows1, mockGL, mockCIP)
	if err != nil || len(glErrs1) > 0 {
		t.Fatalf("File 1 ResolveReimbursementGL failed: %v, errs: %+v", err, glErrs1)
	}
	drafts1, groupErrs1 := BuildSupplierInvoices(rows1)
	if len(groupErrs1) > 0 {
		t.Fatalf("File 1 BuildSupplierInvoices errors: %+v", groupErrs1)
	}
	t.Logf("File 1 total drafts: %d", len(drafts1))
	for _, d := range drafts1 {
		t.Logf("Draft Reimb: Ref=%s InvoicingParty=%s Gross=%s GL=%s CC=%s Text=%s Assign=%s",
			d.InvoiceRef, d.Payload.InvoicingParty, d.Payload.InvoiceGrossAmount,
			d.Payload.ToSupplierInvoiceItemGLAcct[0].GLAccount,
			d.Payload.ToSupplierInvoiceItemGLAcct[0].CostCenter,
			d.Payload.ToSupplierInvoiceItemGLAcct[0].SupplierInvoiceItemText,
			d.Payload.ToSupplierInvoiceItemGLAcct[0].AssignmentReference)
	}

	glErrs2, err := ResolveReimbursementGL(rows2, mockGL, mockCIP)
	if err != nil || len(glErrs2) > 0 {
		t.Fatalf("File 2 ResolveReimbursementGL failed: %v, errs: %+v", err, glErrs2)
	}
	drafts2, groupErrs2 := BuildSupplierInvoices(rows2)
	if len(groupErrs2) > 0 {
		t.Fatalf("File 2 BuildSupplierInvoices errors: %+v", groupErrs2)
	}
	t.Logf("File 2 total drafts: %d", len(drafts2))
	for _, d := range drafts2 {
		t.Logf("Draft BT: Ref=%s InvoicingParty=%s Gross=%s GL=%s CC=%s Text=%s Assign=%s",
			d.InvoiceRef, d.Payload.InvoicingParty, d.Payload.InvoiceGrossAmount,
			d.Payload.ToSupplierInvoiceItemGLAcct[0].GLAccount,
			d.Payload.ToSupplierInvoiceItemGLAcct[0].CostCenter,
			d.Payload.ToSupplierInvoiceItemGLAcct[0].SupplierInvoiceItemText,
			d.Payload.ToSupplierInvoiceItemGLAcct[0].AssignmentReference)
	}
}

func TestNormalizeTalentaReimbursement_HTMLXLS(t *testing.T) {
	t.Setenv("FIN_TALENTA_INVOICING_PARTY", "VND-0061")
	t.Setenv("FIN_COMPANY_CODE", "")
	xls := `<html><body><table>
<tr><th>Transaction ID</th><th>Employee ID</th><th>Reimbursement Policy Name</th><th>Reimbursement Effective Date</th><th>Total Reimbursement Paid Amount</th><th>Reimbursement Notes</th>
<th>Benefit Name 1</th><th>Paid Amount 1</th><th>Description 1</th><th>Benefit Name 2</th><th>Paid Amount 2</th><th>Description 2</th><th>Final Status</th></tr>
<tr><td>20260900079</td><td>5072</td><td>Medical Claim - Outpatient</td><td>2026-10-08</td><td>Rp 350.000</td><td>Rawat jalan</td>
<td>Rawat Jalan Pratama</td><td>Rp 350.000</td><td>Klinik</td><td></td><td>0</td><td></td><td>Approved</td></tr>
<tr><td>20260900080</td><td>5073</td><td>Medical Claim - Outpatient</td><td>2026-10-08</td><td>Rp 100.000</td><td>x</td>
<td>A</td><td>Rp 100.000</td><td>B</td><td></td><td>0</td><td></td><td>Rejected</td></tr>
</table></body></html>`
	table, err := readTable("Reimburhsment 1008.xls", []byte(xls))
	if err != nil {
		t.Fatal(err)
	}
	out, ok := NormalizeTalentaTable(table, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))
	if !ok || len(out) != 2 {
		t.Fatalf("harus 1 baris approved, dapat ok=%v rows=%v", ok, out)
	}
	got := strings.Join(out[1], ",")
	want := "20260900079,1000,2026-10-08,2026-11-10,5072,IDR,350000,20260900079,Z014,Medical Claim - Outpatient,,,350000,V0,Klinik,Rawat Jalan Pratama,ZB,REIMBURSEMENT,5072,"
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestNormalizeTalentaBusinessTrip_ParsesEndToEnd(t *testing.T) {
	t.Setenv("FIN_TALENTA_INVOICING_PARTY", "VND-0061")
	csv := "Trip ID,Employee ID (Requested By),Trip Type,Trip Purpose,Destination,Request Date,Departure Date,Approved Expense Amount,Approval Report Date,Trip Status\n" +
		"BT20260955986,5072,Domestic,Koordinasi Stakeholder,Semarang,2026-09-15,2026-09-20,Rp 1.500.000,2026-10-08,Approved\n"
	rows, rowErrs, err := ParseSupplierInvoiceFile("Bussiness Trip BT1008.csv", []byte(csv))
	if err != nil || len(rowErrs) > 0 {
		t.Fatalf("err=%v rowErrs=%+v", err, rowErrs)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	r := rows[0]
	if r.InvoiceRef != "BT20260955986" || r.InvoicingParty != "VND-0061" || r.GrossAmount != 1500000 || r.PolicyName != "Domestic" || r.DocumentDate != "2026-10-08" {
		t.Fatalf("hasil salah: %+v", r)
	}
}

func TestNormalizeTalenta_TemplateUntouched(t *testing.T) {
	if _, ok := NormalizeTalentaTable([][]string{templateReimbursementHeader}, time.Now()); ok {
		t.Fatal("template tidak boleh dinormalisasi ulang")
	}
}

func TestReadLegacyXLS_BinaryRejected(t *testing.T) {
	if _, err := readTable("x.xls", []byte{0xD0, 0xCF, 0x11, 0xE0}); err == nil {
		t.Fatal("BIFF harus ditolak dengan pesan jelas")
	}
}
