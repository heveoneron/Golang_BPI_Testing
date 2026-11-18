package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
)

// Migrasi PO Outstanding < 2025 (PRD Fitur 3): template "Template PO Outstanding and AX.xlsx", satu baris per
// line item, dikirim ke SAP sebagai PO (API_PURCHASEORDER_2) lewat mapping engine PO yang sama tanpa lock pagu.

// POOutstandingCutoff: hanya PO dengan orderdate sebelum tanggal ini yang boleh dimigrasikan.
var POOutstandingCutoff = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// POOutstandingTemplateHeaders = urutan kolom template (header boleh berisi catatan dalam kurung).
var POOutstandingTemplateHeaders = []string{"currencycode", "orderdate", "DP Percentage", "ponum", "siteid", "vendor",
	"bpibudgetcode", "pol_desc_ld", "itemnum", "orderqty", "orderunit", "polinenum", "taxcode1", "unitcost"}

// poHeaderKey: "pol_desc_ld (ditaruh di short text ...)" -> "poldescld", "unitcost (harga satuan)" -> "unitcost".
func poHeaderKey(h string) string {
	if i := strings.IndexAny(h, "(\n"); i >= 0 {
		h = h[:i]
	}
	return normalizeHeader(h)
}

var poRequiredColumns = []string{"ponum", "orderdate", "vendor", "polinenum", "orderqty", "unitcost"}

// POOutstandingUploadResult adalah hasil validasi + simpan satu file upload.
type POOutstandingUploadResult struct {
	BatchID   string                 `json:"batch_id"`
	TotalRows int                    `json:"total_rows"`
	Saved     int                    `json:"saved"`
	POCount   int                    `json:"po_count"`
	Errors    []model.UploadRowError `json:"errors,omitempty"`
}

// ParsePOOutstandingFile membaca & memvalidasi file template. Tidak ada baris yang disimpan bila ada error.
func ParsePOOutstandingFile(fileName string, data []byte, sourceSystem string) ([]repository.POOutstandingLine, []model.UploadRowError, error) {
	sourceSystem = strings.ToUpper(strings.TrimSpace(sourceSystem))
	if sourceSystem != "MAXIMO" && sourceSystem != "AX" {
		return nil, nil, newValidation("source_system harus MAXIMO atau AX")
	}
	table, err := readTable(fileName, data)
	if err != nil {
		return nil, nil, newValidation(err.Error())
	}
	hdrRow := -1
	col := map[string]int{}
	for i, r := range table {
		if strings.TrimSpace(strings.Join(r, "")) == "" {
			continue
		}
		hdrRow = i
		for c, h := range r {
			col[poHeaderKey(h)] = c
		}
		break
	}
	if hdrRow < 0 {
		return nil, nil, newValidation("file kosong")
	}
	var missing []string
	for _, k := range poRequiredColumns {
		if _, ok := col[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, nil, newValidation("kolom wajib tidak ada: " + strings.Join(missing, ", ") + " (gunakan Download Template)")
	}
	get := func(r []string, key string) string {
		if i, ok := col[key]; ok && i < len(r) {
			return strings.TrimSpace(r[i])
		}
		return ""
	}

	var lines []repository.POOutstandingLine
	var errs []model.UploadRowError
	seen := map[string]int{}
	headerOf := map[string]repository.POOutstandingLine{}
	for i := hdrRow + 1; i < len(table); i++ {
		r := table[i]
		if strings.TrimSpace(strings.Join(r, "")) == "" {
			continue
		}
		rowNo := i + 1
		addErr := func(field, msg string) {
			errs = append(errs, model.UploadRowError{Row: rowNo, Field: field, Message: msg})
		}
		l := repository.POOutstandingLine{SourceSystem: sourceSystem, RowNumber: rowNo,
			CurrencyCode: strings.ToUpper(get(r, "currencycode")), PONum: get(r, "ponum"), SiteID: get(r, "siteid"),
			Vendor: get(r, "vendor"), BPIBudgetCode: get(r, "bpibudgetcode"), POLDescLD: get(r, "poldescld"),
			ItemNum: get(r, "itemnum"), OrderUnit: get(r, "orderunit"), TaxCode1: get(r, "taxcode1")}
		if l.PONum == "" {
			addErr("ponum", "wajib diisi")
		}
		if l.Vendor == "" {
			addErr("vendor", "wajib diisi")
		}
		if d, ok := parseFlexibleDate(get(r, "orderdate")); !ok {
			addErr("orderdate", "tanggal tidak valid")
		} else if t, _ := time.Parse("2006-01-02", d); !t.Before(POOutstandingCutoff) {
			addErr("orderdate", fmt.Sprintf("%s bukan PO < 2025; PO tahun 2025 ke atas lewat alur Maximo/AX biasa", d))
		} else {
			l.OrderDate = d
		}
		if n, ok := parseAmount(get(r, "polinenum")); !ok || n <= 0 || n != float64(int(n)) {
			addErr("polinenum", "harus bilangan bulat > 0")
		} else {
			l.POLineNum = int(n)
		}
		if q, ok := parseAmount(get(r, "orderqty")); !ok || q <= 0 {
			addErr("orderqty", "harus angka > 0")
		} else {
			l.OrderQty = q
		}
		if u, ok := parseAmount(get(r, "unitcost")); !ok || u < 0 {
			addErr("unitcost", "harus angka >= 0")
		} else {
			l.UnitCost = u
		}
		if s := get(r, "dppercentage"); s != "" {
			if p, ok := parseAmount(strings.TrimSuffix(s, "%")); !ok || p < 0 || p > 100 {
				addErr("DP Percentage", "harus 0-100")
			} else {
				l.DPPercentage = p
			}
		}
		if l.PONum != "" && l.POLineNum > 0 {
			key := fmt.Sprintf("%s#%d", l.PONum, l.POLineNum)
			if prev, dup := seen[key]; dup {
				addErr("polinenum", fmt.Sprintf("duplikat PO %s line %d (baris %d)", l.PONum, l.POLineNum, prev))
			}
			seen[key] = rowNo
		}
		// Field header harus sama untuk semua baris satu PO.
		if h, ok := headerOf[l.PONum]; ok && l.PONum != "" {
			for _, c := range []struct{ name, a, b string }{
				{"vendor", h.Vendor, l.Vendor}, {"currencycode", h.CurrencyCode, l.CurrencyCode},
				{"orderdate", h.OrderDate, l.OrderDate}, {"siteid", h.SiteID, l.SiteID},
			} {
				if c.a != c.b && c.b != "" {
					addErr(c.name, fmt.Sprintf("berbeda dengan baris lain PO %s (%q vs %q)", l.PONum, c.a, c.b))
				}
			}
			if h.DPPercentage != l.DPPercentage {
				addErr("DP Percentage", fmt.Sprintf("berbeda dengan baris lain PO %s", l.PONum))
			}
		} else if l.PONum != "" {
			headerOf[l.PONum] = l
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 && len(errs) == 0 {
		return nil, nil, newValidation("tidak ada baris data")
	}
	return lines, errs, nil
}

// UploadPOOutstanding memvalidasi file lalu menyimpannya ke staging (semua-atau-tidak sama sekali).
func (s *SyncService) UploadPOOutstanding(ctx context.Context, fileName string, data []byte, sourceSystem, uploadedBy string) (POOutstandingUploadResult, error) {
	res := POOutstandingUploadResult{BatchID: fmt.Sprintf("POL-%s", time.Now().Format("20060102150405.000"))}
	res.BatchID = strings.ReplaceAll(res.BatchID, ".", "")
	lines, errs, err := ParsePOOutstandingFile(fileName, data, sourceSystem)
	if err != nil {
		return res, err
	}
	res.TotalRows = len(lines) + countErrorOnlyRows(lines, errs)
	pos := map[repository.POOutstandingKey]bool{}
	for _, l := range lines {
		pos[repository.POOutstandingKey{SourceSystem: l.SourceSystem, PONum: l.PONum}] = true
	}
	keys := make([]repository.POOutstandingKey, 0, len(pos))
	for k := range pos {
		keys = append(keys, k)
	}
	synced, err := s.PGRepo.SyncedPONumbers(ctx, keys)
	if err != nil {
		return res, err
	}
	for _, l := range lines {
		if sapPO, ok := synced[repository.POOutstandingKey{SourceSystem: l.SourceSystem, PONum: l.PONum}]; ok {
			errs = append(errs, model.UploadRowError{Row: l.RowNumber, Field: "ponum",
				Message: fmt.Sprintf("PO %s sudah terkirim/sedang dikirim ke SAP (PO SAP %s); baris tidak bisa diubah/ditambah", l.PONum, sapPO)})
		}
	}
	res.POCount = len(pos)
	if len(errs) > 0 {
		res.Errors = errs
		return res, nil
	}
	saved, err := s.PGRepo.UpsertPOOutstanding(ctx, res.BatchID, uploadedBy, lines)
	res.Saved = saved
	return res, err
}

func countErrorOnlyRows(lines []repository.POOutstandingLine, errs []model.UploadRowError) int {
	inLines := map[int]bool{}
	for _, l := range lines {
		inLines[l.RowNumber] = true
	}
	extra := map[int]bool{}
	for _, e := range errs {
		if !inLines[e.Row] {
			extra[e.Row] = true
		}
	}
	return len(extra)
}

// poOutstandingRaw menyusun payload gaya Maximo (MaximoPOHeader) dari baris staging satu PO.
func poOutstandingRaw(lines []repository.POOutstandingLine) (json.RawMessage, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("PO tanpa baris")
	}
	h := lines[0]
	po := model.MaximoPOHeader{PONum: h.PONum, Vendor: h.Vendor, OrderDate: h.OrderDate, CurrencyCode: h.CurrencyCode,
		SiteID: h.SiteID, Status: "APPR", DownPaymentPercentageOfTotAmt: h.DPPercentage}
	if h.DPPercentage > 0 {
		po.DownPaymentType = "V"
	}
	for _, l := range lines {
		po.Lines = append(po.Lines, model.MaximoPOLine{POLineNum: l.POLineNum, ItemNum: l.ItemNum, POLDescLD: l.POLDescLD,
			OrderUnit: l.OrderUnit, OrderQty: l.OrderQty, UnitCost: l.UnitCost, BPIBudgetCode: l.BPIBudgetCode,
			TaxCodePPN: l.TaxCode1, SiteID: l.SiteID})
	}
	return json.Marshal(po)
}

// POOutstandingSyncResult merangkum satu kali Sync to SAP.
type POOutstandingSyncResult struct {
	Requested int                     `json:"requested"`
	Synced    int                     `json:"synced"`
	Failed    int                     `json:"failed"`
	Skipped   int                     `json:"skipped"`
	Details   []POOutstandingSyncItem `json:"details"`
}

type POOutstandingSyncItem struct {
	PONum       string `json:"ponum"`
	Source      string `json:"source_system"`
	Status      string `json:"status"` // SYNCED / FAILED / SKIPPED
	SAPPONumber string `json:"sap_po_number,omitempty"`
	Message     string `json:"message,omitempty"`
}

// maxPOOutstandingPerSync membatasi satu request agar tidak melewati timeout HTTP.
const maxPOOutstandingPerSync = 200

// SyncPOOutstanding mengirim PO terpilih (ids) atau semua PENDING ke SAP. Setiap PO dikunci atomik dulu
// (is_processing); PO yang sudah terkirim / sedang diproses dilewati.
func (s *SyncService) SyncPOOutstanding(ctx context.Context, ids []int64, allPending bool) (POOutstandingSyncResult, error) {
	var res POOutstandingSyncResult
	if !allPending && len(ids) == 0 {
		return res, newValidation("pilih baris yang akan dikirim atau gunakan Sync All Pending")
	}
	keys, err := s.PGRepo.POOutstandingKeys(ctx, ids, allPending)
	if err != nil {
		return res, err
	}
	if len(keys) > maxPOOutstandingPerSync {
		keys = keys[:maxPOOutstandingPerSync]
	}
	res.Requested = len(keys)
	ep, epErr := s.PGRepo.GetSAPEndpointByCode(ctx, envOr("PO_OUTSTANDING_ENDPOINT_CODE", "PO"))
	for _, key := range keys {
		item := POOutstandingSyncItem{PONum: key.PONum, Source: key.SourceSystem}
		lines, err := s.PGRepo.ClaimPOOutstanding(ctx, key)
		if err != nil {
			return res, err
		}
		if len(lines) == 0 {
			item.Status, item.Message = "SKIPPED", "sudah terkirim atau sedang diproses"
			res.Skipped++
			res.Details = append(res.Details, item)
			continue
		}
		lineIDs := make([]int64, len(lines))
		for i, l := range lines {
			lineIDs[i] = l.ID
		}
		sapPO, sendErr := s.sendPOOutstanding(ctx, ep, epErr, lines)
		groupID := "GRP-PO_LEGACY-" + key.PONum
		if sendErr != nil {
			item.Status, item.Message = "FAILED", sendErr.Error()
			res.Failed++
			_ = s.PGRepo.FinishPOOutstanding(ctx, lineIDs, "", sendErr.Error())
			_ = s.PGRepo.InsertIntegrationLog(ctx, groupID, "MIDDLEWARE", "SAP GROW", "PO_LEGACY", key.PONum, "FAILED", sendErr.Error(), "PO Outstanding < 2025 gagal dikirim: "+sendErr.Error())
		} else {
			item.Status, item.SAPPONumber = "SYNCED", sapPO
			res.Synced++
			_ = s.PGRepo.FinishPOOutstanding(ctx, lineIDs, sapPO, "")
			_ = s.PGRepo.InsertIntegrationLog(ctx, groupID, "MIDDLEWARE", "SAP GROW", "PO_LEGACY", key.PONum, "SUCCESS", "", "PO Outstanding < 2025 terkirim (PO SAP "+sapPO+")")
		}
		res.Details = append(res.Details, item)
	}
	return res, nil
}

func (s *SyncService) sendPOOutstanding(ctx context.Context, ep *model.SAPEndpoint, epErr error, lines []repository.POOutstandingLine) (string, error) {
	if epErr != nil || ep == nil {
		return "", fmt.Errorf("endpoint SAP PO tidak ditemukan di SAP Integration: %v", epErr)
	}
	if !ep.IsActive {
		return "", fmt.Errorf("endpoint SAP %s nonaktif", ep.ServiceCode)
	}
	raw, err := poOutstandingRaw(lines)
	if err != nil {
		return "", err
	}
	payload, err := s.transformPO(withoutBudgetLock(ctx), raw)
	if err != nil {
		return "", fmt.Errorf("mapping PO gagal: %w", err)
	}
	_, sapDocNo, _, err := s.SAPClient.ExecuteOData(ep, "POST", payload)
	if err != nil {
		return "", err
	}
	if sapDocNo == "" {
		sapDocNo = ConvertMaximoPOToSAP(lines[0].PONum)
	}
	return sapDocNo, nil
}
