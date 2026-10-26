package controllers

import (
	"io"
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type ExchangeRateController struct {
	svc services.ExchangeRateService
}

func NewExchangeRateController(svc services.ExchangeRateService) *ExchangeRateController {
	return &ExchangeRateController{svc: svc}
}

func (c *ExchangeRateController) ShowExchangeRates(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	username := "Admin"
	if u := session.Get("username"); u != nil {
		username = u.(string)
	}

	search := ctx.Query("q")
	limit := 100
	if l := ctx.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}

	rates, err := c.svc.GetRates(limit, search)
	if err != nil {
		rates = []models.ExchangeRate{}
	}

	summary, err := c.svc.GetSummary()
	if err != nil {
		summary = models.ExchangeRateSummary{}
	}

	ctx.HTML(http.StatusOK, "exchange_rates.html", gin.H{
		"Title":      "Master Data Exchange Rate (Kurs Pajak)",
		"Username":   username,
		"Rates":      rates,
		"Summary":    summary,
		"Search":     search,
		"ActiveMenu": "exchange_rates",
	})
}

func (c *ExchangeRateController) HandleUploadJSON(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var rawBytes []byte
	var err error

	// Cek apakah payload dikirim via form field 'raw_json' atau body langsung
	rawJSONStr := ctx.PostForm("raw_json")
	if strings.TrimSpace(rawJSONStr) != "" {
		rawBytes = []byte(rawJSONStr)
	} else {
		rawBytes, err = io.ReadAll(ctx.Request.Body)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Gagal membaca body request: " + err.Error()})
			return
		}
	}

	if len(rawBytes) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Payload JSON kosong"})
		return
	}

	count, err := c.svc.ProcessJSONPayload(rawBytes)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Gagal memproses payload kurs: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"message":     "Berhasil mengupload dan menyimpan " + strconv.Itoa(count) + " baris data Exchange Rate",
		"rates_count": count,
	})
}

func (c *ExchangeRateController) GetRatesAPI(ctx *gin.Context) {
	search := ctx.Query("q")
	limit := 100
	if l := ctx.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}

	rates, err := c.svc.GetRates(limit, search)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"total":  len(rates),
		"data":   rates,
	})
}
