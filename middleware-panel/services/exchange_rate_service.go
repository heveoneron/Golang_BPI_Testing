package services

import (
	"encoding/json"
	"fmt"
	"middleware-panel/models"
	"middleware-panel/repository"
	"strconv"
	"time"
)

type ExchangeRatePayload struct {
	Event  string `json:"event"`
	SentAt string `json:"sent_at"`
	Source string `json:"source"`
	Period struct {
		KMKNumber   string `json:"kmk_number"`
		ValidFrom   string `json:"valid_from"`
		ValidTo     string `json:"valid_to"`
		DocumentURL string `json:"document_url"`
	} `json:"period"`
	Rates []struct {
		CurrencyCode string   `json:"currency_code"`
		CurrencyName string   `json:"currency_name"`
		Rate         float64  `json:"rate"`
		Unit         int      `json:"unit"`
		RatePerUnit  string   `json:"rate_per_unit"`
		Change       *float64 `json:"change"`
	} `json:"rates"`
}

type ExchangeRateService interface {
	GetRates(limit int, search string) ([]models.ExchangeRate, error)
	GetSummary() (models.ExchangeRateSummary, error)
	ProcessJSONPayload(rawJSON []byte) (int, error)
}

type exchangeRateService struct {
	repo repository.ExchangeRateRepository
}

func NewExchangeRateService(repo repository.ExchangeRateRepository) ExchangeRateService {
	return &exchangeRateService{repo: repo}
}

func (s *exchangeRateService) GetRates(limit int, search string) ([]models.ExchangeRate, error) {
	return s.repo.GetAll(limit, search)
}

func (s *exchangeRateService) GetSummary() (models.ExchangeRateSummary, error) {
	return s.repo.GetSummary()
}

func (s *exchangeRateService) ProcessJSONPayload(rawJSON []byte) (int, error) {
	var payload ExchangeRatePayload
	if err := json.Unmarshal(rawJSON, &payload); err != nil {
		return 0, fmt.Errorf("invalid json payload: %w", err)
	}

	if len(payload.Rates) == 0 {
		return 0, fmt.Errorf("rates list is empty")
	}

	var validFrom *time.Time
	if payload.Period.ValidFrom != "" {
		if t, err := time.Parse("2006-01-02", payload.Period.ValidFrom); err == nil {
			validFrom = &t
		}
	}

	var validTo *time.Time
	if payload.Period.ValidTo != "" {
		if t, err := time.Parse("2006-01-02", payload.Period.ValidTo); err == nil {
			validTo = &t
		}
	}

	var entities []models.ExchangeRate
	for _, r := range payload.Rates {
		rpu, _ := strconv.ParseFloat(r.RatePerUnit, 64)
		if rpu == 0 && r.Unit > 0 {
			rpu = r.Rate / float64(r.Unit)
		}

		unit := r.Unit
		if unit <= 0 {
			unit = 1
		}

		item := models.ExchangeRate{
			Event:        payload.Event,
			Source:       payload.Source,
			KMKNumber:    payload.Period.KMKNumber,
			ValidFrom:    validFrom,
			ValidTo:      validTo,
			DocumentURL:  payload.Period.DocumentURL,
			CurrencyCode: r.CurrencyCode,
			CurrencyName: r.CurrencyName,
			Rate:         r.Rate,
			Unit:         unit,
			RatePerUnit:  &rpu,
			Change:       r.Change,
			RawPayload:   string(rawJSON),
			CreatedAt:    time.Now(),
		}
		entities = append(entities, item)
	}

	if err := s.repo.CreateBatch(entities); err != nil {
		return 0, err
	}

	return len(entities), nil
}
