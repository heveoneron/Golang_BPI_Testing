package service

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RICEF 7: normalisasi file ekspor Mekari Talenta (Reimbursement & Business Trip) ke format template_reimbursement.csv.

var templateReimbursementHeader = []string{"InvoiceRef", "CompanyCode", "DocumentDate", "PostingDate", "InvoicingParty", "Currency",
	"GrossAmount", "HeaderText", "PaymentTerms", "PolicyName", "CIP", "CostCenter", "Amount", "TaxCode", "ItemText", "AssignmentRef"}

var (
	htmlRowRe  = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	htmlCellRe = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	htmlTagRe  = regexp.MustCompile(`(?s)<[^>]+>`)
)

// readLegacyXLS membaca file .xls Talenta: sebenarnya xlsx (zip) atau tabel HTML. BIFF biner tidak didukung.
func readLegacyXLS(data []byte) ([][]string, error) {
	if bytes.HasPrefix(data, []byte("PK")) {
		return readXLSX(data)
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	if len(trimmed) > 0 && trimmed[0] == '<' {
		var table [][]string
		for _, row := range htmlRowRe.FindAllSubmatch(trimmed, -1) {
			var cells []string
			for _, c := range htmlCellRe.FindAllSubmatch(row[1], -1) {
				cells = append(cells, strings.TrimSpace(html.UnescapeString(htmlTagRe.ReplaceAllString(string(c[1]), ""))))
			}
			table = append(table, cells)
		}
		if len(table) == 0 {
			return nil, fmt.Errorf("tabel tidak ditemukan di file .xls")
		}
		return table, nil
	}
	return nil, fmt.Errorf("file .xls biner (Excel 97-2003) belum didukung; buka di Excel lalu simpan sebagai .xlsx atau .csv")
}

func talentaKey(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(h) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func talentaAmount(s string) float64 {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "Rp"))
	s = strings.ReplaceAll(s, " ", "")
	if strings.Count(s, ".") > 1 && !strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
	}
	v, _ := parseAmount(s)
	return v
}

func fmtAmount(v float64) string { return strconv.FormatFloat(round2(v), 'f', -1, 64) }

func talentaDate(s string) string {
	if d, ok := parseFlexibleDate(strings.TrimSpace(s)); ok {
		return d
	}
	if len(s) >= 10 {
		if d, ok := parseFlexibleDate(s[:10]); ok {
			return d
		}
	}
	return strings.TrimSpace(s)
}

// NormalizeTalentaTable mendeteksi format ekspor Talenta dan mengubahnya menjadi tabel template.
// ok=false bila tabel bukan format Talenta (misalnya sudah berupa template), sehingga dipakai apa adanya.
func NormalizeTalentaTable(table [][]string, today time.Time) ([][]string, bool) {
	headerRow, kind := -1, ""
	var idx map[string]int
	for i := 0; i < len(table) && i < 15; i++ {
		m := map[string]int{}
		for c, h := range table[i] {
			if k := talentaKey(h); k != "" {
				if _, dup := m[k]; !dup {
					m[k] = c
				}
			}
		}
		if _, ok := m["invoiceref"]; ok {
			return nil, false
		}
		_, hasTx := m["transactionid"]
		_, hasPolicy := m["reimbursementpolicyname"]
		_, hasTrip := m["tripid"]
		if hasTx && hasPolicy {
			headerRow, kind, idx = i, "REIMB", m
			break
		}
		if hasTrip {
			headerRow, kind, idx = i, "BT", m
			break
		}
	}
	if headerRow < 0 {
		return nil, false
	}

	get := func(r []string, keys ...string) string {
		for _, k := range keys {
			if c, ok := idx[k]; ok && c < len(r) {
				if v := strings.TrimSpace(r[c]); v != "" {
					return v
				}
			}
		}
		return ""
	}
	company := DefaultCompanyCode()
	terms := envOr("FIN_INVOICE_PAYMENT_TERMS", "NT15")
	tax := envOr("FIN_INVOICE_DEFAULT_TAX_CODE", "V0")
	// Employee ID tidak dikirim ke SAP; semua klaim memakai satu vendor penampung yang dikonfigurasi.
	vendor := envOr("FIN_TALENTA_INVOICING_PARTY", "")
	posting := today.Format("2006-01-02")

	out := [][]string{templateReimbursementHeader}
	for i := headerRow + 1; i < len(table); i++ {
		r := table[i]
		if strings.TrimSpace(strings.Join(r, "")) == "" {
			continue
		}
		status := get(r, "finalstatus", "finalstatustransaksi", "tripstatus", "status", "transactionstatus")
		if status != "" && !strings.EqualFold(status, "approved") {
			continue
		}
		// Kosong = Cost Center diisi Budget Code dari mapping reimbursement saat resolusi GL.
		cc := get(r, "costcenter")
		line := func(ref, docDate, gross, header, policy, amount, text string) []string {
			return []string{ref, company, docDate, posting, vendor, "IDR", gross, truncate(header, 50), terms, policy, "", cc, amount, tax, truncate(text, 50), ref}
		}

		if kind == "REIMB" {
			ref := get(r, "transactionid")
			policy := get(r, "reimbursementpolicyname")
			docDate := talentaDate(get(r, "reimbursementeffectivedate", "effectivedate", "date"))
			notes := firstNonEmpty(get(r, "reimbursementnotes", "notes"), policy)
			var lines [][2]string
			sum := 0.0
			for n := 1; n <= 7; n++ {
				s := strconv.Itoa(n)
				amt := talentaAmount(get(r, "paidamount"+s, "reimbursementpaidamount"+s))
				if amt <= 0 {
					continue
				}
				sum += amt
				lines = append(lines, [2]string{fmtAmount(amt), firstNonEmpty(get(r, "description"+s, "reimbursementdescription"+s), get(r, "benefitname"+s), policy)})
			}
			gross := talentaAmount(get(r, "totalreimbursementpaidamount", "totalpaidamount"))
			if gross <= 0 {
				gross = sum
			}
			if len(lines) == 0 && gross > 0 {
				lines = append(lines, [2]string{fmtAmount(gross), notes})
			}
			for _, l := range lines {
				out = append(out, line(ref, docDate, fmtAmount(gross), notes, policy, l[0], l[1]))
			}
			continue
		}

		ref := get(r, "tripid")
		amount := talentaAmount(get(r, "approvedexpenseamount"))
		if amount <= 0 {
			continue
		}
		docDate := talentaDate(firstNonEmpty(get(r, "approvalreportdate"), get(r, "requestdate")))
		header := strings.TrimSpace(get(r, "destination") + " " + get(r, "departuredate"))
		out = append(out, line(ref, docDate, fmtAmount(amount), firstNonEmpty(header, "Perjalanan dinas"),
			get(r, "triptype"), fmtAmount(amount), firstNonEmpty(get(r, "trippurpose"), "Perjalanan dinas")))
	}
	return out, true
}
