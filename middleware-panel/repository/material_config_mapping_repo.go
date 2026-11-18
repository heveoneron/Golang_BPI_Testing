package repository

import (
	"middleware-panel/models"
	"strings"

	"gorm.io/gorm"
)

type MaterialConfigMappingRepository interface {
	GetAll(configType string) ([]models.MaterialConfigMapping, error)
	GetByID(id uint) (*models.MaterialConfigMapping, error)
	Save(m *models.MaterialConfigMapping) error
	ToggleActive(id uint) error
	Delete(id uint) error
}

type materialConfigMappingRepo struct {
	db *gorm.DB
}

func NewMaterialConfigMappingRepository(db *gorm.DB) MaterialConfigMappingRepository {
	if db != nil {
		// Pastikan kolom baru otomatis terbuat jika database PostgreSQL belum termigrasi
		_ = db.AutoMigrate(&models.MaterialConfigMapping{})
		_ = db.Exec("ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS inventory_posting_gl VARCHAR(50)").Error
		_ = db.Exec("ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS goods_issue_gl VARCHAR(50)").Error
		_ = db.Exec("ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS stock_adj_gl VARCHAR(50)").Error
		_ = db.Exec("ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS cost_center VARCHAR(50)").Error

		// Seed otomatis 7 Valuation Class (DJ-MM-ENH-ZME003) jika belum terisi di material_config_mappings
		type valSeed struct {
			SAPVal, InvGL, GIGL, AdjGL, CC string
		}
		seeds := []valSeed{
			{"S001", "114001", "512033", "512033", "71098"},
			{"S002", "114004", "512030", "512030", "71099"},
			{"S003", "114009", "512031", "512031", "71099"},
			{"S004", "114012", "512032", "512032", "71099"},
			{"S006", "114006", "512034", "512034", "71099"},
			{"S009", "114017", "512028", "512028", "71099"},
			{"S011", "114016", "512023", "512023", "71099"},
		}
		for _, s := range seeds {
			_ = db.Exec(`UPDATE material_config_mappings 
				SET inventory_posting_gl = ?, goods_issue_gl = ?, stock_adj_gl = ?, cost_center = ? 
				WHERE UPPER(sap_value) = ? AND (inventory_posting_gl IS NULL OR inventory_posting_gl = '')`,
				s.InvGL, s.GIGL, s.AdjGL, s.CC, s.SAPVal).Error
		}
	}
	return &materialConfigMappingRepo{db: db}
}

func (r *materialConfigMappingRepo) GetAll(configType string) ([]models.MaterialConfigMapping, error) {
	var list []models.MaterialConfigMapping
	q := r.db.Order("config_type asc, maximo_value asc")
	if configType != "" {
		q = q.Where("config_type = ?", configType)
	}
	err := q.Find(&list).Error
	return list, err
}

func (r *materialConfigMappingRepo) GetByID(id uint) (*models.MaterialConfigMapping, error) {
	var item models.MaterialConfigMapping
	err := r.db.First(&item, id).Error
	return &item, err
}

func (r *materialConfigMappingRepo) Save(m *models.MaterialConfigMapping) error {
	m.ConfigType = strings.ToUpper(strings.TrimSpace(m.ConfigType))
	m.MaximoValue = strings.TrimSpace(m.MaximoValue)
	m.SAPValue = strings.TrimSpace(m.SAPValue)

	doSave := func() error {
		if m.ID > 0 {
			return r.db.Save(m).Error
		}

		var existing models.MaterialConfigMapping
		err := r.db.Where("config_type = ? AND UPPER(maximo_value) = UPPER(?)", m.ConfigType, m.MaximoValue).First(&existing).Error
		if err == nil {
			m.ID = existing.ID
			return r.db.Model(&existing).Updates(m).Error
		}

		return r.db.Create(m).Error
	}

	err := doSave()
	if err != nil && (strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "42703")) {
		_ = r.db.AutoMigrate(&models.MaterialConfigMapping{})
		_ = r.db.Exec("ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS inventory_posting_gl VARCHAR(50)").Error
		_ = r.db.Exec("ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS goods_issue_gl VARCHAR(50)").Error
		_ = r.db.Exec("ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS stock_adj_gl VARCHAR(50)").Error
		_ = r.db.Exec("ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS cost_center VARCHAR(50)").Error
		return doSave()
	}
	return err
}

func (r *materialConfigMappingRepo) ToggleActive(id uint) error {
	var item models.MaterialConfigMapping
	if err := r.db.First(&item, id).Error; err != nil {
		return err
	}
	item.IsActive = !item.IsActive
	return r.db.Model(&item).Update("is_active", item.IsActive).Error
}

func (r *materialConfigMappingRepo) Delete(id uint) error {
	return r.db.Delete(&models.MaterialConfigMapping{}, id).Error
}

