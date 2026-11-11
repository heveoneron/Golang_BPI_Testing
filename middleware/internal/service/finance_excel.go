package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
)

// maxUploadRows membatasi ukuran satu file upload.
const maxUploadRows = 5000

// ===================== Pembaca XLSX minimal (tanpa dependensi eksternal) =====================

type xlsxSST struct {
	Items []struct {
		T string `xml:"t"`
		R []struct {
			T string `xml:"t"`
		} `xml:"r"`
	} `xml:"si"`
}

type xlsxSheet struct {
	Rows []struct {
		R     int `xml:"r,attr"`
		Cells []struct {
			Ref string `xml:"r,attr"`
			T   string `xml:"t,attr"`
			V   string `xml:"v"`
			IS  struct {
				T string `xml:"t"`
				R []struct {
					T string `xml:"t"`
				} `xml:"r"`
			} `xml:"is"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

type xlsxWorkbook struct {
	Sheets []struct {
		RID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	} `xml:"sheets>sheet"`
}

type xlsxRels struct {
	Rels []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}

func readZipFile(files map[string]*zip.File, name string) ([]byte, error) {
	f, ok := files[name]
	if !ok {
		return nil, fmt.Errorf("bagian %s tidak ada di file xlsx", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 64<<20))
}

// columnIndex mengubah referensi sel (mis. "AB12") menjadi indeks kolom berbasis 0.
func columnIndex(ref string) int {
	idx := 0
	for _, c := range ref {
		if c >= 'A' && c <= 'Z' {
			idx = idx*26 + int(c-'A'+1)
		} else if c >= 'a' && c <= 'z' {
			idx = idx*26 + int(c-'a'+1)
		} else {
			break
		}
	}
	return idx - 1
}

// readXLSX membaca sheet pertama sebagai baris string. Nilai numerik dikembalikan apa adanya
// (tanggal Excel tetap berupa nomor seri dan diolah oleh parseFlexibleDate).
func readXLSX(data []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("file bukan xlsx yang valid: %w", err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}

	var shared []string
	if b, err := readZipFile(files, "xl/sharedStrings.xml"); err == nil {
		var sst xlsxSST
		if err := xml.Unmarshal(b, &sst); err != nil {
			return nil, fmt.Errorf("sharedStrings tidak valid: %w", err)
		}
		for _, si := range sst.Items {
			s := si.T
			for _, r := range si.R {
				s += r.T
			}
			shared = append(shared, s)
		}
	}

	sheetPath := "xl/worksheets/sheet1.xml"
	if wb, err := readZipFile(files, "xl/workbook.xml"); err == nil {
		var w xlsxWorkbook
		if xml.Unmarshal(wb, &w) == nil && len(w.Sheets) > 0 {
			if rb, err := readZipFile(files, "xl/_rels/workbook.xml.rels"); err == nil {
				var rels xlsxRels
				if xml.Unmarshal(rb, &rels) == nil {
					for _, rel := range rels.Rels {
						if rel.ID == w.Sheets[0].RID {
							t := strings.TrimPrefix(rel.Target, "/")
							if !strings.HasPrefix(t, "xl/") {
								t = path.Join("xl", t)
							}
							sheetPath = t
						}
					}
				}
			}
		}
	}

	sb, err := readZipFile(files, sheetPath)
	if err != nil {
		return nil, err
	}
	var sheet xlsxSheet
	if err := xml.Unmarshal(sb, &sheet); err != nil {
		return nil, fmt.Errorf("sheet tidak valid: %w", err)
	}

	var rows [][]string
	for _, r := range sheet.Rows {
		var row []string
		for i, c := range r.Cells {
			col := i
			if c.Ref != "" {
				col = columnIndex(c.Ref)
			}
			if col < 0 {
				continue
			}
			for len(row) <= col {
				row = append(row, "")
			}
			val := c.V
			switch c.T {
			case "s":
				if n, err := strconv.Atoi(strings.TrimSpace(c.V)); err == nil && n >= 0 && n < len(shared) {
					val = shared[n]
				}
			case "inlineStr":
				val = c.IS.T
				for _, rr := range c.IS.R {
					val += rr.T
				}
			case "b":
				if c.V == "1" {
					val = "TRUE"
				} else {
					val = "FALSE"
				}
			}
			row[col] = strings.TrimSpace(val)
		}
		// Sisipkan baris kosong agar nomor baris tetap sesuai dengan Excel.
		for r.R > 0 && len(rows) < r.R-1 {
			rows = append(rows, nil)
		}
		rows = append(rows, row)
		if len(rows) > maxUploadRows+1 {
			return nil, fmt.Errorf("file melebihi batas %d baris", maxUploadRows)
		}
	}
	return rows, nil
}

func readCSV(data []byte) ([][]string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	firstLine := string(data)
	if i := strings.IndexAny(firstLine, "\r\n"); i >= 0 {
		firstLine = firstLine[:i]
	}
	r := csv.NewReader(bytes.NewReader(data))
	if strings.Count(firstLine, ";") > strings.Count(firstLine, ",") {
		r.Comma = ';'
	}
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV tidak valid: %w", err)
	}
	if len(rows) > maxUploadRows+1 {
		return nil, fmt.Errorf("file melebihi batas %d baris", maxUploadRows)
	}
	return rows, nil
}

// ===================== Konversi nilai =====================

// parseFlexibleDate menerima YYYY-MM-DD, DD/MM/YYYY, DD-MM-YYYY, atau nomor seri tanggal Excel.
func parseFlexibleDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006", "2/1/2006", "02-01-2006", "2006/01/02", "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 20000 && f < 80000 {
		base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
		return base.AddDate(0, 0, int(f)).Format("2006-01-02"), true
	}
	return "", false
}

// parseAmount menerima 1500000, 1500000.50, 1.500.000,50, 1,500,000.50.
func parseAmount(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
	s = strings.TrimPrefix(strings.TrimPrefix(s, "Rp"), "IDR")
	if s == "" {
		return 0, false
	}
	lastDot, lastComma := strings.LastIndex(s, "."), strings.LastIndex(s, ",")
	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastComma > lastDot { // 1.500.000,50
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else { // 1,500,000.50
			s = strings.ReplaceAll(s, ",", "")
		}
	case lastComma >= 0:
		if len(s)-lastComma-1 <= 2 && strings.Count(s, ",") == 1 { // 1500000,50
			s = strings.Replace(s, ",", ".", 1)
		} else { // 1,500,000
			s = strings.ReplaceAll(s, ",", "")
		}
	case lastDot >= 0 && strings.Count(s, ".") > 1: // 1.500.000
		s = strings.ReplaceAll(s, ".", "")
	case lastDot >= 0 && len(s)-lastDot-1 == 3: // 500.000 (ribuan format Indonesia), bukan 500,000 desimal
		s = strings.ReplaceAll(s, ".", "")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func normalizeHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	return strings.NewReplacer(" ", "", "_", "", "-", "", ".", "").Replace(h)
}

// ===================== RICEF 7: parsing & validasi =====================

// GLAccount tidak wajib bila file memakai kolom nama kebijakan (GL ditentukan oleh mapping).
var invoiceRequiredColumns = []string{"invoiceref", "documentdate", "postingdate", "invoicingparty", "grossamount", "amount"}

// Alias header kolom nama kebijakan & status CIP (setelah normalizeHeader).
var policyHeaderAliases = []string{"policyname", "reimpolicename", "reimbursepolicy", "reimbursementpolicy", "policy"}
var cipHeaderAliases = []string{"cip", "cipflag", "cipnoncip", "comment(cip/noncip)"}

// readTable membaca .xlsx / .csv menjadi baris string.
func readTable(fileName string, data []byte) ([][]string, error) {
	switch strings.ToLower(path.Ext(fileName)) {
	case ".xlsx":
		return readXLSX(data)
	case ".csv":
		return readCSV(data)
	case ".xls":
		return readLegacyXLS(data)
	}
	return nil, fmt.Errorf("format file harus .xlsx, .xls (ekspor Talenta) atau .csv")
}

// headerIndex mencari baris header pertama yang tidak kosong dan memetakan nama kolom ternormalisasi.
func headerIndex(table [][]string) (int, map[string]int) {
	for i, r := range table {
		if strings.TrimSpace(strings.Join(r, "")) != "" {
			idx := map[string]int{}
			for c, h := range r {
				idx[normalizeHeader(h)] = c
			}
			return i, idx
		}
	}
	return -1, nil
}

func firstColumn(colIdx map[string]int, aliases []string) string {
	for _, a := range aliases {
		if _, ok := colIdx[a]; ok {
			return a
		}
	}
	return ""
}

// ParseSupplierInvoiceFile membaca file .xlsx / .csv menjadi baris supplier invoice beserta error per baris.
func ParseSupplierInvoiceFile(fileName string, data []byte) ([]model.SupplierInvoiceRow, []model.UploadRowError, error) {
	table, err := readTable(fileName, data)
	if err != nil {
		return nil, nil, newValidation(err.Error())
	}
	if normalized, ok := NormalizeTalentaTable(table, time.Now()); ok {
		table = normalized
	}
	headerRow, colIdx := headerIndex(table)
	if headerRow < 0 {
		return nil, nil, newValidation("file kosong")
	}
	var missing []string
	for _, c := range invoiceRequiredColumns {
		if _, ok := colIdx[c]; !ok {
			missing = append(missing, c)
		}
	}
	policyCol := firstColumn(colIdx, policyHeaderAliases)
	cipCol := firstColumn(colIdx, cipHeaderAliases)
	if _, ok := colIdx["glaccount"]; !ok && policyCol == "" {
		missing = append(missing, "glaccount atau policyname")
	}
	if len(missing) > 0 {
		return nil, nil, newValidation("kolom wajib tidak ditemukan: " + strings.Join(missing, ", "))
	}

	get := func(r []string, col string) string {
		if i, ok := colIdx[col]; ok && i < len(r) {
			return strings.TrimSpace(r[i])
		}
		return ""
	}

	var rows []model.SupplierInvoiceRow
	var rowErrs []model.UploadRowError
	for i := headerRow + 1; i < len(table); i++ {
		r := table[i]
		if strings.TrimSpace(strings.Join(r, "")) == "" {
			continue
		}
		rowNo := i + 1
		addErr := func(field, msg string) {
			rowErrs = append(rowErrs, model.UploadRowError{Row: rowNo, Field: field, Message: msg})
		}
		row := model.SupplierInvoiceRow{
			RowNumber:      rowNo,
			InvoiceRef:     get(r, "invoiceref"),
			CompanyCode:    get(r, "companycode"),
			InvoicingParty: get(r, "invoicingparty"),
			Currency:       strings.ToUpper(get(r, "currency")),
			HeaderText:     get(r, "headertext"),
			DocType:        strings.ToUpper(get(r, "doctype")),
			GLAccount:      get(r, "glaccount"),
			CostCenter:     get(r, "costcenter"),
			TaxCode:        strings.ToUpper(get(r, "taxcode")),
			ItemText:       get(r, "itemtext"),
		}
		if row.CompanyCode == "" {
			row.CompanyCode = DefaultCompanyCode()
		}
		if row.Currency == "" {
			row.Currency = "IDR"
		}
		if policyCol != "" {
			row.PolicyName = get(r, policyCol)
		}
		if cipCol != "" {
			if raw := get(r, cipCol); raw != "" {
				if flag, ok := model.NormalizeCIPFlag(raw); ok {
					row.CIPFlag = flag
				} else {
					addErr("CIP", "harus CIP atau Non CIP")
				}
			}
		}
		row.AssignmentRef = get(r, "assignmentref")
		if row.AssignmentRef == "" {
			row.AssignmentRef = get(r, "assignmentreference")
		}
		row.PaymentTerms = strings.ToUpper(get(r, "paymentterms"))
		if row.TaxCode == "" {
			row.TaxCode = envOr("FIN_INVOICE_DEFAULT_TAX_CODE", "V0")
		}
		if row.InvoiceRef == "" {
			addErr("InvoiceRef", "wajib diisi")
		} else if len(row.InvoiceRef) > 16 {
			addErr("InvoiceRef", "maksimal 16 karakter (batas SupplierInvoiceIDByInvcgParty)")
		}
		if row.InvoicingParty == "" {
			addErr("InvoicingParty", "wajib diisi")
		}
		if row.GLAccount == "" && row.PolicyName == "" {
			addErr("GLAccount", "wajib diisi bila PolicyName kosong")
		}
		if d, ok := parseFlexibleDate(get(r, "documentdate")); ok {
			row.DocumentDate = d
		} else {
			addErr("DocumentDate", "tanggal tidak valid")
		}
		if d, ok := parseFlexibleDate(get(r, "postingdate")); ok {
			row.PostingDate = d
		} else {
			addErr("PostingDate", "tanggal tidak valid")
		}
		if v, ok := parseAmount(get(r, "grossamount")); ok && v > 0 {
			row.GrossAmount = round2(v)
		} else {
			addErr("GrossAmount", "harus angka lebih dari 0")
		}
		if v, ok := parseAmount(get(r, "amount")); ok && v > 0 {
			row.Amount = round2(v)
		} else {
			addErr("Amount", "harus angka lebih dari 0")
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, nil, newValidation("tidak ada baris data di bawah header")
	}
	return rows, rowErrs, nil
}

// GLLookup mencari GL & budget code dari kebijakan + CIP; VendorCIPLookup mencari status CIP vendor.
type GLLookup func(policyName, cipFlag string) (gl, budgetCode string, found bool, err error)
type VendorCIPLookup func(vendorCode string) (cipFlag string, found bool, err error)

// ResolveReimbursementGL mengisi GLAccount & BudgetCode untuk baris yang memakai PolicyName.
// Status CIP diambil dari kolom CIP, atau dari mapping vendor karyawan (InvoicingParty).
// Baris yang sudah berisi GLAccount dibiarkan (GL eksplisit diutamakan).
func ResolveReimbursementGL(rows []model.SupplierInvoiceRow, glLookup GLLookup, cipLookup VendorCIPLookup) ([]model.UploadRowError, error) {
	var errs []model.UploadRowError
	cipCache := map[string]string{}
	for i := range rows {
		r := &rows[i]
		if r.PolicyName == "" || r.GLAccount != "" {
			continue
		}
		flag := r.CIPFlag
		if flag == "" {
			if cached, ok := cipCache[r.InvoicingParty]; ok {
				flag = cached
			} else {
				f, found, err := cipLookup(r.InvoicingParty)
				if err != nil {
					return nil, err
				}
				if found {
					flag = f
				}
				cipCache[r.InvoicingParty] = flag
			}
		}
		if flag == "" {
			errs = append(errs, model.UploadRowError{Row: r.RowNumber, Field: "InvoicingParty",
				Message: fmt.Sprintf("vendor karyawan %s belum terdaftar CIP/Non CIP; isi kolom CIP atau daftarkan di mapping vendor karyawan", r.InvoicingParty)})
			continue
		}
		r.CIPFlag = flag
		gl, bc, found, err := glLookup(r.PolicyName, flag)
		if err != nil {
			return nil, err
		}
		if !found {
			errs = append(errs, model.UploadRowError{Row: r.RowNumber, Field: "PolicyName",
				Message: fmt.Sprintf("kebijakan %q (%s) tidak ada di mapping GL reimbursement", r.PolicyName, flag)})
			continue
		}
		r.GLAccount, r.BudgetCode = gl, bc
		if r.CostCenter == "" {
			// Kolom "Budget Code / Cost Center" di mapping reimbursement dipakai sebagai Cost Center SAP.
			r.CostCenter = bc
		}
	}
	return errs, nil
}

// ParseEmployeeVendorFile membaca daftar vendor karyawan (.xlsx / .csv) dengan kolom
// VendorCode dan CIP (atau "Comment (CIP/Non CIP)"), opsional Description.
func ParseEmployeeVendorFile(fileName string, data []byte) ([]model.EmployeeVendorMapping, []model.UploadRowError, error) {
	table, err := readTable(fileName, data)
	if err != nil {
		return nil, nil, newValidation(err.Error())
	}
	headerRow, colIdx := headerIndex(table)
	if headerRow < 0 {
		return nil, nil, newValidation("file kosong")
	}
	vendorCol := firstColumn(colIdx, []string{"vendorcode", "vendor", "invoicingparty"})
	cipCol := firstColumn(colIdx, cipHeaderAliases)
	if vendorCol == "" || cipCol == "" {
		return nil, nil, newValidation("kolom wajib: Vendor Code dan CIP / Comment (CIP/Non CIP)")
	}
	descCol := firstColumn(colIdx, []string{"description", "name", "nama"})
	get := func(r []string, col string) string {
		if i, ok := colIdx[col]; ok && i < len(r) {
			return strings.TrimSpace(r[i])
		}
		return ""
	}
	var out []model.EmployeeVendorMapping
	var errs []model.UploadRowError
	seen := map[string]int{}
	for i := headerRow + 1; i < len(table); i++ {
		r := table[i]
		if strings.TrimSpace(strings.Join(r, "")) == "" {
			continue
		}
		code := get(r, vendorCol)
		flag, ok := model.NormalizeCIPFlag(get(r, cipCol))
		switch {
		case code == "":
			errs = append(errs, model.UploadRowError{Row: i + 1, Field: "VendorCode", Message: "wajib diisi"})
		case !ok:
			errs = append(errs, model.UploadRowError{Row: i + 1, Field: "CIP", Message: "harus CIP atau Non CIP"})
		case seen[code] > 0:
			errs = append(errs, model.UploadRowError{Row: i + 1, Field: "VendorCode", Message: fmt.Sprintf("duplikat dengan baris %d", seen[code])})
		default:
			seen[code] = i + 1
			desc := ""
			if descCol != "" {
				desc = get(r, descCol)
			}
			out = append(out, model.EmployeeVendorMapping{VendorCode: code, CIPFlag: flag, Description: desc, IsActive: true})
		}
	}
	return out, errs, nil
}

// ImportEmployeeVendors memvalidasi seluruh file lalu menyimpan; satu baris salah = tidak ada yang disimpan.
func (f *FinanceService) ImportEmployeeVendors(ctx context.Context, fileName string, data []byte) (int, []model.UploadRowError, error) {
	items, errs, err := ParseEmployeeVendorFile(fileName, data)
	if err != nil {
		return 0, nil, err
	}
	if len(errs) > 0 {
		return 0, errs, nil
	}
	n, err := f.PGRepo.UpsertEmployeeVendors(ctx, items)
	return n, nil, err
}

// SupplierInvoiceDraft adalah satu invoice hasil penggabungan baris per InvoiceRef.
type SupplierInvoiceDraft struct {
	InvoiceRef string
	Rows       []model.SupplierInvoiceRow
	Payload    model.SAPSupplierInvoicePayload
}

// isZeroTaxCode bernilai true untuk kode pajak nol (default V0) yang tidak memerlukan hitung pajak otomatis.
func isZeroTaxCode(code string) bool {
	return code == "" || strings.EqualFold(code, envOr("FIN_INVOICE_DEFAULT_TAX_CODE", "V0"))
}

// sapDateTime memformat tanggal seperti template BPI ("2026-10-28T00:00").
func sapDateTime(d string) string {
	return d + "T00:00"
}

// BuildSupplierInvoices menggabungkan baris per InvoiceRef, memeriksa konsistensi header dan total,
// lalu membentuk payload A_SupplierInvoice sesuai template "BPI - Payload API BT & Reimbursement.txt".
func BuildSupplierInvoices(rows []model.SupplierInvoiceRow) ([]SupplierInvoiceDraft, []model.UploadRowError) {
	groups := map[string][]model.SupplierInvoiceRow{}
	var order []string
	for _, r := range rows {
		if _, ok := groups[r.InvoiceRef]; !ok {
			order = append(order, r.InvoiceRef)
		}
		groups[r.InvoiceRef] = append(groups[r.InvoiceRef], r)
	}
	sort.Strings(order)

	var drafts []SupplierInvoiceDraft
	var errs []model.UploadRowError
	for _, ref := range order {
		g := groups[ref]
		h := g[0]
		var sum float64
		hasTax := false
		items := make([]model.SAPSupplierInvoiceGLAcctItem, 0, len(g))
		for i, r := range g {
			if r.CompanyCode != h.CompanyCode || r.InvoicingParty != h.InvoicingParty || r.Currency != h.Currency ||
				r.DocumentDate != h.DocumentDate || r.PostingDate != h.PostingDate || r.GrossAmount != h.GrossAmount ||
				r.DocType != h.DocType || r.PaymentTerms != h.PaymentTerms {
				errs = append(errs, model.UploadRowError{Row: r.RowNumber, Field: "InvoiceRef",
					Message: fmt.Sprintf("kolom header (CompanyCode, InvoicingParty, Currency, tanggal, GrossAmount, DocType, PaymentTerms) berbeda dengan baris %d untuk invoice %s", h.RowNumber, ref)})
			}
			sum += r.Amount
			if !isZeroTaxCode(r.TaxCode) {
				hasTax = true
			}
			items = append(items, model.SAPSupplierInvoiceGLAcctItem{
				SupplierInvoiceItem:       fmt.Sprintf("%d", i+1),
				DocumentCurrency:          r.Currency,
				SupplierInvoiceItemAmount: fmt.Sprintf("%.2f", r.Amount),
				DebitCreditCode:           "S",
				GLAccount:                 r.GLAccount,
				CostCenter:                r.CostCenter,
				TaxCode:                   r.TaxCode,
				SupplierInvoiceItemText:   truncateRunes(r.ItemText, 50),
				AssignmentReference:       truncateRunes(r.AssignmentRef, 18),
			})
		}
		sum = round2(sum)
		if !hasTax && sum != h.GrossAmount {
			errs = append(errs, model.UploadRowError{Row: h.RowNumber, Field: "GrossAmount",
				Message: fmt.Sprintf("GrossAmount %.2f tidak sama dengan jumlah Amount %.2f untuk invoice %s", h.GrossAmount, sum, ref)})
		}
		if hasTax && sum > h.GrossAmount {
			errs = append(errs, model.UploadRowError{Row: h.RowNumber, Field: "GrossAmount",
				Message: fmt.Sprintf("jumlah Amount %.2f melebihi GrossAmount %.2f untuk invoice %s", sum, h.GrossAmount, ref)})
		}
		drafts = append(drafts, SupplierInvoiceDraft{
			InvoiceRef: ref,
			Rows:       g,
			Payload: model.SAPSupplierInvoicePayload{
				CompanyCode:                   h.CompanyCode,
				DocumentDate:                  sapDateTime(h.DocumentDate),
				PostingDate:                   sapDateTime(h.PostingDate),
				TaxDeterminationDate:          sapDateTime(h.DocumentDate),
				SupplierInvoiceIDByInvcgParty: ref,
				InvoicingParty:                h.InvoicingParty,
				DocumentCurrency:              h.Currency,
				InvoiceGrossAmount:            fmt.Sprintf("%.2f", h.GrossAmount),
				DocumentHeaderText:            truncateRunes(h.HeaderText, 25),
				SupplierInvoiceStatus:         envOr("FIN_INVOICE_STATUS", "A"),
				AccountingDocumentType:        h.DocType,
				TaxIsCalculatedAutomatically:  hasTax,
				PaymentTerms:                  firstNonEmpty(h.PaymentTerms, envOr("FIN_INVOICE_PAYMENT_TERMS", "NT15")),
				DueCalculationBaseDate:        sapDateTime(h.DocumentDate),
				ToSupplierInvoiceItemGLAcct:   items,
			},
		})
	}
	return drafts, errs
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// UploadResult adalah hasil upload satu file supplier invoice.
type UploadResult struct {
	Batch    model.ExcelUploadBatch `json:"batch"`
	Invoices []string               `json:"invoices,omitempty"`
}

// UploadSupplierInvoices memvalidasi seluruh file lalu mengantrekan invoice. Satu baris tidak valid
// membuat seluruh file ditolak (batch REJECTED) agar tidak ada upload setengah jadi.
func (f *FinanceService) UploadSupplierInvoices(ctx context.Context, fileName, uploadedBy string, data []byte) (*UploadResult, error) {
	rows, rowErrs, err := ParseSupplierInvoiceFile(fileName, data)
	if err != nil {
		return nil, err
	}
	glErrs, err := ResolveReimbursementGL(rows,
		func(p, c string) (string, string, bool, error) { return f.PGRepo.LookupReimbursementGL(ctx, p, c) },
		func(v string) (string, bool, error) { return f.PGRepo.LookupEmployeeVendorCIP(ctx, v) })
	if err != nil {
		return nil, err
	}
	rowErrs = append(rowErrs, glErrs...)
	drafts, groupErrs := BuildSupplierInvoices(rows)
	rowErrs = append(rowErrs, groupErrs...)

	for _, d := range drafts {
		status, found, err := f.PGRepo.GetDocumentStatus(ctx, model.DocTypeSupplierInvoice, d.InvoiceRef)
		if err != nil {
			return nil, err
		}
		if found && (status == "success" || status == "processing") {
			rowErrs = append(rowErrs, model.UploadRowError{Row: d.Rows[0].RowNumber, Field: "InvoiceRef",
				Message: fmt.Sprintf("invoice %s sudah %s di SAP / sedang dikirim", d.InvoiceRef, status)})
		}
	}
	sort.SliceStable(rowErrs, func(i, j int) bool { return rowErrs[i].Row < rowErrs[j].Row })

	batch := model.ExcelUploadBatch{FileName: fileName, UploadedBy: uploadedBy, TotalRows: len(rows), TotalInvoices: len(drafts)}
	if len(rowErrs) > 0 {
		batch.Status = "REJECTED"
		batch.Errors = rowErrs
		_ = f.PGRepo.InsertUploadBatch(ctx, &batch)
		return &UploadResult{Batch: batch}, nil
	}

	res := &UploadResult{}
	for _, d := range drafts {
		raw, _ := json.Marshal(map[string]interface{}{"file": fileName, "uploaded_by": uploadedBy, "rows": d.Rows})
		sap, _ := json.Marshal(d.Payload)
		err := f.PGRepo.QueueDocument(ctx, model.Document{
			DocType: model.DocTypeSupplierInvoice, DocumentNo: d.InvoiceRef, RawPayload: raw, SAPPayload: sap,
		})
		if err != nil {
			if errors.Is(err, repository.ErrAlreadyPosted) {
				batch.Errors = append(batch.Errors, model.UploadRowError{Row: d.Rows[0].RowNumber, Field: "InvoiceRef", Message: err.Error()})
				continue
			}
			return nil, err
		}
		res.Invoices = append(res.Invoices, d.InvoiceRef)
		_ = f.PGRepo.InsertIntegrationLog(ctx, "GRP-SUPPLIER_INVOICE-"+d.InvoiceRef, "TALENTA/MEKARI", "MIDDLEWARE", model.DocTypeSupplierInvoice, d.InvoiceRef, "RECEIVED", "",
			fmt.Sprintf("Supplier invoice dari %s (%d baris) diantre oleh %s", fileName, len(d.Rows), uploadedBy))
	}
	batch.Status = "QUEUED"
	_ = f.PGRepo.InsertUploadBatch(ctx, &batch)
	res.Batch = batch
	return res, nil
}

// SupplierInvoiceView adalah ringkasan monitoring invoice hasil upload.
type SupplierInvoiceView struct {
	InvoiceRef     string  `json:"invoice_ref"`
	InvoicingParty string  `json:"invoicing_party"`
	DocumentDate   string  `json:"document_date"`
	GrossAmount    string  `json:"gross_amount"`
	Currency       string  `json:"currency"`
	Status         string  `json:"status"`
	SAPDocumentNo  *string `json:"sap_document_no"`
	ErrorMessage   *string `json:"error_message"`
	UpdatedAt      string  `json:"updated_at"`
}

func (f *FinanceService) ListSupplierInvoices(ctx context.Context, limit int) ([]SupplierInvoiceView, error) {
	docs, err := f.PGRepo.ListDocumentsByType(ctx, model.DocTypeSupplierInvoice, limit)
	if err != nil {
		return nil, err
	}
	out := make([]SupplierInvoiceView, 0, len(docs))
	for _, d := range docs {
		var p model.SAPSupplierInvoicePayload
		_ = json.Unmarshal(d.SAPPayload, &p)
		out = append(out, SupplierInvoiceView{
			InvoiceRef:     d.DocumentNo,
			InvoicingParty: p.InvoicingParty,
			DocumentDate:   strings.SplitN(p.DocumentDate, "T", 2)[0],
			GrossAmount:    p.InvoiceGrossAmount,
			Currency:       p.DocumentCurrency,
			Status:         d.Status,
			SAPDocumentNo:  d.SAPDocumentNo,
			ErrorMessage:   d.ErrorMessage,
			UpdatedAt:      d.UpdatedAt.Format(time.RFC3339),
		})
	}
	return out, nil
}
