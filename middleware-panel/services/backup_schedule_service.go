package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"middleware-panel/models"
	"middleware-panel/repository"
	"net/http"
	"os"
	"time"
)

type BackupScheduleService interface {
	FetchAllSchedules() ([]models.BackupSchedule, error)
	SaveSchedule(schedule *models.BackupSchedule) error
	ExecuteBackupNow(id int) error
}

type backupScheduleService struct {
	repo repository.BackupScheduleRepository
}

func NewBackupScheduleService(repo repository.BackupScheduleRepository) BackupScheduleService {
	return &backupScheduleService{repo}
}

func (s *backupScheduleService) FetchAllSchedules() ([]models.BackupSchedule, error) {
	return s.repo.GetAll()
}

func (s *backupScheduleService) SaveSchedule(schedule *models.BackupSchedule) error {
	return s.repo.Update(schedule)
}

// Fungsi Helper Baru: Mengambil Bearer Token dari API Backend
func (s *backupScheduleService) getBearerToken(apiBaseURL string) (string, error) {
	// Catatan: Pastikan endpoint ini sesuai dengan path asli di Swagger Anda.
	// (Misal: /admin/login atau /api/login)
	loginEndpoint := fmt.Sprintf("%s/admin/login", apiBaseURL)

	// Ambil dari .env (Sangat disarankan) atau gunakan fallback dari pengujian Swagger
	username := os.Getenv("BACKUP_API_USER")
	password := os.Getenv("BACKUP_API_PASS")
	if username == "" {
		username = "admin" // Sesuaikan dengan data uji Swagger Anda
		password = "admin123"
	}

	payload := map[string]string{
		"username": username,
		"password": password,
	}
	jsonPayload, _ := json.Marshal(payload)

	resp, err := http.Post(loginEndpoint, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gagal login ke API backend, status: %d", resp.StatusCode)
	}

	// Ekstrak properti "token" dari respons JSON
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Token, nil
}

func (s *backupScheduleService) ExecuteBackupNow(id int) error {
	config, err := s.repo.GetByID(id)
	if err != nil {
		return errors.New("konfigurasi backup tidak ditemukan")
	}

	apiBaseURL := os.Getenv("BACKUP_API_URL")
	if apiBaseURL == "" {
		return errors.New("variabel BACKUP_API_URL belum dikonfigurasi di .env")
	}

	// 1. Ambil Bearer Token terlebih dahulu
	token, err := s.getBearerToken(apiBaseURL)
	if err != nil {
		fmt.Printf("--> [BACKUP ENGINE] Gagal Autentikasi: %v\n", err)
		return errors.New("gagal mendapatkan otorisasi dari backend API")
	}

	// 2. Persiapan Klien HTTP untuk Backup
	endpoint := fmt.Sprintf("%s/admin/backup/run", apiBaseURL)
	client := &http.Client{Timeout: 15 * time.Minute}
	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}

	// 3. INJEKSI BEARER TOKEN KE DALAM HEADER
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	// 4. Eksekusi API
	fmt.Printf("--> [BACKUP ENGINE] Triggering API: POST %s\n", endpoint)
	resp, err := client.Do(req)

	timestamp := time.Now()
	status := "SUCCESS"

	// 5. Validasi Respons
	if err != nil {
		status = "FAILED"
		fmt.Printf("--> [BACKUP ENGINE] Gagal mengeksekusi API: %v\n", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			status = "FAILED"
			fmt.Printf("--> [BACKUP ENGINE] API mengembalikan status gagal: %d\n", resp.StatusCode)
		} else {
			fmt.Println("--> [BACKUP ENGINE] Sukses! API pencadangan merespons OK.")
		}
	}

	// 6. Perbarui Status di Database
	config.LastBackupTime = &timestamp
	config.LastBackupStatus = &status
	s.repo.Update(config)

	if status == "FAILED" {
		return errors.New("proses eksekusi via API gagal, periksa log backend untuk detail")
	}

	return nil
}
