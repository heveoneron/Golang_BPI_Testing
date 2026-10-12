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

	// Load HTML templates & Static assets
	r.LoadHTMLGlob("web/templates/*")
	r.Static("/static", "web/static")

	// Web Panel Interface
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/panel")
	})
	r.GET("/panel", ctrl.ShowPanel)

	// Swagger UI handler
	r.GET("/swagger", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := r.Group("/api/v1")
	{
		api.GET("/health", ctrl.HealthCheck)
		api.GET("/documents", ctrl.GetDocuments)
		api.POST("/ingest/po", ctrl.IngestManualPO)
		api.POST("/trigger/push", ctrl.TriggerPush)
	}

	return r
}
