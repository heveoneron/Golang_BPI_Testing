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

    // Auto Migrate Tabel (UBAH KE Document)
    config.DB.AutoMigrate(&models.Document{})

    // 2. Oper config.DB (GORM) ke dalam SetupRouter
    router := routes.SetupRouter(config.DB)

    port := os.Getenv("APP_PORT")
    if port == "" {
        port = "8091"
    }

    fmt.Printf("Server berjalan di port :%s...\n", port)
    router.Run(":" + port)
}