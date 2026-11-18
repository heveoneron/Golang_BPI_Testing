package repository

import (
	"context"
	"fmt"
)

// ClosePeriod adalah status tutup buku satu bulan (menu Panel > Close Period).
type ClosePeriod struct {
	Year      int    `json:"year"`
	Month     int    `json:"month"`
	IsClosed  bool   `json:"is_closed"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// ClosedPeriods mengembalikan himpunan (tahun, bulan) yang sudah ditutup. Bulan tanpa baris = terbuka.
func (r *PostgresRepository) ClosedPeriods(ctx context.Context) (map[[2]int]bool, error) {
	if r == nil || r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	rows, err := r.Pool.Query(ctx, `SELECT period_year, period_month FROM close_periods WHERE is_closed`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[[2]int]bool{}
	for rows.Next() {
		var y, m int
		if err := rows.Scan(&y, &m); err != nil {
			return nil, err
		}
		out[[2]int{y, m}] = true
	}
	return out, rows.Err()
}

// SetClosePeriods menyimpan status 12 bulan satu tahun (dipakai test & API; Panel menulis lewat GORM).
func (r *PostgresRepository) SetClosePeriods(ctx context.Context, items []ClosePeriod) error {
	if r == nil || r.Pool == nil {
		return fmt.Errorf("postgres pool not available")
	}
	for _, p := range items {
		if p.Month < 1 || p.Month > 12 || p.Year < 2000 {
			return fmt.Errorf("periode tidak valid: %d-%02d", p.Year, p.Month)
		}
		if _, err := r.Pool.Exec(ctx, `
			INSERT INTO close_periods (period_year, period_month, is_closed, updated_by, updated_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (period_year, period_month) DO UPDATE SET
				is_closed = EXCLUDED.is_closed, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
			p.Year, p.Month, p.IsClosed, p.UpdatedBy); err != nil {
			return err
		}
	}
	return nil
}
