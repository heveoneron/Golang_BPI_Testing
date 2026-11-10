# SESSION_MEMORY: BPI Middleware & Panel Integration

Dokumen ini adalah ringkasan status, arsitektur, progres implementasi terakhir, dan rencana kerja berikutnya untuk integrasi IBM Maximo ERP $\leftrightarrow$ SAP S/4HANA Grow.

---

## 1. Arsitektur & Spesifikasi Sistem

- **Middleware Core** (`middleware/` - Port `:8085`):
  - Ingestion API: Menampung data PO, GRN, Material/Item Master, Vendor BP, dan Invoice dari Maximo / DB Staging.
  - Transformation Layer: Mengonversi data Maximo ke format JSON payload SAP S/4HANA Grow (OData v2 & v4).
  - Validation Layer: Menjalankan pre-check (seperti pengecekan Material Document sebelum PATCH PO).
  - SAP Client Dispatcher: Menangani autentikasi Basic Auth, CSRF token fetch (`x-csrf-token: fetch`), chained calls (`A_ProductProcurement`), SOAP Journal Entry bulk create, dan dispatch ke SAP endpoint.
  - Storage: PostgreSQL `localhost:5432` (`bpi_middleware_db`, user: `postgres`).

- **Middleware Panel** (`middleware-panel/` - Port `:8091`):
  - Framework: Go Gin Engine + HTML Template (Tailwind & Bootstrap UI).
  - Fitur:
    - Tracking Data (Monitoring status kirim, payload viewer, JSON compare, retry mechanism).
    - Master Data Mapping:
      - UOM Mapping (Maximo $\leftrightarrow$ SAP standard unit).
      - Material Config (ITEMGROUPID $\rightarrow$ ValuationClass, StockType $\rightarrow$ ProductGroup, ItemType $\rightarrow$ ProductType).
      - Valuation Class & GL Mapping (DJ-MM-ENH-ZME003: S001-S011 $\rightarrow$ Inventory, GI, Stock Adj, Cost Center).
      - Recon Account & BP Grouping.
      - Exchange Rate.
    - Scheduler & System Configuration.
    - Audit Trail & User Access.

---

## 2. Kredensial & Endpoint SAP S/4HANA Grow

| Service | Protocol / Ver | Endpoint URL | User ID | Password |
|---|---|---|---|---|
| **Purchase Order (PO)** | OData v4 | `/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder` | `CU_SAP_COM_0053` | `PowerofOne@123456789` |
| **Material Doc / GRN / Return** | OData v2 | `/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader` | `Y_BPI_CU_SAP_COM_0108` | `PowerofOne@123456789` |
| **Product / Material Master** | OData v2 | `/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product` | `CU_SAP_COM_0009` | `PowerofOne@123456789` |
| **Product Procurement (Var PO Unit)** | OData v2 | `/sap/opu/odata/sap/API_PRODUCT_SRV/A_ProductProcurement` | `CU_SAP_COM_0009` | `PowerofOne@123456789` |
| **Business Partner / Vendor** | OData v2 | `/sap/opu/odata/sap/API_BUSINESS_PARTNER` | `CU_SAP_COM_0008` | `PowerofOne@123456789` |
| **G/L Account Line Items** | OData v2 | `/sap/opu/odata/sap/API_GLACCOUNTLINEITEM_SRV` | `Y_BPI_CU_SAP_COM_0303` | `PowerofOne@123456789` |
| **Manual Journal Entry** | SOAP 1.1 / 1.2 | `/sap/bc/srt/scs_ext/sap/journalentrycreaterequestconfi` | `CB9980000003` / env | (via Basic Auth) |

---

## 3. Progres Implementasi Terakhir

### A. Aturan Konversi Nomor PO (`ConvertMaximoPOToSAP`)
- **Prefix 45**: Hapus 1 digit angka `0` setelah `"45"` (contoh: `4500005678` $\rightarrow$ `450005678`).
- **Prefix 47**: Hapus 4 digit angka `0` setelah `"47"`, ganti `"-"` jadi `"0"`, dan jika digit setelah strip hanya 1 digit, tambahkan leading zero `"0"` (contoh: `4700000012-23` $\rightarrow$ `470012023`, `4700000123-1` $\rightarrow$ `470123001`).
- **Konsistensi Lintas Modul**:
  - Di **PO Header**: Field `PurchaseOrder` diisi nomor SAP hasil konversi, sedangkan nomor PO Maximo asli selalu dicatat di `SupplierRespSalesPersonName`.
  - Di **GRN Line Item**: Field `to_MaterialDocumentItem.PurchaseOrder` otomatis dikonversi dengan fungsi yang sama.

### B. Purchase Order (PO) Transformation (Type 1 – Type 4)
- **Tipe PO yang didukung**:
  1. *Type 1 (Normal PO)*: Standard PO (NB / ZPRC / ZCTR / ZBPI). Prefix `45*` $\rightarrow$ `ZPRC`, Prefix `47*` $\rightarrow$ `ZCTR`.
  2. *Type 2 (PO with Tax)*: Tax code terisi, memuat `_PurOrdPricingElement` dengan `DRG1` (tax percentage unit `%`) dan `PMP0` (unit cost). Item `NetPriceAmount` diambil dari `UnitCost`.
  3. *Type 3 (PO with Down Payment)*: Mengisi Header `DownPaymentType: "V"`, `DownPaymentPercentageOfTotAmt`, dan `DownPaymentDueDate`.
  4. *Type 4 (PO with Tax & Down Payment)*: Kombinasi Tax dan DP.
- **Aturan Pricing Element & Tax**:
  - Jika ada Tax (`TAXCODE1` terisi): Buat pricing element `DRG1` + `PMP0`, `NetPriceAmount` = `UnitCost`.
  - Jika **tidak ada Tax**: `_PurOrdPricingElement` di-omit (`nil`), `TaxCode` di-set `"V0"`, `NetPriceAmount` = `UnitCost`.
- **Text Formatting**: Text item dipotong $\le 40$ karakter untuk `PurchaseOrderItemText`, dan deskripsi lengkap disimpan di `_PurchaseOrderItemNote[].PlainLongText` (TextObjectType: `"F01"`).

### C. Validasi PO Change (PATCH PO)
- Sebelum mengirim update PO ke SAP via `PATCH`, middleware melakukan verifikasi pergerakan material di SAP:
  $$\text{Hasil} = \sum \text{Qty}(101) - \sum \text{Qty}(102) - \sum \text{Qty}(122)$$
- Jika $\text{Hasil} > 0$: PO diblokir untuk di-update, status diset `error_data` (GRN aktif).
- Jika $\text{Hasil} \le 0$ atau tidak ada dokumen GR: Perubahan PO diizinkan dan dikirim via `PATCH`.

### D. Item Master: Sanitasi Karakter & Chaining `A_ProductProcurement`
- **Sanitasi Item Code**: Karakter `&` pada `ITEMNUM` Maximo otomatis diubah menjadi `N` (contoh: `SP&001` $\rightarrow$ `SPN001`).
- **Variable PO Unit Chaining**: Setelah dokumen `A_Product` berhasil dibuat di SAP, jika terdapat `PurchaseOrderQuantityUnit`, middleware otomatis memicu sub-request ke `POST /A_ProductProcurement` dengan `VarblPurOrdUnitStatus = "1"` agar material dapat dipesan dengan satuan pembelian fleksibel di PO.

### E. GRN Dates & SiteID Mapping
- **Format Tanggal OData v2**: Field `DocumentDate` dan `PostingDate` dikirim dalam format `/Date(ms)/`.
- **Proteksi Backdate**: Jika `ActualDate` berada di masa lalu, `PostingDate` otomatis diset ke tanggal hari ini (`today`) untuk mencegah kegagalan posting period di SAP.
- **SiteID $\rightarrow$ Plant Mapping**: `JKT` dikonversi ke `1000`, `BTNG` ke `2000` (fallback `5110`).
- **InvtryMgmtReferenceDocument**: Pada return 122, referensi material document SAP diambil otomatis via nomor Maximo lookup.

### F. Vendor / Business Partner Mapping
- Pemecahan nama vendor `OrganizationBPName1..4` berbasis kata (word wrap) max 40 karakter per slot.
- Smart default fallback untuk Grouping dan Recon Account:
  - `01` $\rightarrow$ `Z001` / `211001`
  - `02` $\rightarrow$ `Z002` / `211003`
  - `03` $\rightarrow$ `Z003` / `211005`
  - `04` $\rightarrow$ `Z004` / `211006`

### G. Determinasi Akun G/L & Cost Center Jurnal Entry (DJ-MM-ENH-ZME003)
- **Matriks Pemetaan**: Tabel `valuation_class_gl_mappings` di PostgreSQL beserta in-memory fallback untuk 7 Valuation Class (`S001` - `S011`):
  - `S001` $\rightarrow$ Inventory `114001`, GI `512033`, Stock Adj `512033`, CC `71098`
  - `S002` $\rightarrow$ Inventory `114004`, GI `512030`, Stock Adj `512030`, CC `71099`
  - `S003` $\rightarrow$ Inventory `114009`, GI `512031`, Stock Adj `512031`, CC `71099`
  - `S004` $\rightarrow$ Inventory `114012`, GI `512032`, Stock Adj `512032`, CC `71099`
  - `S006` $\rightarrow$ Inventory `114006`, GI `512034`, Stock Adj `512034`, CC `71099`
  - `S009` $\rightarrow$ Inventory `114017`, GI `512028`, Stock Adj `512028`, CC `71099`
  - `S011` $\rightarrow$ Inventory `114016`, GI `512023`, Stock Adj `512023`, CC `71099`
- **OData Valuation Class Fetcher**: Method `GetProductValuationClass` di `sap_client.go` untuk query ke `/A_Product('<ItemNum>')/to_Valuation` jika belum ada di staging.
- **Engine Determinasi Otomatis** (`DetermineJournalEntryFromGIOrAdj`):
  - **Goods Issue**: DocType `WL`, Header `Goods Issue`, Baris 1 Inventory `H` (tanpa Cost Center), Baris 2 GI `S` (dengan Cost Center).
  - **Stock Adjustment Positif**: DocType `WA`, Header `Stock Adjustment`, Baris 1 Inventory `S` (tanpa Cost Center), Baris 2 Stock Adj `H` (dengan Cost Center).
  - **Stock Adjustment Negatif**: DocType `WA`, Header `Stock Adjustment`, Baris 1 Inventory `H` (tanpa Cost Center), Baris 2 Stock Adj `S` (dengan Cost Center).
  - **Kaidah Akuntansi**: Baris akun `Inventory Posting` **TIDAK PERNAH** ditempel Cost Center (invariant verified).
- **API Endpoints**:
  - `POST /api/v1/finance/goods-issue-adjustment`: Ingestion transaksi GI / Stock Adj Maximo, determinasi G/L otomatis, validasi pagu, dan antre SOAP.
  - `GET /api/v1/finance/valuation-class-mappings` & `POST ...`: Pengelolaan matriks mapping.

### E. Standardisasi Docker & Linux Deployment (Production Ready)
- **Multi-Stage Dockerfiles**:
  - `middleware/Dockerfile` & `middleware-panel/Dockerfile` menggunakan `golang:alpine` builder (kompatibel Go 1.24/1.25+ sesuai requirement dependensi `go-playground/validator/v10` & `golang.org/x/net`) dengan `CGO_ENABLED=0 GOOS=linux`.
  - Menggunakan `-mod=vendor` agar proses compile di server Linux tidak membutuhkan koneksi internet untuk download package (100% offline & fast build).
  - Runtime container minimal `alpine:3.20` dengan `ca-certificates` (wajib untuk TLS/HTTPS ke SAP S/4HANA Cloud), `tzdata` (Asia/Jakarta), dan `curl`.
  - Binary, templates (`templates/`), dan static assets (`static/`) disalin rapi ke runner container.
- **Docker Compose Stack** (`docker-compose.yml` & `docker.yml`):
  - Service `postgres:16-alpine` dengan healthcheck otomatis (`pg_isready`), volume persistent `bpi_postgres_data`.
  - Mounting otomatis inisialisasi schema: `./middleware/database/schema.sql` (`01_schema.sql`) dan `./middleware/internal/repository/migrations/finance.sql` (`02_finance.sql`) ke `/docker-entrypoint-initdb.d/`.
  - Penambahan file `.env.example` untuk memudahkan konfigurasi dinamis server (DB, SAP credentials, Maximo SQL Server host, port).
  - Penambahan `host.docker.internal:host-gateway` pada `extra_hosts` agar container dapat mengakses Maximo SQL Server yang berjalan di host.
- **Swagger Dynamic Host & CORS Fix**:
  - `middleware/internal/routes/router.go` ditambahkan middleware CORS (`Access-Control-Allow-Origin: *`, preflight OPTIONS support).
  - `docs.SwaggerInfo.Host = ""` dan Swagger spec diset dinamis (empty string) agar Swagger UI di browser laptop memanggil IP server (`192.168.214.210:8085`) bukan hardcoded `localhost:8085`.
  - Link Swagger di web panel (`sidebar.html` & `exchange_rates.html`) disesuaikan agar otomatis mengarah ke IP server yang sedang diakses.

---

## 4. Status Verifikasi & Testing

- **Middleware Core Tests**:
  - `cd middleware; go test ./...` $\rightarrow$ **100% PASS** (`client`, `controller`, `repository`, `routes`, `service`).
  - Unit test baru mencakup:
    - `TestDetermineJournalEntry_GoodsIssue`
    - `TestDetermineJournalEntry_StockAdjustment_Positive`
    - `TestDetermineJournalEntry_StockAdjustment_Negative`
    - `TestDetermineJournalEntry_AllValuationClasses_Invariant`
    - `TestSyncService_ConvertMaximoPOToSAP`
    - `TestSyncService_ItemMaster_AmpersandToN`
    - `TestSyncService_GRN_DatesAndSiteID`
- **Middleware Panel Tests**:
  - `cd middleware-panel; go test ./...` $\rightarrow$ **100% PASS** (`main`, `controllers`, `repository`).

---

## 5. Rencana & Task Selanjutnya

1. **Live Simulation & End-to-End Test**:
   - Simulasi posting transaksi Goods Issue (DocType `WL`) via endpoint `/api/v1/finance/goods-issue-adjustment`.
   - Simulasi posting transaksi Stock Adjustment surplus dan defisit (DocType `WA`).
   - Verifikasi log integrasi SOAP di tabel `integration_logs`.
2. **Monitoring & Dispatching**:
   - Memastikan scheduler SOAP berjalan normal dan memantau antrean dokumen di Web Panel.

---
*Generated automatically by Antigravity Session Memory System.*
