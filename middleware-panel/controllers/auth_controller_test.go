package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"middleware-panel/repository"
	"middleware-panel/services"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupAuthRouter() *gin.Engine {
	_ = os.Setenv("LOGIN_API_URL", "")
	_ = os.Setenv("PANEL_ADMIN_USER", "admin")
	_ = os.Setenv("PANEL_ADMIN_PASS", "admin")

	r := gin.New()
	store := cookie.NewStore([]byte("secret_key_test"))
	r.Use(sessions.Sessions("test_session", store))

	authRepo := repository.NewAuthRepository()
	authService := services.NewAuthService(authRepo)
	authController := NewAuthController(authService)

	r.POST("/login", authController.ProcessLogin)
	return r
}

func TestAuthController_ProcessLogin_Success(t *testing.T) {
	r := setupAuthRouter()

	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "admin",
	})

	req, _ := http.NewRequest("POST", "/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var res map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["status"] != "success" {
		t.Errorf("expected status 'success', got %v", res["status"])
	}
}

func TestAuthController_ProcessLogin_InvalidCredentials(t *testing.T) {
	r := setupAuthRouter()

	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "wrongpassword",
	})

	req, _ := http.NewRequest("POST", "/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
