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

// Batas panjang kolom INVOICENUM di dokumen mapping (UPPER 8).
const mxInvoiceNumMaxLen = 8

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

func round2f(v float64) float64 { return math.Round(v*100) / 100 }

func maximoSiteID() string   { return envOr("FIN_MAXIMO_SITEID", "BTNG") }
func maximoLineType() string { return envOr("FIN_MAXIMO_LINETYPE", "SERVICE") }

// glExtra adalah field GLAccountLineItem yang tidak punya kolom sendiri di sap_gl_line_items dan dibaca dari raw.
type glExtra struct {
	LineNo         string
	AmountGlobal   float64
	GlobalCurrency string
	Quantity       float64
	PurchasingDoc  string
	PurchasingItem string
	Assignment     string
	WHTCode        string
}

func glExtras(it model.GLLineItem) glExtra {
	var m map[string]interface{}
	_ = json.Unmarshal(it.Raw, &m)
	line := str(m, "AccountingDocumentItem")
	if line == "" {
		line = it.LedgerGLLineItem
	}
	if t := strings.TrimLeft(line, "0"); t != "" {
		line = t
	}
	amount := parseODataNumber(m["AmountInGlobalCurrency"])
	if _, ok := m["AmountInGlobalCurrency"]; !ok {
		amount = it.AmountInCompanyCodeCurrency
	}
	return glExtra{
		LineNo:         line,
		AmountGlobal:   amount,
		GlobalCurrency: str(m, "GlobalCurrency"),
		Quantity:       math.Abs(parseODataNumber(m["Quantity"])),
		PurchasingDoc:  str(m, "PurchasingDocument"),
		PurchasingItem: strings.TrimLeft(str(m, "PurchasingDocumentItem"), "0"),
		Assignment:     str(m, "AssignmentReference"),
		WHTCode:        str(m, "WithholdingTaxCode", "WithholdingTaxType"),
	}
}

func glHasGlobalAmount(it model.GLLineItem) bool {
	var m map[string]interface{}
	_ = json.Unmarshal(it.Raw, &m)
	_, ok := m["AmountInGlobalCurrency"]
	return ok
}

// BuildMxInvoiceFromGL membentuk SATU baris mxinvoice dari satu dokumen akuntansi SAP (GLAccountLineItem),
// mengikuti dokumen BPI_MAPPING_API_MAXIMO_to_SAP (FinanceModule):
//   - INVOICE (sheet Invoice, RE/KR): vendor, nilai, teks, assignment & GL diambil dari baris supplier (AP);
//     ITEMNUM, quantity & PO dari baris tanpa supplier.
//   - JOURNAL (sheet GJ, SA): LINECOST = jumlah nilai baris yang punya cost center, quantity 1.
//
// Nilai memakai Amount in Global Currency; SITEID = Profit Center; BUDGETNUM = Cost Center + tahun posting;
// COSTLINENUM = nomor dokumen SAP; TRANSSEQ = 0.
func BuildMxInvoiceFromGL(sourceType string, items []model.GLLineItem) (model.MaximoExportMessage, error) {
	if len(items) == 0 {
		return model.MaximoExportMessage{}, fmt.Errorf("dokumen tanpa line item")
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].LedgerGLLineItem < items[j].LedgerGLLineItem })
	first := items[0]
	msg := model.MaximoExportMessage{SourceType: sourceType,
		SourceKey: first.CompanyCode + "/" + first.FiscalYear + "/" + first.AccountingDocument}
	extras := make([]glExtra, len(items))
	for i := range items {
		extras[i] = glExtras(items[i])
		if !glHasGlobalAmount(items[i]) {
			msg.Warnings = append(msg.Warnings, fmt.Sprintf("baris %s tanpa AmountInGlobalCurrency; memakai nilai mata uang company code", extras[i].LineNo))
		}
	}

	postDate := toDate(first.PostingDate)
	docDate := toDate(first.DocumentDate)
	if docDate == nil {
		docDate = postDate
	}
	year := first.FiscalYear
	if first.PostingDate != nil && len(*first.PostingDate) >= 4 {
		year = (*first.PostingDate)[:4]
	}
	site, costCenter, currency := "", "", ""
	for i, it := range items {
		if site == "" {
			site = it.ProfitCenter
		}
		if costCenter == "" {
			costCenter = it.CostCenter
		}
		if currency == "" {
			currency = extras[i].GlobalCurrency
		}
	}
	if site == "" {
		site = maximoSiteID()
		msg.Warnings = append(msg.Warnings, "dokumen tanpa Profit Center; SITEID memakai FIN_MAXIMO_SITEID")
	}
	if currency == "" {
		currency = envOr("FIN_MAXIMO_GLOBAL_CURRENCY", "USD")
	}
	budgetNum := ""
	if costCenter != "" {
		budgetNum = costCenter + year
	}

	row := model.MxInvoiceRow{
		CurrencyCode: currency, GLPostDate: postDate, InvoiceDate: docDate, InvoiceNum: first.AccountingDocument,
		Paid: 0, PaidDate: postDate, SiteID: site, Status: "POSTED", BudgetCode: costCenter, BudgetNum: budgetNum,
		LineType: maximoLineType(), POSiteID: site, CostLineNum: first.AccountingDocument, TransSeq: 0,
	}

	switch sourceType {
	case repository.MaximoSourceInvoice:
		ap := -1
		for i, it := range items {
			if it.Supplier != "" {
				ap = i
				break
			}
		}
		if ap < 0 {
			return msg, fmt.Errorf("invoice %s tanpa baris supplier", msg.SourceKey)
		}
		apItem, apX := items[ap], extras[ap]
		row.InvoiceType = "INVOICE"
		row.Vendor = apItem.Supplier
		row.InvoiceLineNum, _ = strconv.Atoi(apX.LineNo)
		row.LineCost = round2f(math.Abs(apX.AmountGlobal))
		row.InvoiceDesc, row.ItemDesc = apItem.DocumentItemText, apItem.DocumentItemText
		row.BPIGRNNumber = apX.Assignment
		row.GLDebitAcct = apItem.GLAccount
		row.InvoiceQty = 0
		for i, it := range items {
			x := extras[i]
			if row.TaxCode2 == "" {
				row.TaxCode2 = x.WHTCode
			}
			if it.Supplier != "" {
				continue
			}
			if row.ItemNum == "" {
				row.ItemNum = x.LineNo
			}
			if row.PONum == "" && x.PurchasingDoc != "" {
				row.PONum = x.PurchasingDoc
				if n, err := strconv.Atoi(x.PurchasingItem); err == nil {
					row.POLineNum = &n
				}
			}
			if row.InvoiceQty == 0 && x.Quantity > 0 {
				row.InvoiceQty = x.Quantity
			}
		}
		if row.InvoiceQty <= 0 {
			row.InvoiceQty = 1
		}
	case repository.MaximoSourceJournal:
		row.InvoiceType = "JOURNAL"
		row.InvoiceLineNum, row.InvoiceQty = 1, 1
		var total float64
		found := false
		for i, it := range items {
			if it.CostCenter == "" {
				continue
			}
			total += extras[i].AmountGlobal
			if !found {
				found = true
				row.ItemNum = extras[i].LineNo
				row.GLDebitAcct = it.GLAccount
				row.BudgetCode = it.CostCenter
				row.BudgetNum = it.CostCenter + year
			}
			if row.InvoiceDesc == "" && it.DocumentItemText != "" {
				row.InvoiceDesc, row.ItemDesc = it.DocumentItemText, it.DocumentItemText
			}
		}
		if !found {
			return msg, fmt.Errorf("jurnal %s tanpa baris cost center", msg.SourceKey)
		}
		row.LineCost = round2f(total)
	default:
		return msg, fmt.Errorf("source type %q tidak dikenal", sourceType)
	}
	row.UnitCost = round2f(row.LineCost / row.InvoiceQty)
	if len(row.InvoiceNum) > mxInvoiceNumMaxLen {
		msg.Warnings = append(msg.Warnings, fmt.Sprintf("INVOICENUM %s lebih dari %d karakter (batas di dokumen mapping)", row.InvoiceNum, mxInvoiceNumMaxLen))
	}
	msg.Rows = []model.MxInvoiceRow{row}
	return msg, nil
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

// ExportToMaximo membentuk baris mxinvoice dari dokumen GL SAP (invoice RE/KR, termasuk reimbursement
// RICEF 7 yang sudah terposting, dan jurnal SA) yang belum ditulis, lalu (kecuali DryRun) menulisnya ke
// Maximo dan mencatat hasilnya. Dokumen yang gagal dicatat FAILED dan dicoba lagi pada proses berikutnya.
func (f *FinanceService) ExportToMaximo(ctx context.Context, o MaximoExportOptions) (*MaximoExportResult, error) {
	if !o.DryRun && f.MaximoWriter == nil {
		return nil, newValidation("SQL Server Maximo tidak dikonfigurasi; jalankan dengan dry_run untuk pratinjau")
	}
	res := &MaximoExportResult{DryRun: o.DryRun}
	var msgs []model.MaximoExportMessage

	for _, src := range []struct{ sourceType, category string }{
		{repository.MaximoSourceInvoice, "invoice"},
		{repository.MaximoSourceJournal, "journal"},
	} {
		if !wantType(o.Types, src.sourceType) {
			continue
		}
		order, docs, err := f.PGRepo.ListGLDocumentsForExport(ctx, src.sourceType, DocTypesFor(src.category), o.Limit)
		if err != nil {
			return nil, err
		}
		for _, k := range order {
			msg, err := BuildMxInvoiceFromGL(src.sourceType, docs[k])
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
