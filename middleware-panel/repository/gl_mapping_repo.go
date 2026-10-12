package repository

import (
	"middleware-panel/models"

	"gorm.io/gorm"
)

type GLMappingRepository interface {
	GetAll() ([]models.MaterialGLMapping, error)
	GetByID(id uint) (models.MaterialGLMapping, error)
	Create(mapping *models.MaterialGLMapping) error
	Update(mapping *models.MaterialGLMapping) error
	Delete(id uint) error
}

type glMappingRepo struct {
	db *gorm.DB
}

func NewGLMappingRepository(db *gorm.DB) GLMappingRepository {
	return &glMappingRepo{db}
}

func (r *glMappingRepo) GetAll() ([]models.MaterialGLMapping, error) {
	var mappings []models.MaterialGLMapping
	err := r.db.Order("id asc").Find(&mappings).Error
	return mappings, err
}

func (r *glMappingRepo) GetByID(id uint) (models.MaterialGLMapping, error) {
	var mapping models.MaterialGLMapping
	err := r.db.First(&mapping, id).Error
	return mapping, err
}

func (r *glMappingRepo) Create(mapping *models.MaterialGLMapping) error {
	return r.db.Create(mapping).Error
}

func (r *glMappingRepo) Update(mapping *models.MaterialGLMapping) error {
	return r.db.Save(mapping).Error
}

func (r *glMappingRepo) Delete(id uint) error {
	return r.db.Delete(&models.MaterialGLMapping{}, id).Error
}
