package controllers

import (
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type UserController struct {
	svc services.UserService
}

func NewUserController(svc services.UserService) *UserController {
	return &UserController{svc: svc}
}

func (c *UserController) ShowUsers(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	users, err := c.svc.GetUsers()
	if err != nil {
		users = []models.User{}
	}

	username := "Admin"
	if u := session.Get("username"); u != nil {
		username = u.(string)
	}

	activeCount := 0
	adminCount := 0
	operatorCount := 0
	for _, u := range users {
		if u.IsActive {
			activeCount++
		}
		if u.Role == "admin" {
			adminCount++
		} else if u.Role == "operator" {
			operatorCount++
		}
	}

	ctx.HTML(http.StatusOK, "users.html", gin.H{
		"Title":         "User Access Management",
		"Username":      username,
		"Users":         users,
		"TotalUsers":    len(users),
		"ActiveCount":   activeCount,
		"AdminCount":    adminCount,
		"OperatorCount": operatorCount,
		"ActiveMenu":    "users",
	})
}

func (c *UserController) CreateUser(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var req struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		FullName     string `json:"full_name"`
		Email        string `json:"email"`
		Role         string `json:"role"`
		AllowedMenus string `json:"allowed_menus"`
		IsActive     bool   `json:"is_active"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		req.Username = ctx.PostForm("username")
		req.Password = ctx.PostForm("password")
		req.FullName = ctx.PostForm("full_name")
		req.Email = ctx.PostForm("email")
		req.Role = ctx.PostForm("role")
		req.AllowedMenus = ctx.PostForm("allowed_menus")
		req.IsActive = (ctx.PostForm("is_active") == "true" || ctx.PostForm("is_active") == "on" || ctx.PostForm("is_active") == "1")
	}

	user := models.User{
		Username:     req.Username,
		Password:     req.Password,
		FullName:     req.FullName,
		Email:        req.Email,
		Role:         req.Role,
		AllowedMenus: req.AllowedMenus,
		IsActive:     req.IsActive,
	}

	if err := c.svc.CreateUser(&user); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Pengguna baru [" + user.Username + "] berhasil ditambahkan!",
	})
}

func (c *UserController) UpdateUser(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var req struct {
		ID           uint   `json:"id"`
		FullName     string `json:"full_name"`
		Email        string `json:"email"`
		Role         string `json:"role"`
		AllowedMenus string `json:"allowed_menus"`
		Password     string `json:"password"`
		IsActive     bool   `json:"is_active"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		idVal, _ := strconv.Atoi(ctx.PostForm("id"))
		req.ID = uint(idVal)
		req.FullName = ctx.PostForm("full_name")
		req.Email = ctx.PostForm("email")
		req.Role = ctx.PostForm("role")
		req.AllowedMenus = ctx.PostForm("allowed_menus")
		req.Password = ctx.PostForm("password")
		req.IsActive = (ctx.PostForm("is_active") == "true" || ctx.PostForm("is_active") == "on" || ctx.PostForm("is_active") == "1")
	}

	if req.ID <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ID Pengguna tidak valid"})
		return
	}

	user := models.User{
		FullName:     req.FullName,
		Email:        req.Email,
		Role:         req.Role,
		AllowedMenus: req.AllowedMenus,
		Password:     req.Password,
		IsActive:     req.IsActive,
	}
	user.ID = req.ID

	updatePassword := req.Password != ""
	if err := c.svc.UpdateUser(&user, updatePassword); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Data pengguna berhasil diperbarui!",
	})
}

func (c *UserController) ToggleActive(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ID tidak valid"})
		return
	}

	if err := c.svc.ToggleActive(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Status aktif pengguna berhasil diubah",
	})
}

func (c *UserController) DeleteUser(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	currentUsername := "admin"
	if u := session.Get("username"); u != nil {
		currentUsername = u.(string)
	}

	idStr := ctx.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ID tidak valid"})
		return
	}

	if err := c.svc.DeleteUser(uint(id), currentUsername); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Pengguna berhasil dihapus",
	})
}
