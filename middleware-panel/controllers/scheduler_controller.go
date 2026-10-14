package controllers

import (
	"encoding/json"
	"fmt"
	"io"
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

func (c *SchedulerController) ShowSchedulerConfig(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	schedConfig, _ := c.service.GetConfig()
	ctx.HTML(http.StatusOK, "scheduler_config.html", gin.H{
		"SchedConfig": schedConfig,
		"Username":    session.Get("username"),
		"ActiveMenu":  "scheduler",
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

	ctx.HTML(http.StatusOK, "scheduler_config.html", gin.H{
		"Success":     "Automation schedule has been updated successfully!",
		"SchedConfig": schedConfig,
		"Username":    session.Get("username"),
		"ActiveMenu":  "scheduler",
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
