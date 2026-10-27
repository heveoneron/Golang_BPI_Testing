package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectDatabase() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Notice: File .env tidak ditemukan, menggunakan variabel environment sistem/docker.")
	}

	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("DB_USER")
	if user == "" {
		user = "postgres"
	}
	pass := os.Getenv("DB_PASSWORD")
	if pass == "" {
		pass = "postgres123"
	}
	dbname := os.Getenv("DB_NAME")
	if dbname == "" {
		dbname = "bpi_middleware_db"
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Jakarta",
		host, user, pass, dbname, port,
	)

	var database *gorm.DB
	maxRetries := 15
	for i := 1; i <= maxRetries; i++ {
		database, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err == nil {
			break
		}
		log.Printf("[WARN] Menunggu database PostgreSQL siap (%d/%d): %v", i, maxRetries, err)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		log.Fatal("Gagal terhubung ke database setelah beberapa percobaan:", err)
	}

	DB = database
	fmt.Println("Berhasil terhubung ke database PostgreSQL!")
}
