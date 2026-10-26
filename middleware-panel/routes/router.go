package routes

import (
	"middleware-panel/controllers"
	"middleware-panel/repository"
	"middleware-panel/services"
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// requireMenuGuard returns a Gin middleware ensuring the authenticated user has access to menuKey.
func requireMenuGuard(menuKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userId := session.Get("user_id")
		if userId == nil {
			if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/sync/") || strings.HasPrefix(c.Request.URL.Path, "/documents/") {
				c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Sesi telah berakhir. Silakan login kembali."})
			} else {
				c.Redirect(http.StatusFound, "/login")
			}
			c.Abort()
			return
		}

		role, _ := session.Get("role").(string)
		if role == "admin" {
			c.Next()
			return
		}

		allowed, _ := session.Get("allowed_menus").(string)
		if allowed == "*" || allowed == "" {
			c.Next()
			return
		}

		parts := strings.Split(allowed, ",")
		hasAccess := false
		for _, p := range parts {
			if strings.TrimSpace(p) == menuKey {
				hasAccess = true
				break
			}
		}

		if !hasAccess {
			if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/sync/") || strings.HasPrefix(c.Request.URL.Path, "/documents/") {
				c.JSON(http.StatusForbidden, gin.H{
					"status":  "error",
					"message": "Akses ditolak: Akun Anda tidak memiliki izin untuk fitur " + menuKey,
				})
			} else {
				fallback := "/dashboard"
				if menuKey == "dashboard" {
					fallback = getFirstAllowedPath(allowed)
				}
				c.Redirect(http.StatusFound, fallback+"?err=unauthorized")
			}
			c.Abort()
			return
		}

		c.Next()
	}
}

func getFirstAllowedPath(allowed string) string {
	parts := strings.Split(allowed, ",")
	for _, p := range parts {
		switch strings.TrimSpace(p) {
		case "dashboard":
			return "/dashboard"
		case "exchange_rates":
			return "/exchange-rates"
		case "uom_mapping":
			return "/uom-mapping"
		case "config":
			return "/config"
		case "sap_endpoints":
			return "/sap-endpoints"
		case "table_mapping":
			return "/table-api-mapping"
		case "scheduler":
			return "/scheduler-config"
		case "backup":
			return "/backup-config"
		case "users":
			return "/users"
		}
	}
	return "/login"
}

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

	// 0. User Access Domain
	userRepo := repository.NewUserRepository(db)
	userService := services.NewUserService(userRepo)
	userController := controllers.NewUserController(userService)

	// 1. Auth Domain (Terhubung ke User Access Database)
	authRepo := repository.NewAuthRepository(userRepo)
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

	// 7. SAP Integration Endpoints & Sandbox Domain
	sapEndpointRepo := repository.NewSAPEndpointRepository(db)
	sapEndpointService := services.NewSAPEndpointService(sapEndpointRepo)
	sapEndpointController := controllers.NewSAPEndpointController(sapEndpointService)

	// 8. Table-to-API Mapping Domain
	tableMappingRepo := repository.NewTableAPIMappingRepository(db)
	tableMappingService := services.NewTableAPIMappingService(tableMappingRepo, sapEndpointRepo)
	tableMappingController := controllers.NewTableAPIMappingController(tableMappingService)

	// 9. Exchange Rate Domain
	exchangeRateRepo := repository.NewExchangeRateRepository(db)
	exchangeRateService := services.NewExchangeRateService(exchangeRateRepo)
	exchangeRateController := controllers.NewExchangeRateController(exchangeRateService)

	// 10. UOM Mapping Domain
	uomMappingRepo := repository.NewUOMMappingRepository(db)
	uomMappingService := services.NewUOMMappingService(uomMappingRepo)
	uomMappingController := controllers.NewUOMMappingController(uomMappingService)

	// ==========================================
	// ROUTES MAPPING
	// ==========================================

	// Auth
	r.GET("/login", authController.ShowLogin)
	r.POST("/login", authController.ProcessLogin)
	r.GET("/logout", authController.ProcessLogout)

	// Dashboard & SAP Sync (menu: dashboard)
	r.GET("/dashboard", requireMenuGuard("dashboard"), dashController.ShowDashboard)
	r.POST("/sync/send", requireMenuGuard("dashboard"), dashController.HandleSendToSAP)
	r.POST("/sync/send-bulk", requireMenuGuard("dashboard"), dashController.HandleBulkSendToSAP)
	r.POST("/documents/delete", requireMenuGuard("dashboard"), dashController.HandleDeleteDocument)
	r.POST("/documents/delete-bulk", requireMenuGuard("dashboard"), dashController.HandleBulkDelete)
	r.POST("/documents/clear-all", requireMenuGuard("dashboard"), dashController.HandleClearAll)
	r.POST("/api/trigger-push", requireMenuGuard("dashboard"), dashController.HandleTriggerPush)
	r.POST("/api/ingest-po", requireMenuGuard("dashboard"), dashController.HandleIngestTestPO)
	r.GET("/api/documents/:parent_no/children", requireMenuGuard("dashboard"), dashController.GetChildrenAPI)

	// Proxy Audit Trail API (Sudah dinamis membaca BACKUP_API_URL dari .env)
	r.GET("/api/audit-trail", requireMenuGuard("dashboard"), dashController.GetAuditTrailAPI)

	// App Config (.env) (menu: config)
	r.GET("/config", requireMenuGuard("config"), appConfigController.ShowConfig)
	r.POST("/api/config", requireMenuGuard("config"), appConfigController.SaveConfig)
	r.DELETE("/api/config/:key", requireMenuGuard("config"), appConfigController.DeleteConfig)

	// SAP Integration Endpoints & Sandbox (menu: sap_endpoints)
	r.GET("/sap-endpoints", requireMenuGuard("sap_endpoints"), sapEndpointController.ShowEndpoints)
	r.POST("/api/sap-endpoints", requireMenuGuard("sap_endpoints"), sapEndpointController.HandleSaveEndpoint)
	r.POST("/api/sap-endpoints/test", requireMenuGuard("sap_endpoints"), sapEndpointController.HandleTestHandshake)
	r.POST("/api/sap-endpoints/execute-sandbox", requireMenuGuard("sap_endpoints"), sapEndpointController.HandleExecuteSandbox)
	r.GET("/api/sap/material-document/:doc_no", requireMenuGuard("sap_endpoints"), sapEndpointController.HandleGetMaterialDocument)
	r.DELETE("/api/sap-endpoints/:code", requireMenuGuard("sap_endpoints"), sapEndpointController.HandleDeleteEndpoint)

	// Exchange Rate (Kurs Pajak) (menu: exchange_rates)
	r.GET("/exchange-rates", requireMenuGuard("exchange_rates"), exchangeRateController.ShowExchangeRates)
	r.POST("/api/exchange-rates/upload", requireMenuGuard("exchange_rates"), exchangeRateController.HandleUploadJSON)
	r.GET("/api/exchange-rates", requireMenuGuard("exchange_rates"), exchangeRateController.GetRatesAPI)

	// UOM Mapping (menu: uom_mapping)
	r.GET("/uom-mapping", requireMenuGuard("uom_mapping"), uomMappingController.ShowUOMMappings)
	r.POST("/api/uom-mapping", requireMenuGuard("uom_mapping"), uomMappingController.HandleSave)
	r.POST("/api/uom-mapping/toggle/:id", requireMenuGuard("uom_mapping"), uomMappingController.HandleToggleActive)
	r.DELETE("/api/uom-mapping/:id", requireMenuGuard("uom_mapping"), uomMappingController.HandleDelete)

	// Scheduler Config & Manual Pull (menu: scheduler)
	r.GET("/scheduler-config", requireMenuGuard("scheduler"), schedController.ShowSchedulerConfig)
	r.POST("/scheduler-config", requireMenuGuard("scheduler"), schedController.UpdateSchedulerConfig)
	r.POST("/scheduler/pull", requireMenuGuard("scheduler"), schedController.ExecutePullNow)
	r.GET("/scheduler/pull", requireMenuGuard("scheduler"), schedController.ExecutePullNow)
	r.POST("/scheduler/toggle", requireMenuGuard("scheduler"), schedController.QuickToggleAutoPull)

	// Backup Scheduler Config (menu: backup)
	r.GET("/backup-config", requireMenuGuard("backup"), backupController.ShowConfigPage)
	r.POST("/backup-config/update", requireMenuGuard("backup"), backupController.UpdateSchedule)
	r.POST("/backup-config/run", requireMenuGuard("backup"), backupController.HandleBackupNow)

	// Material GL Mapping (menu: config)
	r.GET("/gl-mapping", requireMenuGuard("config"), glMappingController.ShowPage)
	r.POST("/api/gl-mapping", requireMenuGuard("config"), glMappingController.Save)
	r.DELETE("/api/gl-mapping/:id", requireMenuGuard("config"), glMappingController.Delete)

	// Table-to-API Mapping Matrix (menu: table_mapping)
	r.GET("/table-api-mapping", requireMenuGuard("table_mapping"), tableMappingController.ShowMappings)
	r.POST("/api/table-mapping", requireMenuGuard("table_mapping"), tableMappingController.HandleSaveMapping)
	r.POST("/api/table-mapping/toggle/:id", requireMenuGuard("table_mapping"), tableMappingController.HandleToggleActive)
	r.DELETE("/api/table-mapping/:id", requireMenuGuard("table_mapping"), tableMappingController.HandleDeleteMapping)

	// User Access Management (menu: users)
	r.GET("/users", requireMenuGuard("users"), userController.ShowUsers)
	r.POST("/api/users", requireMenuGuard("users"), userController.CreateUser)
	r.POST("/api/users/update", requireMenuGuard("users"), userController.UpdateUser)
	r.POST("/api/users/toggle/:id", requireMenuGuard("users"), userController.ToggleActive)
	r.DELETE("/api/users/:id", requireMenuGuard("users"), userController.DeleteUser)

	return r
}
