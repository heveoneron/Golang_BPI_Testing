package routes

import (
	"middleware-panel/controllers"
	"middleware-panel/repository"
	"middleware-panel/services"
	"os"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func SetupRouter(db *gorm.DB) *gin.Engine {
	r := gin.Default()

	// 1. Ambil Port yang sedang digunakan oleh instance ini
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8091" // Fallback ke dev port
	}

	// 2. Buat nama cookie sesi menjadi SANGAT SPESIFIK berdasarkan port
	// Hasil: "mw_session_8091", "mw_session_8092", "mw_session_8093"
	sessionName := "mw_session_" + port

	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		sessionSecret = "kunci_rahasia_admin"
	}

	store := cookie.NewStore([]byte(sessionSecret))
	store.Options(sessions.Options{MaxAge: 3600, Path: "/", HttpOnly: true})
	r.Use(sessions.Sessions(sessionName, store))

	// Static & Templates
	r.Static("/static", "./static")
	r.LoadHTMLGlob("templates/*")
	r.GET("/", func(c *gin.Context) { c.Redirect(302, "/login") })

	// 1. Auth Domain
	authRepo := repository.NewAuthRepository()
	authService := services.NewAuthService(authRepo)
	authController := controllers.NewAuthController(authService)

	// 2. Dashboard & Sync Domain
	dashRepo := repository.NewDashboardRepository(db)
	dashService := services.NewDashboardService(dashRepo)
	syncService := services.NewSyncService(dashRepo)
	dashController := controllers.NewDashboardController(dashService, syncService)

	// 3. App Config Domain (.env)
	appConfigRepo := repository.NewAppConfigRepository(db)
	appConfigService := services.NewAppConfigService(appConfigRepo)
	appConfigController := controllers.NewAppConfigController(appConfigService)

	// 4. Scheduler Automasi Domain
	schedRepo := repository.NewSchedulerRepository(db)
	schedService := services.NewSchedulerService(schedRepo)
	schedController := controllers.NewSchedulerController(schedService)

	// 5. Backup Config Domain
	backupRepo := repository.NewBackupScheduleRepository(db)
	backupService := services.NewBackupScheduleService(backupRepo)
	backupController := controllers.NewBackupScheduleController(backupService)

	// 6. Material GL Mapping Domain
	glMappingRepo := repository.NewGLMappingRepository(db)
	glMappingService := services.NewGLMappingService(glMappingRepo)
	glMappingController := controllers.NewGLMappingController(glMappingService)

	// ==========================================
	// ROUTES MAPPING
	// ==========================================

	// Auth
	r.GET("/login", authController.ShowLogin)
	r.POST("/login", authController.ProcessLogin)
	r.GET("/logout", authController.ProcessLogout)

	// Dashboard & SAP Sync
	r.GET("/dashboard", dashController.ShowDashboard)
	r.POST("/sync/send", dashController.HandleSendToSAP)
	r.POST("/sync/send-bulk", dashController.HandleBulkSendToSAP)
	r.GET("/api/documents/:parent_no/children", dashController.GetChildrenAPI)

	// Proxy Audit Trail API (Sudah dinamis membaca BACKUP_API_URL dari .env)
	r.GET("/api/audit-trail", dashController.GetAuditTrailAPI)

	// App Config (.env)
	r.GET("/config", appConfigController.ShowConfig)
	r.POST("/api/config", appConfigController.SaveConfig)
	r.DELETE("/api/config/:key", appConfigController.DeleteConfig)

	// Scheduler Config & Manual Pull
	r.GET("/scheduler-config", schedController.ShowSchedulerConfig)
	r.POST("/scheduler-config", schedController.UpdateSchedulerConfig)
	r.POST("/scheduler/pull", schedController.ExecutePullNow)
	r.GET("/scheduler/pull", schedController.ExecutePullNow)

	// Backup Scheduler Config
	r.GET("/backup-config", backupController.ShowConfigPage)
	r.POST("/backup-config/update", backupController.UpdateSchedule)
	r.POST("/backup-config/run", backupController.HandleBackupNow)

	// Material GL Mapping
	r.GET("/gl-mapping", glMappingController.ShowPage)
	r.POST("/api/gl-mapping", glMappingController.Save)
	r.DELETE("/api/gl-mapping/:id", glMappingController.Delete)

	return r
}
