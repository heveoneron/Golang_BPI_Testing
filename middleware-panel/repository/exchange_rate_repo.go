package repository

import (
	"middleware-panel/models"
	"strings"
	"time"

	"gorm.io/gorm"
)

type ExchangeRateRepository interface {
	GetAll(limit int, search string) ([]models.ExchangeRate, error)
	GetSummary() (models.ExchangeRateSummary, error)
	CreateBatch(rates []models.ExchangeRate) error
}

type exchangeRateRepo struct {
	db *gorm.DB
}

func NewExchangeRateRepository(db *gorm.DB) ExchangeRateRepository {
	return &exchangeRateRepo{db: db}
}

func (r *exchangeRateRepo) GetAll(limit int, search string) ([]models.ExchangeRate, error) {
	if limit <= 0 {
		limit = 100
	}
	var list []models.ExchangeRate
	query := r.db.Model(&models.ExchangeRate{})

	if search != "" {
		s := "%" + strings.ToUpper(strings.TrimSpace(search)) + "%"
		query = query.Where("UPPER(currency_code) LIKE ? OR UPPER(currency_name) LIKE ? OR UPPER(kmk_number) LIKE ?", s, s, s)
	}

	err := query.Order("id desc").Limit(limit).Find(&list).Error
	return list, err
}

func (r *exchangeRateRepo) GetSummary() (models.ExchangeRateSummary, error) {
	var summary models.ExchangeRateSummary
	
	// Total records
	_ = r.db.Model(&models.ExchangeRate{}).Count(&summary.TotalRecords).Error

	// Total distinct currencies
	_ = r.db.Model(&models.ExchangeRate{}).Distinct("currency_code").Count(&summary.TotalCurrencies).Error

	// Latest KMK & Last updated
	var latest models.ExchangeRate
	if err := r.db.Model(&models.ExchangeRate{}).Order("id desc").First(&latest).Error; err == nil {
		summary.LatestKMK = latest.KMKNumber
		summary.LastUpdated = latest.CreatedAt
	} else {
		summary.LatestKMK = "-"
		summary.LastUpdated = time.Now()
	}

	return summary, nil
}

func (r *exchangeRateRepo) CreateBatch(rates []models.ExchangeRate) error {
	if len(rates) == 0 {
		return nil
	}
	return r.db.Create(&rates).Error
}
