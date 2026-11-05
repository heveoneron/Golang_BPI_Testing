package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
)

// MaximoInvoiceWriter menulis satu dokumen (beberapa baris) ke dbo.mxinvoice dan mengembalikan transid.
type MaximoInvoiceWriter interface {
	WriteMxInvoiceMessage(ctx context.Context, rows []model.MxInvoiceRow) ([]int64, error)
}

const syncIDSupplierInvoice = "MAXIMO_SUPPLIER_INVOICE"

// Ekspansi navigasi yang dibutuhkan untuk membentuk baris mxinvoice.
const supplierInvoiceExpand = "to_SuplrInvcItemPurOrdRef,to_SupplierInvoiceItemGLAcct,to_SupplierInvoiceWhldgTax"

func toDate(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return nil
	}
	return &t
}

// navResults mengambil daftar record navigasi OData V2 ({"results":[...]}) atau array langsung.
func navResults(m map[string]interface{}, key string) []map[string]interface{} {
	var arr []interface{}
	switch v := m[key].(type) {
	case map[string]interface{}:
		arr, _ = v["results"].([]interface{})
	case []interface{}:
		arr = v
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, it := range arr {
		if mm, ok := it.(map[string]interface{}); ok {
			out = append(out, mm)
		}
	}
	return out
}

func itemNo(m map[string]interface{}) int {
	n, _ := strconv.Atoi(strings.TrimLeft(str(m, "SupplierInvoiceItem"), "0"))
	return n
}

func round2f(v float64) float64 { return math.Round(v*100) / 100 }

// ParseSAPSupplierInvoices memetakan response A_SupplierInvoice (dengan $expand) ke record simpan.
func ParseSAPSupplierInvoices(body []byte) ([]repository.SAPSupplierInvoiceRecord, error) {
	recs, err := odataRecords(body)
	if err != nil {
		return nil, err
	}
	out := make([]repository.SAPSupplierInvoiceRecord, 0, len(recs))
	for _, m := range recs {
		inv, fy := str(m, "SupplierInvoice"), str(m, "FiscalYear")
		if inv == "" || fy == "" {
			continue
		}
		out = append(out, repository.SAPSupplierInvoiceRecord{
			SupplierInvoice: inv, FiscalYear: fy, CompanyCode: str(m, "CompanyCode"),
			PostingDate: parseODataDate(m["PostingDate"]), Raw: rawRecord(m),
		})
	}
	return out, nil
}

// InvoiceLookups menyediakan data Maximo yang tidak ada di SAP (dari dokumen PO / GRN yang pernah dikirim).
type InvoiceLookups struct {
	POLine func(po string, line int) (*repository.POLineInfo, error)
	GRN    func(matDoc string) (string, error)
}

func maximoSiteID() string   { return envOr("FIN_MAXIMO_SITEID", "BTNG") }
func maximoLineType() string { return envOr("FIN_MAXIMO_LINETYPE", "SERVICE") }

func glDebitAcct(gl, budgetCode string) string {
	gl, budgetCode = strings.TrimSpace(gl), strings.TrimSpace(budgetCode)
	if gl != "" && budgetCode != "" {
		return gl + "-" + budgetCode
	}
	return gl
}

// BuildMxInvoiceFromSupplierInvoice membentuk baris mxinvoice dari satu supplier invoice SAP.
// Satu baris per item (referensi PO lalu item GL), mengikuti sheet Invoice dokumen mapping.
func BuildMxInvoiceFromSupplierInvoice(raw []byte, lk InvoiceLookups) (model.MaximoExportMessage, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return model.MaximoExportMessage{}, fmt.Errorf("raw supplier invoice tidak valid: %w", err)
	}
	inv, fy := str(m, "SupplierInvoice"), str(m, "FiscalYear")
	msg := model.MaximoExportMessage{SourceType: "INVOICE", SourceKey: inv + "/" + fy}

	postDate := toDate(parseODataDate(m["PostingDate"]))
	docDate := toDate(parseODataDate(m["DocumentDate"]))
	dueDate := toDate(parseODataDate(m["DueCalculationBaseDate"]))
	if dueDate == nil {
		dueDate = postDate
	}
	whtCode := ""
	for _, w := range navResults(m, "to_SupplierInvoiceWhldgTax") {
		if c := str(w, "WithholdingTaxCode"); c != "" {
			whtCode = c
			break
		}
	}
	headerText := str(m, "DocumentHeaderText")
	site := maximoSiteID()

	base := model.MxInvoiceRow{
		CurrencyCode: str(m, "DocumentCurrency"), InvoiceType: "INVOICE", DueDate: dueDate, GLPostDate: postDate,
		InvoiceDate: docDate, InvoiceNum: inv, Paid: 0, PaidDate: postDate, SiteID: site, Status: "POSTED",
		Vendor: str(m, "InvoicingParty"), BudgetNum: fy, LineType: maximoLineType(), POSiteID: site,
		TaxCode1: "", TaxCode2: whtCode, TransSeq: 1,
	}

	poItems := navResults(m, "to_SuplrInvcItemPurOrdRef")
	glItems := navResults(m, "to_SupplierInvoiceItemGLAcct")
	sort.SliceStable(poItems, func(i, j int) bool { return itemNo(poItems[i]) < itemNo(poItems[j]) })
	sort.SliceStable(glItems, func(i, j int) bool { return itemNo(glItems[i]) < itemNo(glItems[j]) })

	line := 0
	for _, it := range poItems {
		line++
		row := base
		row.InvoiceLineNum, row.CostLineNum = line, line
		row.LineCost = round2f(parseODataNumber(it["SupplierInvoiceItemAmount"]))
		row.InvoiceQty = parseODataNumber(it["QuantityInPurchaseOrderUnit"])
		if row.InvoiceQty <= 0 {
			row.InvoiceQty = 1
		}
		row.UnitCost = round2f(row.LineCost / row.InvoiceQty)
		row.ItemDesc = str(it, "SupplierInvoiceItemText")
		row.PONum = str(it, "PurchaseOrder")
		if n, err := strconv.Atoi(strings.TrimLeft(str(it, "PurchaseOrderItem"), "0")); err == nil {
			row.POLineNum = &n
		}
		matDoc := str(it, "ReferenceDocument")
		row.BPIGRNNumber = matDoc
		if lk.GRN != nil && matDoc != "" {
			if grn, err := lk.GRN(matDoc); err == nil && grn != "" {
				row.BPIGRNNumber = grn
			}
		}
		if lk.POLine != nil && row.POLineNum != nil {
			info, err := lk.POLine(row.PONum, *row.POLineNum)
			if err != nil {
				return msg, err
			}
			if info != nil {
				row.ItemNum, row.BudgetCode = info.ItemNum, info.BudgetCode
				row.GLDebitAcct = glDebitAcct(info.MainAccount, info.BudgetCode)
			} else {
				msg.Warnings = append(msg.Warnings, fmt.Sprintf("baris %d: PO %s line %d tidak ditemukan di data PO middleware; itemnum/budgetcode kosong", line, row.PONum, *row.POLineNum))
			}
		}
		msg.Rows = append(msg.Rows, row)
	}
	for _, it := range glItems {
		line++
		row := base
		row.InvoiceLineNum, row.CostLineNum = line, line
		amount := round2f(parseODataNumber(it["SupplierInvoiceItemAmount"]))
		if strings.EqualFold(str(it, "DebitCreditCode"), "H") {
			amount = -amount
		}
		row.LineCost, row.InvoiceQty, row.UnitCost = amount, 1, amount
		row.ItemDesc = str(it, "SupplierInvoiceItemText")
		row.BudgetCode = str(it, "CostCenter") // di SAP, budget code Maximo disimpan sebagai cost center
		row.GLDebitAcct = glDebitAcct(str(it, "GLAccount"), row.BudgetCode)
		row.BPIGRNNumber = str(it, "AssignmentReference")
		msg.Rows = append(msg.Rows, row)
	}
	if len(msg.Rows) == 0 {
		return msg, fmt.Errorf("supplier invoice %s tidak memiliki item", msg.SourceKey)
	}
	desc := headerText
	if desc == "" {
		desc = msg.Rows[0].ItemDesc
	}
	for i := range msg.Rows {
		msg.Rows[i].InvoiceDesc = desc
	}
	return msg, nil
}

// BuildMxInvoiceFromJournal membentuk SATU baris mxinvoice dari satu jurnal (sheet GJ):
// LINECOST = total debit, qty 1, GL & item text diambil dari baris debit pertama.
func BuildMxInvoiceFromJournal(items []model.GLLineItem) (model.MaximoExportMessage, error) {
	if len(items) == 0 {
		return model.MaximoExportMessage{}, fmt.Errorf("jurnal tanpa line item")
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].LedgerGLLineItem < items[j].LedgerGLLineItem })
	first := items[0]
	msg := model.MaximoExportMessage{SourceType: "JOURNAL",
		SourceKey: first.CompanyCode + "/" + first.FiscalYear + "/" + first.AccountingDocument}

	var total float64
	var pick *model.GLLineItem
	vendor := ""
	for i := range items {
		it := &items[i]
		if strings.EqualFold(it.DebitCreditCode, "S") {
			total += math.Abs(it.AmountInTransactionCurrency)
			if pick == nil {
				pick = it
			}
		}
		if vendor == "" && it.Supplier != "" {
			vendor = it.Supplier
		}
	}
	if pick == nil {
		pick = &items[0]
		for _, it := range items {
			total += math.Abs(it.AmountInTransactionCurrency)
		}
		total /= 2
		msg.Warnings = append(msg.Warnings, "jurnal tanpa baris debit (S); total dihitung dari setengah jumlah absolut")
	}
	total = round2f(total)
	postDate := toDate(first.PostingDate)
	docDate := toDate(first.DocumentDate)
	if docDate == nil {
		docDate = postDate
	}
	site := maximoSiteID()
	msg.Rows = []model.MxInvoiceRow{{
		CurrencyCode: pick.TransactionCurrency, InvoiceDesc: pick.DocumentItemText, InvoiceType: "JOURNAL",
		DueDate: postDate, GLPostDate: postDate, InvoiceDate: docDate, InvoiceNum: first.AccountingDocument,
		Paid: 0, PaidDate: postDate, SiteID: site, Status: "POSTED", Vendor: vendor, BudgetCode: pick.CostCenter,
		BudgetNum: first.FiscalYear, ItemDesc: pick.DocumentItemText, InvoiceLineNum: 1, InvoiceQty: 1,
		LineCost: total, LineType: maximoLineType(), POSiteID: site, UnitCost: total, CostLineNum: 1,
		GLDebitAcct: glDebitAcct(pick.GLAccount, pick.CostCenter), TransSeq: 1,
	}}
	return msg, nil
}

// InvoicePullOptions adalah parameter tarik supplier invoice dari SAP.
type InvoicePullOptions struct {
	CompanyCode string
	FromDate    string
	ToDate      string
}

// BuildSupplierInvoiceFilter menyusun $filter tarikan A_SupplierInvoice.
func BuildSupplierInvoiceFilter(o InvoicePullOptions, status string) string {
	parts := []string{
		fmt.Sprintf("CompanyCode eq '%s'", odataLiteral(o.CompanyCode)),
		fmt.Sprintf("PostingDate ge datetime'%sT00:00:00'", o.FromDate),
		fmt.Sprintf("PostingDate le datetime'%sT23:59:59'", o.ToDate),
	}
	if status != "" {
		parts = append(parts, fmt.Sprintf("SupplierInvoiceStatus eq '%s'", odataLiteral(status)))
	}
	return strings.Join(parts, " and ")
}

// SyncSupplierInvoices menarik supplier invoice SAP (beserta item) untuk ditulis ke Maximo.
func (f *FinanceService) SyncSupplierInvoices(ctx context.Context, o InvoicePullOptions) model.SyncResult {
	if o.CompanyCode == "" {
		o.CompanyCode = DefaultCompanyCode()
	}
	if o.ToDate == "" {
		o.ToDate = time.Now().Format("2006-01-02")
	}
	if o.FromDate == "" {
		o.FromDate = f.PGRepo.SyncWindowStart(ctx, syncIDSupplierInvoice, 24*time.Hour, 30*24*time.Hour).Format("2006-01-02")
	}
	filter := BuildSupplierInvoiceFilter(o, envOr("FIN_INVOICE_PULL_STATUS", ""))
	res := f.pullPagesQ(ctx, model.DocTypeSupplierInvoice, filter, "&$expand="+odataQueryEscape(supplierInvoiceExpand), func(body []byte) (int, int, error) {
		items, err := ParseSAPSupplierInvoices(body)
		if err != nil {
			return 0, 0, err
		}
		n, err := f.PGRepo.UpsertSAPSupplierInvoices(ctx, items)
		return len(items), n, err
	})
	res.Target = "invoice"
	if res.Error == "" {
		_ = f.PGRepo.UpdateLastSync(ctx, syncIDSupplierInvoice, time.Now())
	}
	return res
}

// MaximoExportOptions mengatur proses tulis ke Maximo.
type MaximoExportOptions struct {
	DryRun bool     // true = hanya bentuk baris, tidak menulis ke SQL Server
	Limit  int      // maksimal dokumen per jenis
	Types  []string // INVOICE, JOURNAL (kosong = keduanya)
}

// MaximoExportResult meringkas satu proses tulis ke Maximo.
type MaximoExportResult struct {
	DryRun   bool                        `json:"dry_run"`
	Exported int                         `json:"exported"`
	Failed   int                         `json:"failed"`
	Messages []model.MaximoExportMessage `json:"messages"`
	Errors   []string                    `json:"errors,omitempty"`
}

func wantType(types []string, t string) bool {
	if len(types) == 0 {
		return true
	}
	for _, x := range types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

// ExportToMaximo membentuk baris mxinvoice dari invoice & jurnal SAP yang belum ditulis, lalu (kecuali
// DryRun) menulisnya ke Maximo dan mencatat hasilnya. Dokumen yang gagal dicatat FAILED dan dicoba lagi
// pada proses berikutnya.
func (f *FinanceService) ExportToMaximo(ctx context.Context, o MaximoExportOptions) (*MaximoExportResult, error) {
	if !o.DryRun && f.MaximoWriter == nil {
		return nil, newValidation("SQL Server Maximo tidak dikonfigurasi; jalankan dengan dry_run untuk pratinjau")
	}
	res := &MaximoExportResult{DryRun: o.DryRun}
	var msgs []model.MaximoExportMessage

	if wantType(o.Types, "INVOICE") {
		invs, err := f.PGRepo.ListSupplierInvoicesForExport(ctx, o.Limit)
		if err != nil {
			return nil, err
		}
		lk := InvoiceLookups{
			POLine: func(po string, line int) (*repository.POLineInfo, error) { return f.PGRepo.LookupPOLine(ctx, po, line) },
			GRN:    func(md string) (string, error) { return f.PGRepo.LookupGRNByMaterialDocument(ctx, md) },
		}
		for _, inv := range invs {
			msg, err := BuildMxInvoiceFromSupplierInvoice(inv.Raw, lk)
			if err != nil {
				res.Errors = append(res.Errors, err.Error())
				continue
			}
			msgs = append(msgs, msg)
		}
	}
	if wantType(o.Types, "JOURNAL") {
		order, journals, err := f.PGRepo.ListJournalsForExport(ctx, DocTypesFor("journal"), o.Limit)
		if err != nil {
			return nil, err
		}
		for _, k := range order {
			msg, err := BuildMxInvoiceFromJournal(journals[k])
			if err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", k, err))
				continue
			}
			msgs = append(msgs, msg)
		}
	}

	for _, msg := range msgs {
		if o.DryRun {
			res.Messages = append(res.Messages, msg)
			continue
		}
		ids, err := f.MaximoWriter.WriteMxInvoiceMessage(ctx, msg.Rows)
		rec := model.MaximoExport{SourceType: msg.SourceType, SourceKey: msg.SourceKey, LineCount: len(msg.Rows)}
		if err != nil {
			rec.Status, rec.Error = "FAILED", err.Error()
			res.Failed++
			res.Errors = append(res.Errors, fmt.Sprintf("%s %s: %v", msg.SourceType, msg.SourceKey, err))
		} else {
			strIDs := make([]string, len(ids))
			for i, id := range ids {
				strIDs[i] = strconv.FormatInt(id, 10)
				msg.Rows[i].TransID = id
			}
			rec.Status, rec.TransIDs = "EXPORTED", strings.Join(strIDs, ",")
			res.Exported++
			_ = f.PGRepo.InsertIntegrationLog(ctx, "MXINV-"+msg.SourceKey, "SAP GROW", "MAXIMO", "MX_"+msg.SourceType, msg.SourceKey, "SUCCESS", "",
				fmt.Sprintf("%d baris ditulis ke dbo.mxinvoice (transid %s)", len(ids), rec.TransIDs))
		}
		if err := f.PGRepo.RecordMaximoExport(ctx, rec); err != nil {
			return nil, err
		}
		res.Messages = append(res.Messages, msg)
	}
	return res, nil
}
