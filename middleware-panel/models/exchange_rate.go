package models

import (
	"time"
)

type ExchangeRate struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Event        string     `gorm:"type:varchar(100)" json:"event"`
	Source       string     `gorm:"type:varchar(100)" json:"source"`
	KMKNumber    string     `gorm:"column:kmk_number;type:varchar(100)" json:"kmk_number"`
	ValidFrom    *time.Time `gorm:"column:valid_from;type:date" json:"valid_from"`
	ValidTo      *time.Time `gorm:"column:valid_to;type:date" json:"valid_to"`
	DocumentURL  string     `gorm:"column:document_url;type:text" json:"document_url"`
	CurrencyCode string     `gorm:"column:currency_code;type:varchar(10);not null" json:"currency_code"`
	CurrencyName string     `gorm:"column:currency_name;type:varchar(100)" json:"currency_name"`
	Rate         float64    `gorm:"column:rate;type:numeric(18,4);not null" json:"rate"`
	Unit         int        `gorm:"column:unit;default:1" json:"unit"`
	RatePerUnit  *float64   `gorm:"column:rate_per_unit;type:numeric(18,6)" json:"rate_per_unit"`
	Change       *float64   `gorm:"column:change;type:numeric(18,4)" json:"change"`
	RawPayload   string     `gorm:"column:raw_payload;type:jsonb" json:"raw_payload"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

func (ExchangeRate) TableName() string {
	return "exchange_rates"
}

type ExchangeRateSummary struct {
	TotalRecords    int64
	TotalCurrencies int64
	LatestKMK       string
	LastUpdated     time.Time
}

func (e ExchangeRate) HasChange() bool {
	return e.Change != nil
}

func (e ExchangeRate) ChangeVal() float64 {
	if e.Change == nil {
		return 0
	}
	return *e.Change
}

func (e ExchangeRate) RatePerUnitVal() float64 {
	if e.RatePerUnit == nil {
		return e.Rate
	}
	return *e.RatePerUnit
}
