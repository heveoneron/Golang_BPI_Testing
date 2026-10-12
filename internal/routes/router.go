package routes

import (
	"golang-bpi-testing/internal/controller"

	"github.com/gin-gonic/gin"
)

func SetupRouter(ctrl *controller.WebController) *gin.Engine {
	r := gin.Default()

	api := r.Group("/api/v1")
	{
		api.GET("/health", ctrl.HealthCheck)
		api.GET("/documents", ctrl.GetDocuments)
		api.POST("/ingest/po", ctrl.IngestManualPO)
		api.POST("/trigger/push", ctrl.TriggerPush)
	}

	return r
}
