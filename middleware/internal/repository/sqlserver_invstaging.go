package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// InventoryStagingRow adalah satu baris staging Maximo [axinvissue] (Goods Issue) atau [axinvtrans] (Stock Adjustment).
type InventoryStagingRow struct {
	Source        string // "axinvissue" | "axinvtrans"
	SourceID      int64  // matusetransid | invtransid
	ItemNum       string
	Quantity      float64
	SiteID        string
	StoreLoc      string
	TransDate     time.Time
	TransType     string // issuetype (axinvissue) | transtype (axinvtrans)
	RefWO         string
	InvUseNum     string
	Description   string
	ConditionCode string
}

const axInvIssueQuery = `
SELECT TOP (@p1) matusetransid, ISNULL(itemnum,''), ISNULL(quantity,0), ISNULL(siteid,''), ISNULL(storeloc,''),
       transdate, ISNULL(issuetype,''), ISNULL(refwo,''), ISNULL(invusenum,''), ISNULL(description,''), ISNULL(conditioncode,'')
FROM dbo.axinvissue
WHERE transdate >= @p2 AND matusetransid > @p3
ORDER BY matusetransid`

const axInvTransQuery = `
SELECT TOP (@p1) invtransid, ISNULL(itemnum,''), ISNULL(quantity,0), ISNULL(siteid,''), ISNULL(storeloc,''),
       transdate, ISNULL(transtype,''), '', '', '', ISNULL(conditioncode,'')
FROM dbo.axinvtrans
WHERE transdate >= @p2 AND invtransid > @p3
ORDER BY invtransid`

// maxInventoryPages membatasi satu siklus agar tidak memuat seluruh histori sekaligus.
const maxInventoryPages = 20

// ExtractInventoryStaging membaca semua baris GI & Stock Adjustment sejak waktu tertentu (read-only),
// dipaginasi per ID agar baris baru tidak tertahan di belakang baris lama yang sudah diproses.
func (r *SQLServerRepository) ExtractInventoryStaging(ctx context.Context, since time.Time, pageSize int) ([]InventoryStagingRow, error) {
	if r == nil || r.DB == nil {
		return nil, nil
	}
	if pageSize <= 0 {
		pageSize = 500
	}
	var out []InventoryStagingRow
	for _, src := range []struct{ name, query string }{{"axinvissue", axInvIssueQuery}, {"axinvtrans", axInvTransQuery}} {
		var lastID int64
		for page := 0; page < maxInventoryPages; page++ {
			n, err := r.scanInventoryPage(ctx, src.name, src.query, pageSize, since, &lastID, &out)
			if err != nil {
				return out, err
			}
			if n < pageSize {
				break
			}
		}
	}
	return out, nil
}

func (r *SQLServerRepository) scanInventoryPage(ctx context.Context, name, query string, pageSize int, since time.Time, lastID *int64, out *[]InventoryStagingRow) (int, error) {
	rows, err := r.DB.QueryContext(ctx, query, pageSize, since, *lastID)
	if err != nil {
		return 0, fmt.Errorf("query %s: %w", name, err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var row InventoryStagingRow
		var transDate sql.NullTime
		if err := rows.Scan(&row.SourceID, &row.ItemNum, &row.Quantity, &row.SiteID, &row.StoreLoc, &transDate,
			&row.TransType, &row.RefWO, &row.InvUseNum, &row.Description, &row.ConditionCode); err != nil {
			return n, fmt.Errorf("scan %s: %w", name, err)
		}
		row.Source = name
		row.TransDate = transDate.Time
		row.ItemNum = strings.TrimSpace(row.ItemNum)
		row.TransType = strings.ToUpper(strings.TrimSpace(row.TransType))
		*out = append(*out, row)
		*lastID = row.SourceID
		n++
	}
	return n, rows.Err()
}
