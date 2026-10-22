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

    // Auto Migrate Tabel
    config.DB.AutoMigrate(
    	&models.Document{},
    	&models.SchedulerConfig{},
    	&models.BackupSchedule{},
    	&models.Config{},
    	&models.MaterialGLMapping{},
    	&models.SystemLog{},
    	&models.IntegrationLog{},
    	&models.SAPEndpoint{},
    	&models.TableAPIMapping{},
    	&models.User{},
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

    // Seed SAP Endpoints (PO, BP, Product Master)
    sapRepo := repository.NewSAPEndpointRepository(config.DB)
    _ = sapRepo.Save(&models.SAPEndpoint{
        ServiceCode:  "PO",
        ServiceName:  "Purchase Order Integration (OData v4)",
        ScenarioID:   "SAP_COM_0053",
        ODataVersion: "v4",
        BaseURL:      "https://my445160-api.s4hana.cloud.sap",
        EndpointPath: "/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder",
        Username:     "CU_SAP_COM_0053",
        Password:     "PowerofOne@123456789",
        IsActive:     true,
    })
    _ = sapRepo.Save(&models.SAPEndpoint{
        ServiceCode:  "BP",
        ServiceName:  "Business Partner Integration (OData v2)",
        ScenarioID:   "SAP_COM_0008",
        ODataVersion: "v2",
        BaseURL:      "https://my445160-api.s4hana.cloud.sap",
        EndpointPath: "/sap/opu/odata/sap/API_BUSINESS_PARTNER/A_BusinessPartner",
        Username:     "CU_SAP_COM_0008",
        Password:     "PowerofOne@123456789",
        IsActive:     true,
    })
    _ = sapRepo.Save(&models.SAPEndpoint{
        ServiceCode:  "PRODUCT",
        ServiceName:  "Product / Material Master Integration (OData v2)",
        ScenarioID:   "SAP_COM_0009",
        ODataVersion: "v2",
        BaseURL:      "https://my445160-api.s4hana.cloud.sap",
        EndpointPath: "/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product",
        Username:     "CU_SAP_COM_0009",
        Password:     "PowerofOne@123456789",
        IsActive:     true,
    })

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