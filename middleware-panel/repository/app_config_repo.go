package repository

import (
	"middleware-panel/models"

	"gorm.io/gorm"
)

type AppConfigRepository interface {
	GetAll() ([]models.Config, error)
	GetByKey(key string) (models.Config, error)
	Save(config *models.Config) error
	Delete(key string) error
}

type appConfigRepo struct {
	db *gorm.DB
}

func NewAppConfigRepository(db *gorm.DB) AppConfigRepository {
	return &appConfigRepo{db}
}

func (r *appConfigRepo) GetAll() ([]models.Config, error) {
	var configs []models.Config
	targetKeys := []string{
		"bot_base_url",
		"bpa_base_url",
		"doc_type_bot_mapping",
		"prefix_bot_mapping",
		"sap_username",
		"sap_password",
		"sap_base_url",
		"erp_callback_url",
	}
	err := r.db.Where("key IN ?", targetKeys).Order("key asc").Find(&configs).Error
	return configs, err
}

func (r *appConfigRepo) GetByKey(key string) (models.Config, error) {
	var config models.Config
	err := r.db.Where("key = ?", key).First(&config).Error
	return config, err
}

// Save akan melakukan Insert jika key belum ada, atau Update jika key sudah ada
func (r *appConfigRepo) Save(config *models.Config) error {
	return r.db.Save(config).Error
}

func (r *appConfigRepo) Delete(key string) error {
	return r.db.Where("key = ?", key).Delete(&models.Config{}).Error
}
