package repository

import (
	"middleware-panel/models"

	"gorm.io/gorm"
)

type SchedulerRepository interface {
	GetFirstConfig() (*models.SchedulerConfig, error)
	UpdateConfig(config *models.SchedulerConfig) error
}

type schedulerRepo struct {
	db *gorm.DB
}

func NewSchedulerRepository(db *gorm.DB) SchedulerRepository {
	return &schedulerRepo{db}
}

func (r *schedulerRepo) GetFirstConfig() (*models.SchedulerConfig, error) {
	var config models.SchedulerConfig
	err := r.db.First(&config).Error
	return &config, err
}

func (r *schedulerRepo) UpdateConfig(config *models.SchedulerConfig) error {
	return r.db.Save(config).Error
}