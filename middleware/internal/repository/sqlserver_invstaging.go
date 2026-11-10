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
WHERE transdate >= @p2
ORDER BY transdate, matusetransid`

const axInvTransQuery = `
SELECT TOP (@p1) invtransid, ISNULL(itemnum,''), ISNULL(quantity,0), ISNULL(siteid,''), ISNULL(storeloc,''),
       transdate, ISNULL(transtype,''), '', '', '', ISNULL(conditioncode,'')
FROM dbo.axinvtrans
WHERE transdate >= @p2
ORDER BY transdate, invtransid`

// ExtractInventoryStaging membaca baris GI & Stock Adjustment sejak waktu tertentu (read-only).
func (r *SQLServerRepository) ExtractInventoryStaging(ctx context.Context, since time.Time, limit int) ([]InventoryStagingRow, error) {
	if r == nil || r.DB == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}
	var out []InventoryStagingRow
	for _, src := range []struct{ name, query string }{{"axinvissue", axInvIssueQuery}, {"axinvtrans", axInvTransQuery}} {
		rows, err := r.DB.QueryContext(ctx, src.query, limit, since)
		if err != nil {
			return out, fmt.Errorf("query %s: %w", src.name, err)
		}
		for rows.Next() {
			var row InventoryStagingRow
			var transDate sql.NullTime
			if err := rows.Scan(&row.SourceID, &row.ItemNum, &row.Quantity, &row.SiteID, &row.StoreLoc, &transDate,
				&row.TransType, &row.RefWO, &row.InvUseNum, &row.Description, &row.ConditionCode); err != nil {
				rows.Close()
				return out, fmt.Errorf("scan %s: %w", src.name, err)
			}
			row.Source = src.name
			row.TransDate = transDate.Time
			row.ItemNum = strings.TrimSpace(row.ItemNum)
			row.TransType = strings.ToUpper(strings.TrimSpace(row.TransType))
			out = append(out, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
