package main

import (
    "fmt"
    "middleware-panel/config"
    "middleware-panel/models"
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

    // 2. Oper config.DB (GORM) ke dalam SetupRouter
    router := routes.SetupRouter(config.DB)

    port := os.Getenv("APP_PORT")
    if port == "" {
        port = "8091"
    }

    fmt.Printf("Server berjalan di port :%s...\n", port)
    router.Run(":" + port)
}