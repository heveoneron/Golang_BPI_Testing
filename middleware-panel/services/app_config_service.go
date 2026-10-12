package services

import (
	"middleware-panel/models"
	"middleware-panel/repository"
	"sync"
)

type AppConfigService interface {
	GetAllConfigs() ([]models.Config, error)
	GetValue(key string) string
	SaveConfig(req models.Config) error
	DeleteConfig(key string) error
	ReloadCache() error
}

type appConfigService struct {
	repo  repository.AppConfigRepository
	cache map[string]string
	mutex sync.RWMutex
}

func NewAppConfigService(repo repository.AppConfigRepository) AppConfigService {
	service := &appConfigService{
		repo:  repo,
		cache: make(map[string]string),
	}
	// Load data dari DB ke memori saat service pertama kali dibuat
	service.ReloadCache()
	return service
}

// ReloadCache mengambil semua data dari DB dan memasukkannya ke map
func (s *appConfigService) ReloadCache() error {
	configs, err := s.repo.GetAll()
	if err != nil {
		return err
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.cache = make(map[string]string)
	for _, c := range configs {
		s.cache[c.Key] = c.Value
	}
	return nil
}

// GetValue mengambil nilai dengan sangat cepat langsung dari RAM (pengganti os.Getenv)
func (s *appConfigService) GetValue(key string) string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.cache[key]
}

func (s *appConfigService) GetAllConfigs() ([]models.Config, error) {
	return s.repo.GetAll()
}

func (s *appConfigService) SaveConfig(req models.Config) error {
	err := s.repo.Save(&req)
	if err == nil {
		// Jika berhasil simpan ke DB, update juga cache memorinya
		s.mutex.Lock()
		s.cache[req.Key] = req.Value
		s.mutex.Unlock()
	}
	return err
}

func (s *appConfigService) DeleteConfig(key string) error {
	err := s.repo.Delete(key)
	if err == nil {
		// Hapus juga dari cache memori
		s.mutex.Lock()
		delete(s.cache, key)
		s.mutex.Unlock()
	}
	return err
}