package repository

import (
	"middleware-panel/models"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
)

type SAPEndpointRepository interface {
	GetAll() ([]models.SAPEndpoint, error)
	GetByCode(code string) (models.SAPEndpoint, error)
	GetGlobalBaseURL() (string, error)
	UpdateGlobalBaseURL(baseURL string) (int64, error)
	Save(ep *models.SAPEndpoint) error
	UpdateStatus(code, status, respMsg string) error
	Delete(code string) error
	SeedDefaults() error
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
	err := r.db.Where("UPPER(service_code) = UPPER(?)", strings.TrimSpace(code)).First(&ep).Error
	return ep, err
}

func (r *sapEndpointRepo) GetGlobalBaseURL() (string, error) {
	var cfg models.Config
	if err := r.db.Table("configs").Where("key = ?", "sap_base_url").First(&cfg).Error; err == nil && strings.TrimSpace(cfg.Value) != "" {
		return strings.TrimRight(strings.TrimSpace(cfg.Value), "/"), nil
	}
	var ep models.SAPEndpoint
	if err := r.db.Where("base_url != '' AND base_url IS NOT NULL").First(&ep).Error; err == nil && strings.TrimSpace(ep.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(ep.BaseURL), "/"), nil
	}
	if v := strings.TrimSpace(os.Getenv("SAP_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/"), nil
	}
	return "https://my445160-api.s4hana.cloud.sap", nil
}

func (r *sapEndpointRepo) UpdateGlobalBaseURL(baseURL string) (int64, error) {
	clean := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if clean == "" {
		clean = "https://my445160-api.s4hana.cloud.sap"
	}

	// 1. Update seluruh baris di sap_endpoints langsung via SQL UPDATE agar pasti persisten
	res := r.db.Exec("UPDATE sap_endpoints SET base_url = ?, updated_at = NOW()", clean)

	// 2. Upsert ke tabel configs agar global base URL persisten dan sinkron
	var cfg models.Config
	if err := r.db.Table("configs").Where("key = ?", "sap_base_url").First(&cfg).Error; err != nil {
		_ = r.db.Table("configs").Create(&models.Config{
			Key:         "sap_base_url",
			Value:       clean,
			Description: "Host sentral SAP S/4HANA Grow Cloud untuk seluruh layanan OData",
		}).Error
	} else {
		_ = r.db.Table("configs").Where("key = ?", "sap_base_url").Update("value", clean).Error
	}

	return res.RowsAffected, res.Error
}

func (r *sapEndpointRepo) Save(ep *models.SAPEndpoint) error {
	cleanCode := strings.ToUpper(strings.TrimSpace(ep.ServiceCode))
	ep.ServiceCode = cleanCode
	if ep.BaseURL != "" {
		ep.BaseURL = strings.TrimRight(strings.TrimSpace(ep.BaseURL), "/")
	}

	var existing models.SAPEndpoint
	err := r.db.Where("UPPER(service_code) = ?", cleanCode).First(&existing).Error
	if err == nil {
		// Update existing endpoint tanpa menimpa password jika kosong
		updates := map[string]interface{}{
			"service_name":  ep.ServiceName,
			"scenario_id":   ep.ScenarioID,
			"odata_version": ep.ODataVersion,
			"base_url":      ep.BaseURL,
			"endpoint_path": ep.EndpointPath,
			"username":      ep.Username,
			"is_active":     ep.IsActive,
			"updated_at":    time.Now(),
		}
		if ep.CSRFPath != "" {
			updates["csrf_path"] = ep.CSRFPath
		}
		if ep.Password != "" {
			updates["password"] = ep.Password
		}
		return r.db.Model(&models.SAPEndpoint{}).Where("UPPER(service_code) = ?", cleanCode).Updates(updates).Error
	}

	// Create baru jika belum ada
	if ep.Password == "" {
		ep.Password = "PowerofOne@123456789"
	}
	ep.CreatedAt = time.Now()
	ep.UpdatedAt = time.Now()
	return r.db.Create(ep).Error
}

func (r *sapEndpointRepo) UpdateStatus(code, status, respMsg string) error {
	now := time.Now()
	return r.db.Model(&models.SAPEndpoint{}).
		Where("UPPER(service_code) = UPPER(?)", strings.TrimSpace(code)).
		Updates(map[string]interface{}{
			"last_status":    status,
			"last_response":  respMsg,
			"last_tested_at": &now,
			"updated_at":     now,
		}).Error
}

func (r *sapEndpointRepo) Delete(code string) error {
	return r.db.Where("UPPER(service_code) = UPPER(?)", strings.TrimSpace(code)).Delete(&models.SAPEndpoint{}).Error
}

func (r *sapEndpointRepo) SeedDefaults() error {
	defaultBaseURL := "https://my445160-api.s4hana.cloud.sap"
	if envURL := strings.TrimSpace(os.Getenv("SAP_BASE_URL")); envURL != "" {
		defaultBaseURL = strings.TrimRight(envURL, "/")
	}

	defaults := []models.SAPEndpoint{
		{
			ServiceCode:  "PO",
			ServiceName:  "Purchase Order Integration (OData v4)",
			ScenarioID:   "SAP_COM_0053",
			ODataVersion: "v4",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder",
			Username:     "CU_SAP_COM_0053",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "BP",
			ServiceName:  "Business Partner Integration (OData v2)",
			ScenarioID:   "SAP_COM_0008",
			ODataVersion: "v2",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata/sap/API_BUSINESS_PARTNER/A_BusinessPartner",
			Username:     "CU_SAP_COM_0008",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "PRODUCT",
			ServiceName:  "Product / Material Master Integration (OData v2)",
			ScenarioID:   "SAP_COM_0009",
			ODataVersion: "v2",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product",
			Username:     "CU_SAP_COM_0009",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "GRN",
			ServiceName:  "Material Document / Goods Movement (OData v2)",
			ScenarioID:   "SAP_COM_0108",
			ODataVersion: "v2",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader",
			Username:     "Y_BPI_CU_SAP_COM_0108",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "CANCEL_GR",
			ServiceName:  "Cancel Goods Movement / Material Document (OData v2)",
			ScenarioID:   "SAP_COM_0108",
			ODataVersion: "v2",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/Cancel",
			CSRFPath:     "/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader",
			Username:     "Y_BPI_CU_SAP_COM_0108",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "PO_CHANGE_HEADER",
			ServiceName:  "Purchase Order Change Header (PATCH OData v4)",
			ScenarioID:   "SAP_COM_0053",
			ODataVersion: "v4",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder",
			Username:     "CU_SAP_COM_0053",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "PO_CHANGE_ITEM",
			ServiceName:  "Purchase Order Change Item (PATCH OData v4)",
			ScenarioID:   "SAP_COM_0053",
			ODataVersion: "v4",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrderItem",
			Username:     "CU_SAP_COM_0053",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "GR_WITH_PO",
			ServiceName:  "Goods Receipt / Material Document with PO (OData v2)",
			ScenarioID:   "SAP_COM_0108",
			ODataVersion: "v2",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader",
			Username:     "Y_BPI_CU_SAP_COM_0108",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "GR_NON_PO",
			ServiceName:  "Goods Receipt Non PO (OData v2)",
			ScenarioID:   "SAP_COM_0108",
			ODataVersion: "v2",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader",
			Username:     "Y_BPI_CU_SAP_COM_0108",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "MATERIAL_DOC_DETAIL",
			ServiceName:  "Material Document Header & Item Query (OData v2)",
			ScenarioID:   "SAP_COM_0108",
			ODataVersion: "v2",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader",
			Username:     "Y_BPI_CU_SAP_COM_0108",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "GL_ACCOUNT_LINE_ITEMS",
			ServiceName:  "G/L Account Line Items Query (OData v2)",
			ScenarioID:   "SAP_COM_0303",
			ODataVersion: "v2",
			BaseURL:      defaultBaseURL,
			EndpointPath: "/sap/opu/odata/sap/API_GLACCOUNTLINEITEM/GLAccountLineItem",
			Username:     "Y_BPI_CU_SAP_COM_0303",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
		{
			ServiceCode:  "SUPPLIER_INVOICE",
			ServiceName:  "Supplier Invoice Reimbursement & Business Trip (OData v2)",
			ScenarioID:   "SAP_COM_0057",
			ODataVersion: "v2",
			BaseURL:      "https://my446666-api.s4hana.cloud.sap",
			EndpointPath: "/sap/opu/odata/sap/API_SUPPLIERINVOICE_PROCESS_SRV/A_SupplierInvoice",
			CSRFPath:     "/sap/opu/odata/sap/API_SUPPLIERINVOICE_PROCESS_SRV/",
			Username:     "Y_BPI_CU_SAP_COM_0057",
			Password:     "PowerofOne@123456789",
			IsActive:     true,
		},
	}

	for _, ep := range defaults {
		var count int64
		r.db.Model(&models.SAPEndpoint{}).Where("UPPER(service_code) = ?", ep.ServiceCode).Count(&count)
		if count == 0 {
			ep.CreatedAt = time.Now()
			ep.UpdatedAt = time.Now()
			_ = r.db.Create(&ep).Error
		}
	}
	return nil
}
