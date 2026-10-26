package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"
)

type MiddlewareLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type MiddlewareLoginResponse struct {
	Status       string `json:"status"`
	Token        string `json:"token"`
	Expiry       int64  `json:"expiry"`
	Message      string `json:"message"`
	Role         string `json:"role"`
	AllowedMenus string `json:"allowed_menus"`
}

type AuthRepository interface {
	CheckLoginViaAPI(username, password string) (*MiddlewareLoginResponse, error)
}

type authRepo struct {
	userRepo UserRepository
}

func NewAuthRepository(userRepos ...UserRepository) AuthRepository {
	var uRepo UserRepository
	if len(userRepos) > 0 {
		uRepo = userRepos[0]
	}
	return &authRepo{userRepo: uRepo}
}

func (r *authRepo) CheckLoginViaAPI(username, password string) (*MiddlewareLoginResponse, error) {
	// 1. Cek autentikasi database User Access jika repository tersedia
	if r.userRepo != nil {
		user, err := r.userRepo.GetByUsername(username)
		if err == nil && user != nil {
			if !user.IsActive {
				return nil, errors.New("Akun Anda telah dinonaktifkan. Silakan hubungi administrator.")
			}
			if r.userRepo.VerifyPassword(user, password) {
				_ = r.userRepo.UpdateLastLogin(user.ID)
				allowed := user.AllowedMenus
				if allowed == "" {
					if user.Role == "admin" {
						allowed = "*"
					} else {
						allowed = "dashboard"
					}
				}
				return &MiddlewareLoginResponse{
					Status:       "success",
					Token:        "bpi-panel-db-token-" + time.Now().Format("20060102150405"),
					Expiry:       time.Now().Add(24 * time.Hour).Unix(),
					Message:      "Login berhasil sebagai " + user.Role + ".",
					Role:         user.Role,
					AllowedMenus: allowed,
				}, nil
			}
			return nil, errors.New("Username atau password salah.")
		}
	}

	// 2. Mengambil URL dari file .env jika ingin autentikasi via API eksternal
	apiURL := os.Getenv("LOGIN_API_URL")

	// 3. Fallback kredensial lokal admin dari .env (default: admin / admin)
	if apiURL == "" {
		adminUser := os.Getenv("PANEL_ADMIN_USER")
		if adminUser == "" {
			adminUser = os.Getenv("BACKUP_API_USER")
		}
		if adminUser == "" {
			adminUser = "admin"
		}

		adminPass := os.Getenv("PANEL_ADMIN_PASS")
		if adminPass == "" {
			adminPass = os.Getenv("BACKUP_API_PASS")
		}
		if adminPass == "" {
			adminPass = "admin"
		}

		// Validasi kredensial admin lokal (mendukung adminPass, "admin", atau "admin123")
		if username == adminUser && (password == adminPass || password == "admin" || password == "admin123") {
			return &MiddlewareLoginResponse{
				Status:       "success",
				Token:        "bpi-panel-local-token-" + time.Now().Format("20060102150405"),
				Expiry:       time.Now().Add(24 * time.Hour).Unix(),
				Message:      "Login berhasil sebagai administrator.",
				Role:         "admin",
				AllowedMenus: "*",
			}, nil
		}

		return nil, errors.New("Username atau password salah. (Kredensial default: admin / admin)")
	}

	payload := MiddlewareLoginRequest{Username: username, Password: password}
	jsonPayload, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, errors.New("Failed to establish a connection to the server")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("Unable to reach the API serve")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Invalid request or invalid credentials")
	}

	var apiResp MiddlewareLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, errors.New("Failed to read the server response")
	}

	if apiResp.Status != "success" || apiResp.Token == "" {
		return nil, errors.New("Login failed: Invalid token")
	}

	return &apiResp, nil
}
