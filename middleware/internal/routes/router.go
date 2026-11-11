package routes

import (
	"net/http"

	"golang-bpi-testing/docs"
	"golang-bpi-testing/internal/controller"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func SetupRouter(ctrl *controller.WebController) *gin.Engine {
	docs.SwaggerInfo.Host = ""

	r := gin.Default()

	// CORS Middleware agar bisa diakses dari browser/VPN laptop
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})


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
		api.POST("/ingest/vendor", ctrl.IngestManualVendor)
		api.POST("/ingest/bp", ctrl.IngestManualVendor)
		api.POST("/ingest/item", ctrl.IngestManualItem)
		api.POST("/ingest/product", ctrl.IngestManualItem)
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

// RegisterFinanceRoutes mendaftarkan endpoint RICEF 1-9 di bawah /api/v1/finance.
func RegisterFinanceRoutes(r *gin.Engine, fc *controller.FinanceController) {
	fin := r.Group("/api/v1/finance")
	{
		// RICEF 1
		fin.GET("/budget/masters", fc.ListBudgetMasters)
		fin.POST("/budget/masters", fc.SaveBudgetMaster)
		fin.POST("/budget/supplement", fc.SupplementBudget)
		fin.POST("/budget/transfer", fc.TransferBudget)
		fin.GET("/budget/locks", fc.ListBudgetLocks)
		fin.POST("/budget/locks/release", fc.ReleaseBudgetLock)
		// RICEF 2
		fin.GET("/journal-entries", fc.ListJournalEntries)
		fin.POST("/journal-entries", fc.CreateJournalEntry)
		fin.POST("/goods-issue-adjustment", fc.ProcessGoodsIssueOrAdjustment)
		fin.GET("/valuation-class-mappings", fc.ListValuationClassGLMappings)
		fin.POST("/valuation-class-mappings", fc.SaveValuationClassGLMapping)
		fin.POST("/inventory/sync", fc.SyncInventoryJournals)
		// RICEF 3/4, 5, 9
		fin.GET("/gl-line-items", fc.ListGLLineItems)
		fin.GET("/gl-line-items/detail", fc.GetGLLineItemDetail)
		fin.GET("/payment-proposals", fc.ListPaymentProposals)
		fin.GET("/payment-proposals/bank-file", fc.DownloadBankFile)
		fin.GET("/fixed-assets", fc.ListFixedAssets)
		fin.GET("/fixed-assets/detail", fc.GetFixedAssetDetail)
		fin.POST("/sync/:target", fc.TriggerSync)
		// RICEF 6
		fin.POST("/exchange-rates/queue", fc.QueueExchangeRates)
		fin.GET("/exchange-rates/documents", fc.ListExchangeRateDocuments)
		// RICEF 7
		fin.POST("/supplier-invoices/upload", fc.UploadSupplierInvoices)
		fin.GET("/supplier-invoices", fc.ListSupplierInvoices)
		fin.GET("/reimbursement-mappings", fc.ListReimbursementMappings)
		fin.POST("/reimbursement-mappings", fc.SaveReimbursementMapping)
		fin.DELETE("/reimbursement-mappings/:id", fc.DeleteReimbursementMapping)
		fin.GET("/employee-vendors", fc.ListEmployeeVendors)
		fin.POST("/employee-vendors", fc.SaveEmployeeVendor)
		fin.POST("/employee-vendors/import", fc.ImportEmployeeVendors)
		fin.DELETE("/employee-vendors/:code", fc.DeleteEmployeeVendor)
		fin.POST("/vendor-master/sync-sap", fc.SyncVendorMaster)
		// SAP -> Maximo (dbo.mxinvoice)
		fin.POST("/maximo/export", fc.ExportToMaximo)
		fin.GET("/maximo/exports", fc.ListMaximoExports)
		// RICEF 8
		fin.GET("/assets/acquisitions", fc.ListAssetAcquisitions)
		fin.POST("/assets/acquisitions", fc.ParkAssetAcquisition)
		fin.GET("/assets/acquisitions/:id", fc.GetAssetAcquisition)
		fin.POST("/assets/acquisitions/:id/print", fc.PrintAssetAcquisition)
		fin.POST("/assets/acquisitions/:id/approve", fc.ApproveAssetAcquisition)
		fin.POST("/assets/acquisitions/:id/reject", fc.RejectAssetAcquisition)
	}
}
