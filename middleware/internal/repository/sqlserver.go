package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

type SQLServerRepository struct {
	DB *sql.DB
}

type ExtractedRecord struct {
	TransID       string
	IfaceTbName   string
	DocType       string
	DocumentNo    string
	PayloadJSON   json.RawMessage
	LastUpdatedAt time.Time
}

// InboundControlRecord merepresentasikan data kontrol integrasi masuk ke Maximo (mxin_inter_trans)
// sebagaimana didefinisikan pada makro STG_DATA_INSERT_MXIN di dokumen Doc Staging.
type InboundControlRecord struct {
	IfaceName     string
	TransID       int64
	Action        string
	ExtSysName    string
	TransLanguage string
}

func NewSQLServerRepository(db *sql.DB) *SQLServerRepository {
	return &SQLServerRepository{DB: db}
}

// ExtractData menarik data antrean dari mxout_inter_trans dengan batas limit data valid.
// Hanya data yang terbukti memiliki baris detail di tabel ifacetbname yang akan ditarik.
func (r *SQLServerRepository) ExtractData(ctx context.Context, limit int) ([]ExtractedRecord, error) {
	if r.DB == nil {
		return nil, nil
	}

	if limit <= 0 {
		limit = 1
	}

	var records []ExtractedRecord
	seenTransIDs := make(map[int64]bool)
	var lastTransID int64 = 0

	// Scan antrean sampai terkumpul sejumlah 'limit' data valid yang memiliki data detail di kedua tabel,
	// atau sampai tidak ada lagi antrean yang belum diproses.
	for len(records) < limit {
		batchSize := 50
		var query string
		var rows *sql.Rows
		var err error

		if lastTransID > 0 {
			query = fmt.Sprintf(`
				SELECT TOP (%d) transid, ISNULL(ifacename, ''), ISNULL(ifacetbname, ''), ISNULL([action], '')
				FROM mxout_inter_trans
				WHERE (ax = 0 OR ax IS NULL) AND transid < @p1
				ORDER BY transid DESC
			`, batchSize)
			rows, err = r.DB.QueryContext(ctx, query, lastTransID)
		} else {
			query = fmt.Sprintf(`
				SELECT TOP (%d) transid, ISNULL(ifacename, ''), ISNULL(ifacetbname, ''), ISNULL([action], '')
				FROM mxout_inter_trans
				WHERE (ax = 0 OR ax IS NULL)
				ORDER BY transid DESC
			`, batchSize)
			rows, err = r.DB.QueryContext(ctx, query)
		}

		if err != nil {
			return nil, fmt.Errorf("error query mxout_inter_trans: %w", err)
		}

		type queueItem struct {
			transID     int64
			ifacename   string
			ifacetbname string
			action      string
		}

		var items []queueItem
		for rows.Next() {
			var it queueItem
			if err := rows.Scan(&it.transID, &it.ifacename, &it.ifacetbname, &it.action); err == nil {
				items = append(items, it)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("error iterating mxout_inter_trans rows: %w", err)
		}
		rows.Close()

		if len(items) == 0 {
			// Tidak ada lagi antrean yang belum diproses
			break
		}

		// Update cursor pagination ke transid terkecil di batch ini
		lastTransID = items[len(items)-1].transID

		for _, it := range items {
			if seenTransIDs[it.transID] {
				continue
			}
			seenTransIDs[it.transID] = true

			tbUpper := strings.ToUpper(strings.TrimSpace(it.ifacetbname))
			if tbUpper == "" {
				log.Printf("[SKIP] mxout_inter_trans transid=%d di-skip: ifacetbname kosong. Status ax tetap 0.", it.transID)
				// Status ax TETAP 0 (jangan di-markProcessed agar tidak hilang tanpa jejak)
				continue
			}

			var rec *ExtractedRecord
			var extractErr error

			switch tbUpper {
			case "AXMXPO":
				rec, extractErr = r.extractPO(ctx, it.transID, it.action)
			case "AXPR":
				rec, extractErr = r.extractPR(ctx, it.transID, it.action)
			case "AXRECEIPT":
				rec, extractErr = r.extractReceipt(ctx, it.transID, it.action)
			case "AXVENDOR", "VENDOR":
				rec, extractErr = r.extractVendor(ctx, it.transID, it.action)
			default:
				rec, extractErr = r.extractGeneric(ctx, it.transID, it.ifacetbname, it.action)
			}

			if extractErr != nil {
				log.Printf("[WARN] Gagal query detail transid %d (%s): %v. Status ax tetap 0.", it.transID, it.ifacetbname, extractErr)
				// Status ax TETAP 0 jika query detail error
				continue
			}

			// VALIDASI: Hanya proses jika data detail benar-benar ada di kedua tabel
			if rec == nil {
				log.Printf("[SKIP] mxout_inter_trans transid=%d (%s) di-skip: TIDAK ADA data detail terkait di tabel dbo.[%s]. Status ax tetap 0 (belum diposting ke staging).", it.transID, it.ifacename, it.ifacetbname)
				// PENTING: Status ax TETAP 0! Jangan tandai ax = 1 jika data detail belum ada di SQL Server.
				continue
			}

			// Pastikan IfaceTbName selalu terisi sesuai antrean sumber
			if rec.IfaceTbName == "" {
				rec.IfaceTbName = it.ifacetbname
			}

			// Data valid ditemukan lengkap di kedua tabel!
			records = append(records, *rec)
			if len(records) >= limit {
				break
			}
		}
	}

	return records, nil
}

// MarkProcessed menandai status ax = 1 pada mxout_inter_trans agar record tidak ditarik berulang.
// Menggunakan pasangan transid dan ifacetbname (persis seperti standar makro integrasi Maximo STG_MXOUT_UPDATE_START).
func (r *SQLServerRepository) MarkProcessed(ctx context.Context, transID string, ifacetbname ...string) error {
	if r.DB == nil {
		return nil
	}
	tbName := ""
	if len(ifacetbname) > 0 {
		tbName = strings.TrimSpace(ifacetbname[0])
	}

	if tbName != "" {
		_, err := r.DB.ExecContext(ctx, "UPDATE mxout_inter_trans SET ax = 1 WHERE transid = @p1 AND ifacetbname = @p2", transID, tbName)
		return err
	}

	_, err := r.DB.ExecContext(ctx, "UPDATE mxout_inter_trans SET ax = 1 WHERE transid = @p1", transID)
	return err
}

// InsertInboundControl mencatat transaksi masuk ke Maximo melalui tabel mxin_inter_trans
// untuk skenario feedback status atau data dari SAP kembali ke Maximo (sesuai makro STG_DATA_INSERT_MXIN).
func (r *SQLServerRepository) InsertInboundControl(ctx context.Context, rec InboundControlRecord) error {
	if r.DB == nil {
		return nil
	}
	if rec.Action == "" {
		rec.Action = "INSERT"
	}
	if rec.ExtSysName == "" {
		rec.ExtSysName = "SAP_GROW"
	}
	if rec.TransLanguage == "" {
		rec.TransLanguage = "EN"
	}

	query := `
		INSERT INTO mxin_inter_trans (ifacename, transid, [action], extsysname, translanguage)
		VALUES (@p1, @p2, @p3, @p4, @p5)
	`
	_, err := r.DB.ExecContext(ctx, query, rec.IfaceName, rec.TransID, rec.Action, rec.ExtSysName, rec.TransLanguage)
	return err
}

// Ekstraksi data PO Maximo (Header + Lines) dari tabel axmxpo
func (r *SQLServerRepository) extractPO(ctx context.Context, transID int64, action string) (*ExtractedRecord, error) {
	query := `
		SELECT 
			ISNULL(ponum, ''), ISNULL(vendor, ''), orderdate, ISNULL(currencycode, 'IDR'),
			ISNULL(status, 'APPR'), ISNULL(revisionnum, 0), ISNULL(siteid, ''),
			ISNULL(polinenum, 0), ISNULL(prnum, ''), ISNULL(prlinenum, 0), ISNULL(linetype, 'ITEM'),
			ISNULL(itemnum, ''), ISNULL(orderqty, 0), ISNULL(orderunit, 'PCS'), ISNULL(unitcost, 0),
			ISNULL(bpibudgetcode, ''), ISNULL(bpidepartment, ''), ISNULL(bpicoa, ''),
			ISNULL(taxcode1, ''), ISNULL(storeloc, ''), ISNULL(budgetyear, ''), transseq
		FROM axmxpo
		WHERE transid = @p1
		ORDER BY transseq ASC
	`
	rows, err := r.DB.QueryContext(ctx, query, transID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var poNum, vendor, currCode, status, siteID string
	var orderDate sql.NullTime
	var revisionNum int
	var lines []map[string]interface{}
	isFirst := true

	for rows.Next() {
		var pNum, vnd, cCode, st, sID, prNum, lType, itm, unit, bCode, dept, coa, tax1, sLoc, bYear string
		var oDate sql.NullTime
		var rev, pLine, prLine, seq int
		var qty, cost float64

		if err := rows.Scan(
			&pNum, &vnd, &oDate, &cCode, &st, &rev, &sID,
			&pLine, &prNum, &prLine, &lType, &itm, &qty, &unit, &cost,
			&bCode, &dept, &coa, &tax1, &sLoc, &bYear, &seq,
		); err != nil {
			continue
		}

		if isFirst {
			poNum = pNum
			vendor = vnd
			orderDate = oDate
			currCode = cCode
			status = st
			revisionNum = rev
			siteID = sID
			isFirst = false
		}

		lines = append(lines, map[string]interface{}{
			"POLINENUM":   pLine,
			"PRNUM":       prNum,
			"PRLINENUM":   prLine,
			"LINETYPE":    lType,
			"ITEMNUM":     itm,
			"ORDERUNIT":   unit,
			"QTY":         qty,
			"UNITCOST":    cost,
			"BUDGETCODE":  bCode,
			"DEPARTMENT":  dept,
			"MAINACCOUNT": coa,
			"TAXCODE1":    tax1,
			"STORELOC":    sLoc,
			"BUDGETYEAR":  bYear,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating axmxpo rows: %w", err)
	}

	// Jika tidak ada line/data detail sama sekali di axmxpo untuk transid ini
	if isFirst || len(lines) == 0 {
		return nil, nil
	}

	dateStr := time.Now().Format("2006-01-02")
	if orderDate.Valid {
		dateStr = orderDate.Time.Format("2006-01-02")
	}

	poPayload := map[string]interface{}{
		"PONUM":        poNum,
		"VENDOR":       vendor,
		"ORDERDATE":    dateStr,
		"CURRENCYCODE": currCode,
		"STATUS":       status,
		"REVISIONNUM":  revisionNum,
		"SITEID":       siteID,
		"ACTION":       action,
		"LINES":        lines,
	}

	payloadBytes, err := json.Marshal(poPayload)
	if err != nil {
		return nil, err
	}

	return &ExtractedRecord{
		TransID:       fmt.Sprintf("%d", transID),
		IfaceTbName:   "axmxpo",
		DocType:       "PO",
		DocumentNo:    poNum,
		PayloadJSON:   payloadBytes,
		LastUpdatedAt: time.Now(),
	}, nil
}

// Ekstraksi data PR Maximo dari tabel axpr
func (r *SQLServerRepository) extractPR(ctx context.Context, transID int64, action string) (*ExtractedRecord, error) {
	query := `
		SELECT 
			ISNULL(prnum, ''), ISNULL(requestedby, ''), issuedate, ISNULL(status, 'APPR'), ISNULL(siteid, ''),
			ISNULL(prlinenum, 0), ISNULL(itemnum, ''), ISNULL(linetype, 'ITEM'), ISNULL(orderqty, 0),
			ISNULL(orderunit, 'PCS'), ISNULL(bpibudgetcode, ''), ISNULL(bpidepartment, ''), ISNULL(bpicoa, ''), transseq
		FROM axpr
		WHERE transid = @p1
		ORDER BY transseq ASC
	`
	rows, err := r.DB.QueryContext(ctx, query, transID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prNum, reqBy, status, siteID string
	var issueDate sql.NullTime
	var lines []map[string]interface{}
	isFirst := true

	for rows.Next() {
		var pNum, rBy, st, sID, itm, lType, unit, bCode, dept, coa string
		var iDate sql.NullTime
		var prLine, seq int
		var qty float64

		if err := rows.Scan(&pNum, &rBy, &iDate, &st, &sID, &prLine, &itm, &lType, &qty, &unit, &bCode, &dept, &coa, &seq); err != nil {
			continue
		}

		if isFirst {
			prNum = pNum
			reqBy = rBy
			issueDate = iDate
			status = st
			siteID = sID
			isFirst = false
		}

		lines = append(lines, map[string]interface{}{
			"PRLINENUM":   prLine,
			"ITEMNUM":     itm,
			"LINETYPE":    lType,
			"ORDERQTY":    qty,
			"ORDERUNIT":   unit,
			"BUDGETCODE":  bCode,
			"DEPARTMENT":  dept,
			"MAINACCOUNT": coa,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating axpr rows: %w", err)
	}

	// Jika tidak ada data detail sama sekali di axpr untuk transid ini
	if isFirst || len(lines) == 0 {
		return nil, nil
	}

	dateStr := time.Now().Format("2006-01-02")
	if issueDate.Valid {
		dateStr = issueDate.Time.Format("2006-01-02")
	}

	prPayload := map[string]interface{}{
		"PRNUM":       prNum,
		"REQUESTEDBY": reqBy,
		"ISSUEDATE":   dateStr,
		"STATUS":      status,
		"SITEID":      siteID,
		"ACTION":      action,
		"LINES":       lines,
	}

	payloadBytes, _ := json.Marshal(prPayload)
	return &ExtractedRecord{
		TransID:       fmt.Sprintf("%d", transID),
		IfaceTbName:   "axpr",
		DocType:       "PR",
		DocumentNo:    prNum,
		PayloadJSON:   payloadBytes,
		LastUpdatedAt: time.Now(),
	}, nil
}

// Ekstraksi data Penerimaan Barang (GRN/Receipt) dari tabel axreceipt
func (r *SQLServerRepository) extractReceipt(ctx context.Context, transID int64, action string) (*ExtractedRecord, error) {
	query := `
		SELECT 
			ISNULL(bpigrnnumber, ''), ISNULL(ponum, ''), ISNULL(polinenum, 0), ISNULL(itemnum, ''),
			ISNULL(qty, 0), actualdate, ISNULL(status, 'APPR'), transseq
		FROM axreceipt
		WHERE transid = @p1
		ORDER BY transseq ASC
	`
	rows, err := r.DB.QueryContext(ctx, query, transID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var grnNo, poNum, status string
	var actDate sql.NullTime
	var lines []map[string]interface{}
	isFirst := true

	for rows.Next() {
		var gNo, pNo, itm, st string
		var pLine, seq int
		var qty float64
		var aDate sql.NullTime

		if err := rows.Scan(&gNo, &pNo, &pLine, &itm, &qty, &aDate, &st, &seq); err != nil {
			continue
		}

		if isFirst {
			grnNo = gNo
			poNum = pNo
			status = st
			actDate = aDate
			isFirst = false
		}

		lines = append(lines, map[string]interface{}{
			"POLINENUM": pLine,
			"ITEMNUM":   itm,
			"QTY":       qty,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating axreceipt rows: %w", err)
	}

	// Jika tidak ada data detail sama sekali di axreceipt untuk transid ini
	if isFirst || len(lines) == 0 {
		return nil, nil
	}

	docNo := grnNo
	if docNo == "" {
		docNo = fmt.Sprintf("GRN-%s-%d", poNum, transID)
	}

	dateStr := time.Now().Format("2006-01-02")
	if actDate.Valid {
		dateStr = actDate.Time.Format("2006-01-02")
	}

	receiptPayload := map[string]interface{}{
		"GRNNUM":     docNo,
		"PONUM":      poNum,
		"ACTUALDATE": dateStr,
		"STATUS":     status,
		"ACTION":     action,
		"LINES":      lines,
	}

	payloadBytes, _ := json.Marshal(receiptPayload)
	return &ExtractedRecord{
		TransID:       fmt.Sprintf("%d", transID),
		IfaceTbName:   "axreceipt",
		DocType:       "GRN",
		DocumentNo:    docNo,
		PayloadJSON:   payloadBytes,
		LastUpdatedAt: time.Now(),
	}, nil
}

// Ekstraksi data Vendor / Business Partner Maximo dari tabel axvendor
func (r *SQLServerRepository) extractVendor(ctx context.Context, transID int64, action string) (*ExtractedRecord, error) {
	if r.DB == nil {
		return nil, nil
	}
	query := `
		SELECT 
			ISNULL(bpivendorgroup, ''), 
			ISNULL(company, ''), 
			ISNULL(currencycode, 'IDR'), 
			ISNULL(name, ''), 
			ISNULL(orgid, 'BPI'), 
			transseq
		FROM axvendor
		WHERE transid = @p1
		ORDER BY transseq ASC
	`
	rows, err := r.DB.QueryContext(ctx, query, transID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bpiVendorGroup, company, currCode, name, orgID string
	var seq int
	found := false

	for rows.Next() {
		var bvg, comp, cCode, nm, org string
		var sq int
		if err := rows.Scan(&bvg, &comp, &cCode, &nm, &org, &sq); err != nil {
			continue
		}
		if !found {
			bpiVendorGroup = bvg
			company = comp
			currCode = cCode
			name = nm
			orgID = org
			seq = sq
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating axvendor rows: %w", err)
	}

	if !found || strings.TrimSpace(company) == "" {
		log.Printf("[DEBUG] extractVendor: tidak ada baris di axvendor dengan transid=%d (periksa kecocokan transid antara mxout_inter_trans dan axvendor)", transID)
		return nil, nil
	}

	if action == "" {
		action = "Add"
	}

	vendorPayload := map[string]interface{}{
		"BPIVENDORGROUP": bpiVendorGroup,
		"COMPANY":        company,
		"CURRENCYCODE":   currCode,
		"NAME":           name,
		"ORGID":          orgID,
		"TRANSID":        fmt.Sprintf("%d", transID),
		"TRANSSEQ":       seq,
		"ACTION":         action,
	}

	payloadBytes, err := json.Marshal(vendorPayload)
	if err != nil {
		return nil, err
	}

	return &ExtractedRecord{
		TransID:       fmt.Sprintf("%d", transID),
		IfaceTbName:   "axvendor",
		DocType:       "AXVENDOR",
		DocumentNo:    company,
		PayloadJSON:   payloadBytes,
		LastUpdatedAt: time.Now(),
	}, nil
}

// ExtractVendorByTransID mengekstrak vendor tertentu berdasarkan transid (bisa dipakai untuk pengujian atau manual ingestion)
func (r *SQLServerRepository) ExtractVendorByTransID(ctx context.Context, transID int64) (*ExtractedRecord, error) {
	return r.extractVendor(ctx, transID, "Add")
}

// Ekstraksi umum untuk tabel-tabel lainnya (AXITEM, AXUOM, AXVENDOR, AXBUDGET, AXINVISSUE, dll)
func (r *SQLServerRepository) extractGeneric(ctx context.Context, transID int64, tableName, action string) (*ExtractedRecord, error) {
	// Sanitasi nama tabel untuk keamanan query
	cleanTable := strings.ReplaceAll(tableName, ";", "")
	cleanTable = strings.ReplaceAll(cleanTable, "--", "")
	cleanTable = strings.ReplaceAll(cleanTable, " ", "")

	if cleanTable == "" {
		return nil, nil
	}

	// Cek apakah tabel ada di database
	var tableExists int
	checkQuery := fmt.Sprintf("IF OBJECT_ID('dbo.[%s]', 'U') IS NOT NULL SELECT 1 ELSE SELECT 0", cleanTable)
	if err := r.DB.QueryRowContext(ctx, checkQuery).Scan(&tableExists); err != nil || tableExists == 0 {
		return nil, nil
	}

	query := fmt.Sprintf("SELECT * FROM dbo.[%s] WHERE transid = @p1", cleanTable)
	rows, err := r.DB.QueryContext(ctx, query, transID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			continue
		}

		m := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			if b, ok := val.([]byte); ok {
				m[colName] = string(b)
			} else {
				m[colName] = val
			}
		}
		results = append(results, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating generic table rows: %w", err)
	}

	// JIKA TIDAK ADA DATA DETAIL (results kosong), JANGAN DIKIRIM!
	if len(results) == 0 {
		return nil, nil
	}

	docNo := fmt.Sprintf("%s-%d", cleanTable, transID)
	payload := map[string]interface{}{
		"TABLENAME": cleanTable,
		"TRANSID":   transID,
		"ACTION":    action,
		"DATA":      results,
	}

	payloadBytes, _ := json.Marshal(payload)
	return &ExtractedRecord{
		TransID:       fmt.Sprintf("%d", transID),
		IfaceTbName:   cleanTable,
		DocType:       cleanTable,
		DocumentNo:    docNo,
		PayloadJSON:   payloadBytes,
		LastUpdatedAt: time.Now(),
	}, nil
}
