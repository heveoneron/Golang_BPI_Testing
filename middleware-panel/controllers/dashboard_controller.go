package controllers

import (
	"encoding/json"
	"io"
	"math"
	"middleware-panel/services"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type DashboardController struct {
	dashService services.DashboardService
	syncService services.SyncService
}

func NewDashboardController(ds services.DashboardService, ss services.SyncService) *DashboardController {
	return &DashboardController{dashService: ds, syncService: ss}
}

func (c *DashboardController) ShowDashboard(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	search := ctx.Query("search")
	status := ctx.Query("status")
	docType := ctx.Query("doc_type")

	startDate := ctx.Query("start_date")
	endDate := ctx.Query("end_date")

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "10"))
	if limit < 1 {
		limit = 10
	}

	// 1. UBAH: Tambahkan variabel 'trends' untuk menampung balikan dari service
	docs, totalRoot, totalAll, stats, trends, _ := c.dashService.GetDashboardData(search, status, docType, startDate, endDate, page, limit)

	for i := range docs {
		if docs[i].Status == "success" {
			if docs[i].DocType == "PPMBillOfMaterialC" ||
				docs[i].DocType == "PPMBillOfMaterialItemC" ||
				docs[i].DocType == "PPMBillOfMaterialItemU" ||
				docs[i].DocType == "PPMBillOfMaterialItemD" {

				customMsg := "BOM processed successfully in SAP (Direct)"
				docs[i].ErrorMessage = &customMsg

			} else if docs[i].ErrorMessage == nil || *docs[i].ErrorMessage == "" {
				defaultMsg := "Finalized by BPA"
				docs[i].ErrorMessage = &defaultMsg
			}
		}
	}

	docTypeOptions, _ := c.dashService.GetDocTypeOptions()

	totalPages := int(math.Ceil(float64(totalRoot) / float64(limit)))

	startIndex := (page-1)*limit + 1
	endIndex := startIndex + len(docs) - 1
	if len(docs) == 0 {
		startIndex, endIndex = 0, 0
	}

	// 2. UBAH: Konversi map Golang menjadi Array JSON String
	trendAll, _ := json.Marshal(trends["All"])
	trendSuccess, _ := json.Marshal(trends["Success"])
	trendFailed, _ := json.Marshal(trends["Failed"])
	trendSent, _ := json.Marshal(trends["Sent"])
	trendPending, _ := json.Marshal(trends["Pending"])

	// 3. UBAH: Suntikkan variabel JSON ke HTML
	ctx.HTML(http.StatusOK, "dashboard.html", gin.H{
		"orders":         docs,
		"search":         search,
		"status":         status,
		"docType":        docType,
		"StartDate":      startDate,
		"EndDate":        endDate,
		"docTypeOptions": docTypeOptions,
		"page":           page,
		"limit":          limit,
		"totalData":      totalAll,
		"totalPages":     totalPages,
		"hasPrev":        page > 1,
		"hasNext":        page < totalPages,
		"prevPage":       page - 1,
		"nextPage":       page + 1,
		"startIndex":     startIndex,
		"endIndex":       endIndex,
		"Username":       session.Get("username"),
		"TotalAll":       stats["TotalAll"],
		"TotalSuccess":   stats["TotalSuccess"],
		"TotalFailed":    stats["TotalFailed"],
		"TotalSent":      stats["TotalSent"],
		"TotalPending":   stats["TotalPending"],
		"TrendAll":       string(trendAll),
		"TrendSuccess":   string(trendSuccess),
		"TrendFailed":    string(trendFailed),
		"TrendSent":      string(trendSent),
		"TrendPending":   string(trendPending),
		"ActiveMenu":     "dashboard",
	})
}

// --- ENDPOINT KIRIM DATA ---
func (c *DashboardController) HandleSendToSAP(ctx *gin.Context) {
	session := sessions.Default(ctx)

	tokenStr := ""
	if token := session.Get("token"); token != nil {
		tokenStr = token.(string)
	}

	var requestBody struct {
		DocType string `json:"doc_type"`
		DocId   string `json:"doc_id"`
	}

	if err := ctx.ShouldBindJSON(&requestBody); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Format parameter tidak valid",
		})
		return
	}

	c.syncService.SendToSAPServiceLayer(requestBody.DocType, requestBody.DocId, tokenStr)

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Document triggered successfully",
	})
}

func (c *DashboardController) HandleBulkSendToSAP(ctx *gin.Context) {
	session := sessions.Default(ctx)
	tokenStr := ""
	if token := session.Get("token"); token != nil {
		tokenStr = token.(string)
	}

	ctx.Request.ParseForm()
	ids := ctx.PostFormArray("ids[]")
	for _, combined := range ids {
		parts := strings.SplitN(combined, "|", 2)
		if len(parts) != 2 {
			continue
		}
		c.syncService.SendToSAPServiceLayer(parts[0], parts[1], tokenStr)
	}

	redirectTo := ctx.PostForm("redirect_to")
	if redirectTo == "" {
		redirectTo = "/dashboard"
	}
	ctx.Redirect(http.StatusFound, redirectTo)
}

// --- TREE-VIEW ---
func (c *DashboardController) GetChildrenAPI(ctx *gin.Context) {
	parentNo := ctx.Param("parent_no")
	if parentNo == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Parent Number is required"})
		return
	}

	children, err := c.dashService.GetChildDocuments(parentNo)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   children,
	})
}

// --- PROXY / DIRECT AUDIT TRAIL API ---
func (c *DashboardController) GetAuditTrailAPI(ctx *gin.Context) {
	docType := ctx.Query("doc_type")
	docNo := ctx.Query("doc_no")

	// 1. Coba ambil riwayat audit trail langsung dari database PostgreSQL
	logs, err := c.dashService.GetAuditTrail(docType, docNo)
	if err == nil && len(logs) > 0 {
		ctx.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   logs,
		})
		return
	}

	// 2. Jika tidak ada dan BACKUP_API_URL dikonfigurasi, coba proxy ke engine eksternal
	apiURL := os.Getenv("BACKUP_API_URL")
	if apiURL != "" {
		session := sessions.Default(ctx)
		tokenStr := ""
		if token := session.Get("token"); token != nil {
			tokenStr = token.(string)
		}

		targetURL := apiURL + "/admin/integration-logs?document_type=" + docType + "&document_no=" + docNo
		req, err := http.NewRequest("GET", targetURL, nil)
		if err == nil {
			if tokenStr != "" {
				req.Header.Set("Authorization", "Bearer "+tokenStr)
			}
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				var jsonResp interface{}
				if err := json.Unmarshal(body, &jsonResp); err == nil {
					ctx.JSON(resp.StatusCode, jsonResp)
					return
				}
			}
		}
	}

	// 3. Fallback jika kosong
	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   logs,
	})
}
