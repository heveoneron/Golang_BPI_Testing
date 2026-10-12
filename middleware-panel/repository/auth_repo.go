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
	Status  string `json:"status"`
	Token   string `json:"token"`
	Expiry  int64  `json:"expiry"`
	Message string `json:"message"`
}

type AuthRepository interface {
	CheckLoginViaAPI(username, password string) (*MiddlewareLoginResponse, error)
}

type authRepo struct{}

func NewAuthRepository() AuthRepository {
	return &authRepo{}
}

func (r *authRepo) CheckLoginViaAPI(username, password string) (*MiddlewareLoginResponse, error) {
	payload := MiddlewareLoginRequest{Username: username, Password: password}
	jsonPayload, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 10 * time.Second}

	// --- PERUBAHAN DI SINI ---
	// Mengambil URL dari file .env
	apiURL := os.Getenv("LOGIN_API_URL")

	// Validasi jika variabel .env lupa diisi
	if apiURL == "" {
		return nil, errors.New("Konfigurasi LOGIN_API_URL tidak ditemukan di file .env")
	}

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
