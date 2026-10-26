package routes

import (
	"net/http"

	_ "golang-bpi-testing/docs"
	"golang-bpi-testing/internal/controller"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func SetupRouter(ctrl *controller.WebController) *gin.Engine {
	r := gin.Default()

	// Redirect root & /swagger path ke Swagger UI
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})
	r.GET("/swagger", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})

	// Swagger UI handler
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := r.Group("/api/v1")
	{
		api.GET("/health", ctrl.HealthCheck)
		api.GET("/documents", ctrl.GetDocuments)
		api.GET("/integration-logs", ctrl.GetIntegrationLogs)
		api.POST("/ingest/po", ctrl.IngestManualPO)
		api.POST("/trigger/push", ctrl.TriggerPush)
		api.POST("/trigger/pull", ctrl.TriggerPull)
		api.GET("/trigger/pull", ctrl.TriggerPull)
		api.GET("/sap/endpoints", ctrl.GetSAPEndpoints)
		api.POST("/sap/endpoints/test", ctrl.TestSAPEndpoint)
		api.POST("/sap/endpoints", ctrl.SaveSAPEndpoint)
		api.POST("/sap/execute-test", ctrl.ExecuteSAPTest)
		api.GET("/scheduler/status", ctrl.GetSchedulerStatus)
		api.POST("/scheduler/toggle", ctrl.ToggleAutoPull)
		api.POST("/exchange-rate", ctrl.ReceiveExchangeRate)
		api.GET("/exchange-rates", ctrl.GetExchangeRates)
		api.GET("/uom-mappings", ctrl.GetUOMMappings)
		api.GET("/sap/material-document/:doc_no", ctrl.GetSAPMaterialDocumentDetail)
	}

	// Kompatibilitas dengan endpoint ShoeTown /admin/integration-logs
	admin := r.Group("/admin")
	{
		admin.GET("/integration-logs", ctrl.GetIntegrationLogs)
	}

	return r
}
