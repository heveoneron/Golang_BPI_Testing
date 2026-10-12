package controllers

import (
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type BackupScheduleController struct {
	service services.BackupScheduleService
}

func NewBackupScheduleController(s services.BackupScheduleService) *BackupScheduleController {
	return &BackupScheduleController{service: s}
}

func (c *BackupScheduleController) ShowConfigPage(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	schedules, err := c.service.FetchAllSchedules()
	if err != nil {
		ctx.HTML(http.StatusInternalServerError, "backup_config.html", gin.H{
			"Error":    "Failed to load schedules",
			"Username": session.Get("username"),
		})
		return
	}

	currentDB := os.Getenv("DB_NAME")
	if currentDB == "" {
		currentDB = "Unknown DB (Check .env)"
	}

	ctx.HTML(http.StatusOK, "backup_config.html", gin.H{
		"Username":   session.Get("username"),
		"schedules":  schedules,
		"Success":    ctx.Query("success") == "1",
		"CurrentDB":  currentDB,
		"BasePath":   "/var/backups/",
		"ActiveMenu": "backup",
	})
}

// Endpoint untuk menyimpan perubahan Form
func (c *BackupScheduleController) UpdateSchedule(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.PostForm("id"))
	retention, _ := strconv.Atoi(ctx.PostForm("retention_days"))

	weeklyDays := strings.Join(ctx.PostFormArray("weekly_days[]"), ",")
	monthlyDays := strings.Join(ctx.PostFormArray("monthly_days[]"), ",")

	currentDB := os.Getenv("DB_NAME")
	if currentDB == "" {
		currentDB = "middleware_db_unknown"
	}

	schedule := &models.BackupSchedule{
		ID: id,

		// 1. Injeksi Identitas Target Dinamis (Bukan dari Form Frontend)
		DatabaseName:   currentDB,
		DatabaseType:   "PostgreSQL",
		BackupLevel:    "database",
		BackupLocation: "/var/backups/",

		// 2. Data Penjadwalan dari Form Frontend
		ScheduleMode:   ctx.PostForm("schedule_mode"),
		ExecutionTime:  ctx.PostForm("execution_time"),
		WeeklyDays:     weeklyDays,
		MonthlyDays:    monthlyDays,
		CronExpression: ctx.PostForm("cron_expression"),
		RetentionDays:  retention,
		IsActive:       ctx.PostForm("is_active") == "on",
	}

	if err := c.service.SaveSchedule(schedule); err != nil {
		ctx.HTML(http.StatusInternalServerError, "backup_config.html", gin.H{
			"Error": "Gagal menyimpan konfigurasi: " + err.Error(),
		})
		return
	}

	ctx.Redirect(http.StatusFound, "/backup-config?success=1")
}

// Endpoint untuk mengeksekusi Backup manual (Tombol "Run Backup Now")
func (c *BackupScheduleController) HandleBackupNow(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.PostForm("id"))

	if err := c.service.ExecuteBackupNow(id); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Manual backup triggered successfully!"})
}
