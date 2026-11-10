package service

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeTalentaReimbursement_HTMLXLS(t *testing.T) {
	t.Setenv("FIN_TALENTA_DEFAULT_COST_CENTER", "51101101")
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
	want := "20260900079,5110,2026-10-08,2026-11-10,VND-5072,IDR,350000,Rawat jalan,NT15,Medical Claim - Outpatient,,51101101,350000,V0,Klinik,20260900079"
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestNormalizeTalentaBusinessTrip_ParsesEndToEnd(t *testing.T) {
	t.Setenv("FIN_TALENTA_DEFAULT_COST_CENTER", "51101101")
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
	if r.InvoiceRef != "BT20260955986" || r.InvoicingParty != "VND-5072" || r.GrossAmount != 1500000 || r.PolicyName != "Domestic" || r.DocumentDate != "2026-10-08" {
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
