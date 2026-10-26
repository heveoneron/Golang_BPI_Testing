package controllers

import (
	"middleware-panel/services"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type AuthController struct {
	service services.AuthService
}

func NewAuthController(s services.AuthService) *AuthController {
	return &AuthController{service: s}
}

func (c *AuthController) ShowLogin(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "login.html", nil)
}

func (c *AuthController) ProcessLogin(ctx *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Format request tidak valid."})
		return
	}

	if req.Username == "" || req.Password == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Username dan Password wajib diisi."})
		return
	}

	apiResp, err := c.service.Authenticate(req.Username, req.Password)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": err.Error()})
		return
	}

	session := sessions.Default(ctx)
	session.Set("user_id", req.Username)
	session.Set("username", req.Username)
	session.Set("token", apiResp.Token)
	session.Set("role", apiResp.Role)
	session.Set("allowed_menus", apiResp.AllowedMenus)
	session.Save()

	ctx.JSON(http.StatusOK, gin.H{
		"status":        "success",
		"message":       "Login successful. Preparing your workspace...",
		"role":          apiResp.Role,
		"allowed_menus": apiResp.AllowedMenus,
	})
}

func (c *AuthController) ProcessLogout(ctx *gin.Context) {
	session := sessions.Default(ctx)
	session.Clear()
	session.Save()
	ctx.Redirect(http.StatusFound, "/login")
}
