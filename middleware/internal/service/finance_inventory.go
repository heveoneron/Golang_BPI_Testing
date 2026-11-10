package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
)

// InventoryStagingReader membaca staging Maximo [axinvissue] & [axinvtrans].
type InventoryStagingReader interface {
	ExtractInventoryStaging(ctx context.Context, since time.Time, limit int) ([]repository.InventoryStagingRow, error)
}

type InventorySyncResult struct {
	Fetched int      `json:"fetched"`
	Queued  int      `json:"queued"`
	Skipped int      `json:"skipped"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors,omitempty"`
}

func envSet(key, def string) map[string]bool {
	set := map[string]bool{}
	for _, v := range strings.Split(envOr(key, def), ",") {
		if v = strings.ToUpper(strings.TrimSpace(v)); v != "" {
			set[v] = true
		}
	}
	return set
}

// GIInputFromStaging memetakan baris staging ke input determinasi GL (FSD DJ-MM-ENH-ZME003).
// ok=false bila baris tidak diproses (issuetype/transtype di luar filter atau quantity nol).
func GIInputFromStaging(row repository.InventoryStagingRow) (model.GIOrAdjustmentInput, bool) {
	if row.Quantity == 0 || row.ItemNum == "" {
		return model.GIOrAdjustmentInput{}, false
	}
	in := model.GIOrAdjustmentInput{
		ItemNum:    row.ItemNum,
		Quantity:   row.Quantity,
		TransDate:  row.TransDate.Format("2006-01-02"),
		Reference2: truncate(row.SiteID, 10),
	}
	id := strconv.FormatInt(row.SourceID, 10)
	switch row.Source {
	case "axinvissue":
		if !envSet("FIN_GI_ISSUETYPES", "ISSUE")[row.TransType] {
			return in, false
		}
		in.TransactionType = "GOODS_ISSUE"
		in.Reference = "GI-" + id
		in.Reference1 = truncate(row.InvUseNum, 20)
		text := row.Description
		if row.RefWO != "" {
			text = "WO " + row.RefWO + " " + text
		}
		in.ItemText = truncate(strings.TrimSpace(text), 50)
	case "axinvtrans":
		if allowed := envSet("FIN_ADJ_TRANSTYPES", ""); len(allowed) > 0 && !allowed[row.TransType] {
			return in, false
		}
		in.TransactionType = "STOCK_ADJUSTMENT"
		in.Reference = "ADJ-" + id
		in.Reference1 = truncate(row.StoreLoc, 20)
		in.ItemText = truncate("Stock Adj "+row.ItemNum+" "+row.StoreLoc, 50)
	default:
		return in, false
	}
	return in, true
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// SyncInventoryJournals menarik GI & Stock Adj dari Maximo lalu mengantrekan Journal Entry SOAP ke SAP.
func (f *FinanceService) SyncInventoryJournals(ctx context.Context, reader InventoryStagingReader, since time.Time, limit int) InventorySyncResult {
	var res InventorySyncResult
	if reader == nil {
		res.Errors = append(res.Errors, "koneksi SQL Server Maximo tidak tersedia")
		return res
	}
	rows, err := reader.ExtractInventoryStaging(ctx, since, limit)
	res.Fetched = len(rows)
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
	}
	inputs := make([]model.GIOrAdjustmentInput, 0, len(rows))
	refs := make([]string, 0, len(rows))
	for _, row := range rows {
		in, ok := GIInputFromStaging(row)
		if !ok {
			res.Skipped++
			continue
		}
		inputs = append(inputs, in)
		refs = append(refs, in.Reference)
	}
	existing, err := f.PGRepo.ExistingDocumentNos(ctx, model.DocTypeJournalGI, refs)
	if err != nil {
		res.Errors = append(res.Errors, "cek dokumen existing: "+err.Error())
		return res
	}
	for _, in := range inputs {
		if existing[in.Reference] {
			res.Skipped++
			continue
		}
		if _, err := f.ProcessGoodsIssueOrAdjustment(ctx, in); err != nil {
			res.Failed++
			if len(res.Errors) < 20 {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", in.Reference, err))
			}
			continue
		}
		res.Queued++
	}
	return res
}
