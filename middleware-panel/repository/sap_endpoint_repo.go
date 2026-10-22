package repository

import (
	"middleware-panel/models"
	"time"

	"gorm.io/gorm"
)

type SAPEndpointRepository interface {
	GetAll() ([]models.SAPEndpoint, error)
	GetByCode(code string) (models.SAPEndpoint, error)
	Save(ep *models.SAPEndpoint) error
	UpdateStatus(code, status, respMsg string) error
}

type sapEndpointRepo struct {
	db *gorm.DB
}

func NewSAPEndpointRepository(db *gorm.DB) SAPEndpointRepository {
	return &sapEndpointRepo{db: db}
}

func (r *sapEndpointRepo) GetAll() ([]models.SAPEndpoint, error) {
	var endpoints []models.SAPEndpoint
	err := r.db.Order("id asc").Find(&endpoints).Error
	return endpoints, err
}

func (r *sapEndpointRepo) GetByCode(code string) (models.SAPEndpoint, error) {
	var ep models.SAPEndpoint
	err := r.db.Where("UPPER(service_code) = UPPER(?)", code).First(&ep).Error
	return ep, err
}

func (r *sapEndpointRepo) Save(ep *models.SAPEndpoint) error {
	var existing models.SAPEndpoint
	err := r.db.Where("UPPER(service_code) = UPPER(?)", ep.ServiceCode).First(&existing).Error
	if err == nil {
		// Update existing
		updates := map[string]interface{}{
			"service_name":  ep.ServiceName,
			"scenario_id":   ep.ScenarioID,
			"odata_version": ep.ODataVersion,
			"base_url":      ep.BaseURL,
			"endpoint_path": ep.EndpointPath,
			"csrf_path":     ep.CSRFPath,
			"username":      ep.Username,
			"is_active":     ep.IsActive,
			"updated_at":    time.Now(),
		}
		if ep.Password != "" {
			updates["password"] = ep.Password
		}
		return r.db.Model(&existing).Updates(updates).Error
	}
	return r.db.Create(ep).Error
}

func (r *sapEndpointRepo) UpdateStatus(code, status, respMsg string) error {
	now := time.Now()
	return r.db.Model(&models.SAPEndpoint{}).
		Where("UPPER(service_code) = UPPER(?)", code).
		Updates(map[string]interface{}{
			"last_status":    status,
			"last_response":  respMsg,
			"last_tested_at": &now,
			"updated_at":     now,
		}).Error
}
