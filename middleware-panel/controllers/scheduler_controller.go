package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type SchedulerController struct {
	service services.SchedulerService
}

func NewSchedulerController(s services.SchedulerService) *SchedulerController {
	return &SchedulerController{service: s}
}

func (c *SchedulerController) getMiddlewareSchedulerStatus() (bool, string, bool) {
	middlewareURL := os.Getenv("MIDDLEWARE_API_URL")
	if middlewareURL == "" {
		middlewareURL = "http://localhost:8085/api/v1"
	}
	resp, err := http.Get(middlewareURL + "/scheduler/status")
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var res struct {
			Status          string `json:"status"`
			AutoPullEnabled bool   `json:"auto_pull_enabled"`
			EnvSetting      string `json:"env_setting"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil {
			envVal := res.EnvSetting
			if envVal == "" {
				envVal = "true (default)"
			}
			isEnvHardOff := (envVal == "false" || envVal == "0" || envVal == "off" || envVal == "no")
			return res.AutoPullEnabled, envVal, isEnvHardOff
		}
	}
	// Fallback ke ENV lokal jika API belum nyala
	envVal := os.Getenv("AUTO_PULL_ENABLED")
	if envVal == "" {
		envVal = "true (default)"
	}
	isEnvHardOff := (envVal == "false" || envVal == "0" || envVal == "off" || envVal == "no")
	return !isEnvHardOff, envVal, isEnvHardOff
}

func (c *SchedulerController) ShowSchedulerConfig(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	schedConfig, _ := c.service.GetConfig()
	effectiveActive, envSetting, isEnvHardOff := c.getMiddlewareSchedulerStatus()

	ctx.HTML(http.StatusOK, "scheduler_config.html", gin.H{
		"SchedConfig":     schedConfig,
		"EffectiveActive": effectiveActive,
		"EnvSetting":      envSetting,
		"IsEnvHardOff":    isEnvHardOff,
		"Username":        session.Get("username"),
		"ActiveMenu":      "scheduler",
	})
}

func (c *SchedulerController) UpdateSchedulerConfig(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	form := map[string]interface{}{
		"is_active":            ctx.PostForm("is_active"),
		"frequency":            ctx.PostForm("frequency"),
		"start_date":           ctx.PostForm("start_date"),
		"start_time":           ctx.PostForm("start_time"),
		"recur_every":          ctx.PostForm("recur_every"),
		"weekly_days":          ctx.PostFormArray("weekly_days[]"),
		"monthly_months":       ctx.PostFormArray("monthly_months[]"),
		"monthly_days":         ctx.PostFormArray("monthly_days[]"),
		"is_repeat_enabled":    ctx.PostForm("is_repeat_enabled"),
		"repeat_interval_mins": ctx.PostForm("repeat_interval_mins"),
		"batch_limit":          ctx.PostForm("batch_limit"),
	}

	schedConfig, _ := c.service.UpdateConfigFromForm(form)

	// Sync perubahan ke middleware API jika ada
	middlewareURL := os.Getenv("MIDDLEWARE_API_URL")
	if middlewareURL == "" {
		middlewareURL = "http://localhost:8085/api/v1"
	}
	toggleURL := fmt.Sprintf("%s/scheduler/toggle?active=%v", middlewareURL, schedConfig.IsActive)
	_, _ = http.Post(toggleURL, "application/json", nil)

	effectiveActive, envSetting, isEnvHardOff := c.getMiddlewareSchedulerStatus()

	ctx.HTML(http.StatusOK, "scheduler_config.html", gin.H{
		"Success":         "Pengaturan Auto Get Data from Maximo berhasil disimpan!",
		"SchedConfig":     schedConfig,
		"EffectiveActive": effectiveActive,
		"EnvSetting":      envSetting,
		"IsEnvHardOff":    isEnvHardOff,
		"Username":        session.Get("username"),
		"ActiveMenu":      "scheduler",
	})
}

// QuickToggleAutoPull mengubah status Auto-Pull secara instan melalui AJAX dari Panel
func (c *SchedulerController) QuickToggleAutoPull(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var req struct {
		Active bool `json:"active"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		activeStr := ctx.DefaultQuery("active", "true")
		req.Active = (activeStr == "true" || activeStr == "1" || activeStr == "on")
	}

	schedConfig, err := c.service.GetConfig()
	if err != nil {
		schedConfig = &models.SchedulerConfig{}
	}
	schedConfig.IsActive = req.Active
	schedConfig.PullIsActive = req.Active
	_, _ = c.service.UpdateConfigFromForm(map[string]interface{}{
		"is_active": func() string {
			if req.Active {
				return "on"
			}
			return ""
		}(),
		"frequency":            schedConfig.Frequency,
		"start_date":           schedConfig.StartDate,
		"start_time":           schedConfig.StartTime,
		"recur_every":          strconv.Itoa(schedConfig.RecurEvery),
		"is_repeat_enabled":    func() string { if schedConfig.IsRepeatEnabled { return "on" }; return "" }(),
		"repeat_interval_mins": strconv.Itoa(schedConfig.RepeatIntervalMins),
		"batch_limit":          strconv.Itoa(schedConfig.BatchLimit),
	})

	// Teruskan perubahan ke Middleware Engine API jika terhubung
	middlewareURL := os.Getenv("MIDDLEWARE_API_URL")
	if middlewareURL == "" {
		middlewareURL = "http://localhost:8085/api/v1"
	}
	toggleURL := fmt.Sprintf("%s/scheduler/toggle?active=%v", middlewareURL, req.Active)
	_, _ = http.Post(toggleURL, "application/json", nil)

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"active":  req.Active,
		"message": fmt.Sprintf("Auto Get Data from Maximo berhasil diatur ke: %v", req.Active),
	})
}

// ExecutePullNow memicu eksekusi manual penarikan data SQL Server Maximo ke PostgreSQL
func (c *SchedulerController) ExecutePullNow(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized. Silakan login kembali."})
		return
	}

	limitStr := ctx.DefaultQuery("limit", "1")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 1
	}

	middlewareURL := os.Getenv("MIDDLEWARE_API_URL")
	if middlewareURL == "" {
		middlewareURL = "http://localhost:8085/api/v1"
	}
	endpoint := fmt.Sprintf("%s/trigger/pull?limit=%d", middlewareURL, limit)

	resp, err := http.Post(endpoint, "application/json", nil)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": fmt.Sprintf("Gagal menghubungi Middleware Engine di %s: %v", endpoint, err),
		})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	_ = json.Unmarshal(bodyBytes, &result)

	ctx.JSON(resp.StatusCode, gin.H{
		"status":  "success",
		"data":    result,
		"message": fmt.Sprintf("Trigger pull berhasil dieksekusi (Limit: %d data).", limit),
	})
}
