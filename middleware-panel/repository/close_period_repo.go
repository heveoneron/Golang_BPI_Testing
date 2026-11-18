package repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ClosePeriod = satu bulan pada menu Close Period (tabel close_periods, dibaca Engine saat GRN POST/PATCH).
type ClosePeriod struct {
	Year      int        `gorm:"column:period_year;primaryKey" json:"year"`
	Month     int        `gorm:"column:period_month;primaryKey" json:"month"`
	IsClosed  bool       `gorm:"column:is_closed" json:"is_closed"`
	UpdatedBy *string    `gorm:"column:updated_by" json:"updated_by"`
	UpdatedAt *time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ClosePeriod) TableName() string { return "close_periods" }

// DDL sama dengan migrasi Engine (enhancement.sql) agar menu bisa dipakai walau Engine belum pernah jalan.
const closePeriodDDL = `
	CREATE TABLE IF NOT EXISTS close_periods (
		period_year  INT NOT NULL,
		period_month INT NOT NULL CHECK (period_month BETWEEN 1 AND 12),
		is_closed    BOOLEAN NOT NULL DEFAULT FALSE,
		updated_by   VARCHAR(100),
		updated_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		PRIMARY KEY (period_year, period_month)
	)`

type ClosePeriodRepository interface {
	// GetYear selalu mengembalikan 12 bulan (bulan tanpa baris = terbuka).
	GetYear(year int) ([]ClosePeriod, error)
	SaveYear(year int, closedMonths []int, username string) error
}

type closePeriodRepo struct{ db *gorm.DB }

func NewClosePeriodRepository(db *gorm.DB) ClosePeriodRepository {
	_ = db.Exec(closePeriodDDL).Error
	return &closePeriodRepo{db: db}
}

func (r *closePeriodRepo) GetYear(year int) ([]ClosePeriod, error) {
	var rows []ClosePeriod
	if err := r.db.Where("period_year = ?", year).Find(&rows).Error; err != nil {
		return nil, err
	}
	byMonth := map[int]ClosePeriod{}
	for _, p := range rows {
		byMonth[p.Month] = p
	}
	out := make([]ClosePeriod, 12)
	for m := 1; m <= 12; m++ {
		if p, ok := byMonth[m]; ok {
			out[m-1] = p
		} else {
			out[m-1] = ClosePeriod{Year: year, Month: m}
		}
	}
	return out, nil
}

func (r *closePeriodRepo) SaveYear(year int, closedMonths []int, username string) error {
	if year < 2000 || year > 2100 {
		return fmt.Errorf("tahun tidak valid: %d", year)
	}
	closed := map[int]bool{}
	for _, m := range closedMonths {
		if m < 1 || m > 12 {
			return fmt.Errorf("bulan tidak valid: %d", m)
		}
		closed[m] = true
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		for m := 1; m <= 12; m++ {
			if err := tx.Exec(`
				INSERT INTO close_periods (period_year, period_month, is_closed, updated_by, updated_at)
				VALUES (?, ?, ?, ?, NOW())
				ON CONFLICT (period_year, period_month) DO UPDATE SET
					is_closed = EXCLUDED.is_closed,
					updated_by = CASE WHEN close_periods.is_closed = EXCLUDED.is_closed THEN close_periods.updated_by ELSE EXCLUDED.updated_by END,
					updated_at = CASE WHEN close_periods.is_closed = EXCLUDED.is_closed THEN close_periods.updated_at ELSE NOW() END`,
				year, m, closed[m], username).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
