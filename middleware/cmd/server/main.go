package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang-bpi-testing/internal/client"
	"golang-bpi-testing/internal/controller"
	"golang-bpi-testing/internal/repository"
	"golang-bpi-testing/internal/routes"
	"golang-bpi-testing/internal/service"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	_ "github.com/microsoft/go-mssqldb"
)

// @title           BPI Maximo to SAP Grow Middleware Engine API
// @version         1.0
// @description     Middleware integrasi enterprise: IBM Maximo <-> PostgreSQL Staging & State Machine <-> SAP Grow S/4HANA Cloud OData.
// @contact.name    BPI Middleware Development Team
// @host            localhost:8085
// @BasePath        /api/v1
// @schemes         http https
func main() {
	_ = godotenv.Load()

	log.Println("=====================================================")
	log.Println("⚡ STARTING BPI MAXIMO TO SAP GROW MIDDLEWARE ENGINE ⚡")
	log.Println("=====================================================")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Inisialisasi Database PostgreSQL
	pgConnString := os.Getenv("PG_CONN_STRING")
	if pgConnString == "" {
		pgConnString = "postgres://postgres:postgres@localhost:5432/bpi_middleware_db?sslmode=disable"
	}

	pgPool, err := pgxpool.New(ctx, pgConnString)
	if err != nil {
		log.Printf("[WARN] PostgreSQL Pool gagal connect ke %s: %v (Jalankan lokal Postgres saat testing)", pgConnString, err)
	} else {
		log.Println("[INFO] Terhubung ke PostgreSQL Staging Engine.")
	}

	// 2. Inisialisasi Database SQL Server 2012 (Maximo Staging DB)
	sqlConnString := os.Getenv("SQL_CONN_STRING")
	var sqlDB *sql.DB
	if sqlConnString != "" {
		sqlDB, err = sql.Open("sqlserver", sqlConnString)
		if err != nil {
			log.Printf("[WARN] SQL Server tidak dapat terhubung: %v", err)
		} else {
			log.Println("[INFO] Terhubung ke SQL Server 2012 Maximo.")
		}
	} else {
		log.Println("[INFO] SQL_CONN_STRING tidak diatur. Menggunakan Mock Mode untuk SQL Server.")
	}

	// 3. Inisialisasi SAP Grow OData Client
	sapBaseURL := os.Getenv("SAP_BASE_URL")
	if sapBaseURL == "" {
		sapBaseURL = "MOCK" // Default ke Mock saat testing lokal
	}
	sapUser := os.Getenv("SAP_USER")
	sapPass := os.Getenv("SAP_PASS")
	sapClient := client.NewSAPClient(sapBaseURL, sapUser, sapPass)

	// 4. Inisialisasi Repositori & Service
	sqlRepo := repository.NewSQLServerRepository(sqlDB)
	pgRepo := repository.NewPostgresRepository(pgPool)
	syncSvc := service.NewSyncService(sqlRepo, pgRepo, sapClient)

	// 5. Jalankan Dual-Thread Background Worker (Pull 3 Menit & Push Dispatcher)
	var wg sync.WaitGroup
	wg.Add(2)
	go startPullWorker(ctx, syncSvc, pgRepo, &wg)
	go startPushWorker(ctx, syncSvc, &wg)

	// 6. Jalankan HTTP API Server
	webCtrl := controller.NewWebController(pgRepo, syncSvc)
	router := routes.SetupRouter(webCtrl)

	appPort := os.Getenv("APP_PORT")
	if appPort == "" {
		appPort = "8085"
	}

	log.Printf("[INFO] Swagger OpenAPI UI: http://localhost:%s/swagger/index.html", appPort)

	srv := &http.Server{
		Addr:    ":" + appPort,
		Handler: router,
	}

	go func() {
		log.Printf("[INFO] HTTP API Server mendengarkan di port :%s", appPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Listen error: %s\n", err)
		}
	}()

	// Graceful Shutdown Handler
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[INFO] Mematikan service secara graceful...")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	wg.Wait()
	log.Println("[INFO] Engine berhasil dimatikan dengan aman.")
}

// Background Worker 1: Pull dari SQL Server & Transform ke Core Documents
func startPullWorker(ctx context.Context, svc *service.SyncService, pgRepo *repository.PostgresRepository, wg *sync.WaitGroup) {
	defer wg.Done()

	intervalMin := 3
	if envInterval := os.Getenv("PULL_INTERVAL_MINUTES"); envInterval != "" {
		if m, err := strconv.Atoi(envInterval); err == nil && m > 0 {
			intervalMin = m
		}
	}

	initActive, _ := pgRepo.IsAutoPullActive(ctx)
	statusStr := "AKTIF (ON)"
	if !initActive {
		statusStr = "NONAKTIF (OFF)"
	}
	log.Printf("[WORKER] Pull Worker aktif (Interval: %d Menit, Status Auto-Pull: %s)...", intervalMin, statusStr)

	ticker := time.NewTicker(time.Duration(intervalMin) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if pgRepo.Pool == nil {
				continue
			}

			// Pengecekan status dinamis: Apakah fitur Auto-Pull ON atau OFF (via .env atau Panel)
			isActive, dbLimit := pgRepo.IsAutoPullActive(ctx)
			if !isActive {
				log.Println("[WORKER-PULL] Auto get data dari Maximo sedang NONAKTIF / OFF (via .env atau Panel). Siklus penarikan otomatis dilewati.")
				continue
			}

			pullLimit := dbLimit
			if pullLimit <= 0 {
				pullLimit = 1
			}
			if envLimit := os.Getenv("PULL_BATCH_LIMIT"); envLimit != "" {
				if l, err := strconv.Atoi(envLimit); err == nil && l > 0 {
					pullLimit = l
				}
			}

			pulled, transformed := svc.PullDataFromSQLServer(ctx, pullLimit)
			if pulled > 0 {
				log.Printf("[WORKER-PULL] Berhasil menarik %d record dari SQL Server (Transformed: %d).", pulled, transformed)
			}
		}
	}
}

// Background Worker 2: Push Core Documents ke SAP Grow
func startPushWorker(ctx context.Context, svc *service.SyncService, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Println("[WORKER] Push Worker aktif (Interval 1 Menit)...")
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pushed := svc.PushDocumentsToSAP(ctx)
			if pushed > 0 {
				log.Printf("[WORKER-PUSH] Berhasil mengirimkan %d dokumen ke SAP Grow.", pushed)
			}
		}
	}
}
