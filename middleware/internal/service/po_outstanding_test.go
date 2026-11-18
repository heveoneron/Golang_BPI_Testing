package service

import (
	"encoding/json"
	"strings"
	"testing"

	"golang-bpi-testing/internal/model"
)

// Header persis seperti "Template PO Outstanding and AX.xlsx" (dengan catatan dalam kurung & baris baru).
const poTemplateCSV = "currencycode;orderdate;DP Percentage;ponum;siteid;vendor;bpibudgetcode;" +
	"pol_desc_ld (ditaruh di short text dan item text) short text bakal kepotong;itemnum;orderqty;orderunit;" +
	"\"polinenum (tidak perlu berurutan\n1;3;7)\";taxcode1;unitcost (harga satuan)\r\n"

func TestParsePOOutstandingFile(t *testing.T) {
	csv := poTemplateCSV +
		"IDR;45299;20;4500010135;BTNG;VND-2721;16011;Data Communication Subscription;92-00016;3.00;MONTH;1;11PPN;3410000\r\n" +
		"IDR;45299;20;4500010135;BTNG;VND-2721;16011;Data Communication Subscription;92-00016;3.00;MONTH;3;11PPN;7272727.00\r\n"
	lines, errs, err := ParsePOOutstandingFile("po.csv", []byte(csv), "maximo")
	if err != nil || len(errs) != 0 {
		t.Fatalf("err=%v errs=%+v", err, errs)
	}
	if len(lines) != 2 || lines[0].OrderDate != "2024-01-08" || lines[1].POLineNum != 3 || lines[0].DPPercentage != 20 ||
		lines[1].UnitCost != 7272727 || lines[0].SourceSystem != "MAXIMO" || lines[0].POLDescLD != "Data Communication Subscription" {
		t.Fatalf("parsed wrong: %+v", lines)
	}
	raw, err := poOutstandingRaw(lines)
	if err != nil {
		t.Fatal(err)
	}
	var po model.MaximoPOHeader
	_ = json.Unmarshal(raw, &po)
	if po.PONum != "4500010135" || len(po.Lines) != 2 || po.DownPaymentType != "V" || po.Lines[1].TaxCodePPN != "11PPN" {
		t.Fatalf("raw PO wrong: %s", raw)
	}
}

func TestParsePOOutstandingFile_Validation(t *testing.T) {
	csv := poTemplateCSV +
		"IDR;2025-01-01;0;4500010999;BTNG;VND-1;16011;X;92-1;1;EA;1;;100\r\n" + // bukan < 2025
		"IDR;2024-05-01;0;4500010998;BTNG;VND-1;16011;X;92-1;0;EA;1;;100\r\n" + // qty 0
		"IDR;2024-05-01;0;4500010998;BTNG;VND-2;16011;X;92-1;1;EA;1;;100\r\n" // duplikat line + vendor beda
	_, errs, err := ParsePOOutstandingFile("po.csv", []byte(csv), "AX")
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, e := range errs {
		joined += e.Field + ":" + e.Message + "|"
	}
	for _, want := range []string{"orderdate:2025-01-01 bukan PO < 2025", "orderqty:", "polinenum:duplikat", "vendor:berbeda"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing error %q in %s", want, joined)
		}
	}
	if _, _, err := ParsePOOutstandingFile("po.csv", []byte(csv), "SAP"); err == nil {
		t.Error("source_system selain MAXIMO/AX harus ditolak")
	}
}
