package models

import "time"

type SAPEndpoint struct {
	ID           int        `gorm:"primaryKey;column:id" json:"id"`
	ServiceCode  string     `gorm:"column:service_code;type:varchar(50);unique;not null" json:"service_code"`
	ServiceName  string     `gorm:"column:service_name;type:varchar(100);not null" json:"service_name"`
	ScenarioID   string     `gorm:"column:scenario_id;type:varchar(50);not null" json:"scenario_id"`
	ODataVersion string     `gorm:"column:odata_version;type:varchar(10);not null" json:"odata_version"`
	BaseURL      string     `gorm:"column:base_url;type:text;not null" json:"base_url"`
	EndpointPath string     `gorm:"column:endpoint_path;type:text;not null" json:"endpoint_path"`
	CSRFPath     string     `gorm:"column:csrf_path;type:text" json:"csrf_path"`
	Username     string     `gorm:"column:username;type:varchar(100);not null" json:"username"`
	Password     string     `gorm:"column:password;type:text;not null" json:"password"`
	IsActive     bool       `gorm:"column:is_active;default:true" json:"is_active"`
	LastStatus   string     `gorm:"column:last_status;type:varchar(30)" json:"last_status"`
	LastResponse string     `gorm:"column:last_response;type:text" json:"last_response"`
	LastTestedAt *time.Time `gorm:"column:last_tested_at" json:"last_tested_at"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (SAPEndpoint) TableName() string {
	return "sap_endpoints"
}
