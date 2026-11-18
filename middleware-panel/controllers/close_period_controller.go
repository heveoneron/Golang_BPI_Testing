package controllers

import (
	"middleware-panel/repository"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// ClosePeriodController melayani menu Configuration > Close Period (FSD Close Period Service):
// bulan yang dicentang tutup membuat PostingDate GRN digeser ke tanggal 1 bulan terbuka berikutnya.
type ClosePeriodController struct {
	repo repository.ClosePeriodRepository
}

func NewClosePeriodController(repo repository.ClosePeriodRepository) *ClosePeriodController {
	return &ClosePeriodController{repo: repo}
}

var monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

func (c *ClosePeriodController) ShowClosePeriod(ctx *gin.Context) {
	username, ok := sessionUser(ctx)
	if !ok {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}
	year := time.Now().Year()
	if y, err := strconv.Atoi(ctx.Query("year")); err == nil && y >= 2000 && y <= 2100 {
		year = y
	}
	periods, err := c.repo.GetYear(year)
	loadErr := ""
	if err != nil {
		loadErr = "Tabel close_periods belum bisa dibaca: " + err.Error()
	}
	type row struct {
		repository.ClosePeriod
		Name string
	}
	rows := make([]row, 0, 12)
	closedCount := 0
	for _, p := range periods {
		rows = append(rows, row{ClosePeriod: p, Name: monthNames[p.Month-1]})
		if p.IsClosed {
			closedCount++
		}
	}
	years := []int{}
	for y := time.Now().Year() - 2; y <= time.Now().Year()+1; y++ {
		years = append(years, y)
	}
	ctx.HTML(http.StatusOK, "close_period.html", gin.H{
		"Title":       "Close Period",
		"Username":    username,
		"Year":        year,
		"Years":       years,
		"Rows":        rows,
		"ClosedCount": closedCount,
		"LoadError":   loadErr,
		"ActiveMenu":  "close_period",
	})
}

func (c *ClosePeriodController) HandleSave(ctx *gin.Context) {
	username, ok := sessionUser(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}
	var req struct {
		Year         int   `json:"year"`
		ClosedMonths []int `json:"closed_months"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Format tidak valid"})
		return
	}
	if err := c.repo.SaveYear(req.Year, req.ClosedMonths, username); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Close Period " + strconv.Itoa(req.Year) + " disimpan. Berlaku untuk GRN yang ditransformasi berikutnya."})
}
