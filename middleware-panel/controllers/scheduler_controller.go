package controllers

import (
	"middleware-panel/services"
	"net/http"

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
	}

	schedConfig, _ := c.service.UpdateConfigFromForm(form)

	ctx.HTML(http.StatusOK, "scheduler_config.html", gin.H{
		"Success":     "Automation schedule has been updated successfully!",
		"SchedConfig": schedConfig,
		"Username":    session.Get("username"),
		"ActiveMenu":  "scheduler",
	})
}
