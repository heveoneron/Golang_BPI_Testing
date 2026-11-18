package main

import (
    "fmt"
    "middleware-panel/config"
    "middleware-panel/models"
    "middleware-panel/repository"
    "middleware-panel/routes"
    "os"
)

func main() {
    // 1. Inisialisasi Database
    config.ConnectDatabase()
    // Header X-API-Key ke Engine bila ENGINE_API_KEY diisi (.env dimuat oleh ConnectDatabase).
    config.InstallEngineAuth()

    // Auto Migrate Tabel
    config.DB.AutoMigrate(
    	&models.Document{},
    	&models.SchedulerConfig{},
    	&models.BackupSchedule{},
    	&models.Config{},
    	&models.MaterialGLMapping{},
    	&models.MaterialConfigMapping{},
    	&models.SystemLog{},
    	&models.IntegrationLog{},
    	&models.SAPEndpoint{},
    	&models.TableAPIMapping{},
    	&models.User{},
    	&models.ExchangeRate{},
    	&models.UOMMapping{},
    	&models.BPGroupMapping{},
    	&models.ReconAccountMapping{},
    	&models.ValuationClassGLMapping{},
    	&models.VendorMaster{},
    	&models.VendorBankAccount{},
    	&models.DefaultValue{},
    )

    // Seed default record BackupSchedule jika belum ada untuk mencegah error template
    var backupCount int64
    config.DB.Model(&models.BackupSchedule{}).Count(&backupCount)
    if backupCount == 0 {
        config.DB.Create(&models.BackupSchedule{
            DatabaseName:   "postgres",
            DatabaseType:   "postgresql",
            BackupLevel:    "full",
            BackupLocation: "local",
            ScheduleMode:   "daily",
            ExecutionTime:  "02:00",
            RetentionDays:  7,
            IsActive:       false,
        })
    }

    // Seed default records Config (System Target Integration Configuration) jika belum ada
    defaultConfigs := []models.Config{
        {Key: "bot_base_url", Value: "http://localhost:8085/api/v1", Description: "Base URL untuk Bot Poller / Ingestion Engine integrasi Maximo"},
        {Key: "bpa_base_url", Value: "https://spa-api.eu10.process-automation.sap.biz", Description: "Base URL SAP Build Process Automation (BPA) trigger API"},
        {Key: "sap_base_url", Value: "https://my445160-api.s4hana.cloud.sap", Description: "Host sentral SAP S/4HANA Grow Cloud untuk seluruh layanan OData"},
        {Key: "sap_username", Value: "CU_SAP_COM_0053", Description: "Communication User default untuk koneksi SAP S/4HANA"},
        {Key: "sap_password", Value: "PowerofOne@123456789", Description: "Communication Password default untuk koneksi SAP S/4HANA"},
        {Key: "erp_callback_url", Value: "http://host.docker.internal:1433/maximo/api/webhook/status", Description: "Webhook callback URL ke IBM Maximo untuk update status dokumen SAP"},
        {Key: "prefix_bot_mapping", Value: `{"45": "BPA_PO_NORMAL_TRIGGER", "47": "BPA_PO_CONTRACT_TRIGGER"}`, Description: "Mapping prefix nomor PO Maximo ke BPA Workflow Trigger ID"},
        {Key: "doc_type_bot_mapping", Value: `{"NB": "WF_STANDARD_PO", "ZPRC": "WF_PROJECT_PO", "ZCTR": "WF_CONTRACT_PO"}`, Description: "Mapping Document Type Maximo ke Workflow Definition ID"},
    }
    for _, cfg := range defaultConfigs {
        var count int64
        config.DB.Model(&models.Config{}).Where("key = ?", cfg.Key).Count(&count)
        if count == 0 {
            config.DB.Create(&cfg)
        }
    }

    // Seed SAP Endpoints (hanya insert yang belum ada di database, TIDAK menimpa konfigurasi yang sudah diedit user)
    sapRepo := repository.NewSAPEndpointRepository(config.DB)
    _ = sapRepo.SeedDefaults()

    // Seed Table-to-API Mappings & Default Admin User
    tableMappingRepo := repository.NewTableAPIMappingRepository(config.DB)
    _ = tableMappingRepo.SeedDefaults()

    userRepo := repository.NewUserRepository(config.DB)
    _ = userRepo.SeedAdminUser()

    // 2. Oper config.DB (GORM) ke dalam SetupRouter
    router := routes.SetupRouter(config.DB)

    port := os.Getenv("APP_PORT")
    if port == "" {
        port = "8091"
    }

    fmt.Printf("Server berjalan di port :%s...\n", port)
    router.Run(":" + port)
}