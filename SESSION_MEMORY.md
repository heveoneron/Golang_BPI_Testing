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

### F. Enhancements Material Config & Master Data Vendor
- **Material Config Item Group**:
  - Menambahkan kolom finansial sesuai DJ-MM-ENH-ZME003 di tabel dan modal form `material_mapping.html`: `Inventory Posting (GL)`, `Goods Issue (GL)`, `Stock Adj (GL)`, dan `Cost Center`.
  - Menambahkan kolom tersebut pada model `MaterialConfigMapping` dan skema database `material_config_mappings`.
  - Menambahkan auto-fill preset untuk Valuation Class `S001`-`S011`.
- **Vendor Master Data (Outbound Master Data Vendor & Multiple Bank Accounts)**:
  - Memisahkan card Vendor dari menu `Finance Integration` Tab `Reimbursement` ke menu Master Data baru: `Vendor Master Data` (`/vendor-master`).
  - Menangani arsitektur **Multiple Bank Accounts** per vendor (1-to-N) dengan tabel relasi baru `vendor_bank_accounts` (FK ke `employee_vendor_mappings(vendor_code)` ON DELETE CASCADE).
  - Menampung **11 Field OData `to_BusinessPartnerBank`** sesuai mapping spesifikasi:
    1. `d:BankNumber` (Bank Key)
    2. `d:BankAccount` (Bank Account)
    3. `d:BankAccountHolderName` (Account Holder)
    4. `<d:BankIdentification>` (Bank ID)
    5. `<d:BankCountryKey>` (Bank Country)
    6. `<d:SWIFTCode>` (SWIFT Code)
    7. `<d:BankName>` (Bank Name)
    8. `<d:BankControlKey>` (Control Key)
    9. `<d:IBAN>` (IBAN)
    10. `<d:BankAccountReferenceText>` (Reference Details)
    11. `<d:BankAccountName>` (Account Name)
  - UI Web Panel `/vendor-master`:
    - **Tabel Utama Bersih & Rapi**: Menghapus `rowspan` yang rentan merusak layout/patah saat search & pagination. Tabel kembali ke 8 kolom proporsional: `No`, `Vendor Code`, `CIP / Non CIP`, `Email`, `Rekening Bank` (menampilkan ringkasan/badge jumlah rekening & bank utama), `Keterangan`, `Status`, dan `Aksi`.
    - **Modal Detail Rekening Bank (`#modalBankDetail`)**: Ketika tombol `Detail` diklik pada baris vendor, muncul modal pop-up `modal-xl` dengan header info vendor dan **tabel lengkap 11 kolom rekening bank** (Bank ID, Country, Bank Key, Account, SWIFT, Bank Name, Control Key, IBAN, Reference, Account Holder, Account Name) serta shortcut untuk langsung edit data.
    - **Modal Add/Edit Vendor**: Tetap mendukung input dan edit multi-rekening secara dinamis via repeater table `+ Tambah Rekening Bank`.
  - **Tombol Sync Data dari SAP (Manual Trigger)**: Disediakan tombol on-demand di header `/vendor-master` untuk menarik data langsung dari SAP OData `API_BUSINESS_PARTNER` (`A_BusinessPartnerAddress` $\rightarrow$ `to_EmailAddress` & `A_BusinessPartner` $\rightarrow$ `to_BusinessPartnerBank`) tanpa background scheduler otomatis, lengkap dengan feedback spinner dan toast notification. Sedia fallback sample mock vendors untuk lingkungan simulasi/testing.

### G. Finance RICEF (GL Line Item 3/4) Header & Detail Modal Table Redesign
- **Header Table 15 Kolom (Gambar 3)**:
  - Tampilan utama tab `GL Line Item (3/4)` di `/finance` diubah menampilkan 15 kolom dokumen akuntansi: `Company Code`, `Fiscal Year`, `Document Number`, `Transaction Currency`, `Fiscal Period`, `Posting Date`, `Document Date`, `Document Type`, `Document Header Text`, `Reference`, `Created By`, `Time Stamp SAP`, `Time Stamp Get in SAP`, `Flagging Send to Maximo`, `Time Stamp Send to Maximo`, dan tombol aksi `Detail SAP`.
  - Backend query PostgreSQL (`ListGLDocumentHeaders`) mengelompokkan data line item per dokumen akuntansi secara teragregasi.
- **Modal Line Item Detail 30 Kolom (Gambar 4)**:
  - Menggantikan tampilan *raw JSON code dump* (Gambar 2) dengan modal **Tabel Line Items** berisi **30 kolom penuh** sesuai Gambar 4:
    `Company Code`, `Fiscal Year`, `Document Number`, `Line Item`, `Company Code Currency`, `Global Currency`, `UOM`, `Quantity`, `Product`, `G/L Account`, `Cost Center`, `Profit Center`, `Functional Area`, `Amount in Transaction Currency`, `Amount in Company Code Currency`, `Amount in Global Currency`, `Debit/Credit Ind.` (Badge S/H), `Assignment`, `Reversing Document`, `Reversing Document Item`, `Text`, `Supplier`, `Customer`, `Financial Account Type`, `Special G/L Code`, `House Bank`, `Bank Account ID`, `Asset`, `Sub-number`, `WHT Code`.
  - Klik tombol `Detail SAP` langsung mengekstrak seluruh line item dokumen dari database / payload OData dan menampilkannya dalam modal tabel interaktif ber-badge debit/kredit.

### H. Centralized SAP Base URL & System Configuration Seeding
- **Sentralisasi Base URL SAP (`/sap-endpoints`)**:
  - Menambahkan card **Global SAP Base URL (Host Server)** di bagian atas halaman `/sap-endpoints`.
  - Admin dapat mengubah Base URL pusat (misal berpindah server ke `https://my446174-api.s4hana.cloud.sap` atau `MOCK`) dan dengan sekali klik tombol **"Terapkan ke Semua Layanan"**, seluruh 11 endpoint layanan SAP otomatis diperbarui sekaligus.
  - Modal form Tambah/Edit Layanan SAP otomatis menggunakan default Global Base URL, dilengkapi tombol pintas *"Gunakan Global Base URL"*.
  - Sinkronisasi dua arah: Perubahan `sap_base_url` di menu System (`/config`) otomatis menyelaraskan seluruh endpoint di `sap_endpoints`, dan sebaliknya.
- **Aktivasi & Seeding Menu System (`/config`)**:
  - Memperbaiki ketidaksesuaian nama tabel model GORM `app_config.go` (`configs`).
  - Menambahkan auto-seeding data default ke tabel `configs` saat aplikasi pertama kali start atau saat tabel masih kosong:
    1. `bot_base_url` (`http://localhost:8085/api/v1` - Ingestion engine / poller)
    2. `bpa_base_url` (`https://spa-api.eu10.process-automation.sap.biz` - SAP BPA Trigger API)
    3. `sap_base_url` (`https://my445160-api.s4hana.cloud.sap` - Host sentral SAP Cloud)
    4. `sap_username` (`CU_SAP_COM_0053` - Default Communication User)
    5. `sap_password` (`••••••••••••` - Default Communication Password)
    6. `erp_callback_url` (`http://host.docker.internal:1433/maximo/api/webhook/status` - Maximo webhook)
    7. `prefix_bot_mapping` (`{"45": "BPA_PO_NORMAL_TRIGGER", "47": "BPA_PO_CONTRACT_TRIGGER"}`)
    8. `doc_type_bot_mapping` (`{"NB": "WF_STANDARD_PO", "ZPRC": "WF_PROJECT_PO", "ZCTR": "WF_CONTRACT_PO"}`)
  - Seluruh section pada halaman `System` (Global Endpoints, SAP Direct Connection & ERP Callback, Prefix Bot Mapping) kini terisi lengkap dan dapat diedit secara realtime dari Web Panel tanpa perlu restart Docker.

---

### I. Penyelarasan Item / Product Master & Chained API `A_ProductProcurement`
1. **Aturan IssueUnit vs OrderUnit (Excel Mapping Row 6 & Gambar 1-3)**:
   - **Jika `IssueUnit == OrderUnit` (satuan sama, misal sama-sama `SET` atau `AU` atau `EA`)**:
     - `PurchaseOrderQuantityUnit` di-set `""` (BLANK di header payload `A_Product`).
     - `to_ProductUnitsOfMeasure` dikosongkan (`nil` / tidak dikirim) untuk mencegah SAP Error `M3/332`: *"The alternative unit of measure is the same as the base unit of measure"*.
     - Sub-request chained API `A_ProductProcurement` **tetap dikirimkan** dengan payload:
       ```json
       {
         "Product": "<ProductID>",
         "PurchaseOrderQuantityUnit": "",
         "VarblPurOrdUnitStatus": "1"
       }
       ```
   - **Jika `IssueUnit != OrderUnit` (satuan berbeda, misal `BaseUnit = "PC"`, `OrderUnit = "BAG"`)**:
     - `PurchaseOrderQuantityUnit` diisi dengan `poUnit` (dari `ORDERUNIT`).
     - `to_ProductUnitsOfMeasure` dikirim dengan rasio 1:1 (`AlternativeUnit = poUnit`, `BaseUnit = baseUnit`, `QuantityNumerator = "1"`, `QuantityDenominator = "1"`).
     - Sub-request chained API `A_ProductProcurement` **tetap dikirimkan** dengan payload:
       ```json
       {
         "Product": "<ProductID>",
         "PurchaseOrderQuantityUnit": "<OrderUnit>",
         "VarblPurOrdUnitStatus": "1"
       }
       ```
2. **Penyelarasan Field Lainnya**:
   - **Language Field**: Dikembalikan seperti semula dengan nilai default `"EN"` pada `to_Description.results[]`.
   - **Plant & Valuation Area**: Nilai default `PRODUCT_PLANTS` diselaraskan menjadi `"1000,2000"`. Auto-sync di `EnsureDefaultValues`, prioritas `SAP_PRODUCT_PLANTS` di `Default(ctx, key)`, dan query migrasi di `finance.sql` memastikan nilai legacy `5110,BTNG` otomatis digantikan dengan `1000,2000` tanpa perlu reset manual.
   - **PurchasingGroup di to_Plant**: Menambahkan field `"PurchasingGroup": "001"` (hardcode default `"001"` melalui `PRODUCT_PURCHASING_GROUP`) pada setiap baris item `to_Plant.results[]`.
   - **Description Length**: Batas panjang pemotongan deskripsi produk (`ProductDescription`) diset maksimal **40 karakter** (`truncateRunes(description, 40)`), sesuai batasan validasi ketat field `MAKTX` di SAP S/4HANA OData v2 (`API_PRODUCT_SRV/A_ProductDescription`) untuk mencegah error *"Property 'ProductDescription' has invalid value"*.
   - **Unit Testing**: Unit test `TestSyncService_ItemMaster_AmpersandToN` dan `TestSyncService_ItemMaster_SameUnit_PurchaseOrderQuantityUnit` diperbarui $\rightarrow$ 100% PASS.

### J. Perbaikan Persistensi SAP Integration & Global Base URL ke PostgreSQL (Anti-Reset)
1. **Penyebab Data Tereset**:
   - Pada `middleware-panel/main.go`, setiap aplikasi start atau restart (`matin program terus dinyalain lagi`), kode lama menjalankan `sapRepo.Save(...)` untuk PO, BP, PRODUCT, GRN, dan CANCEL_GR secara hardcoded tanpa pengecekan apakah data sudah ada.
   - Karena fungsi `Save()` melakukan `UPDATE` jika record ditemukan, seluruh perubahan Base URL, username, scenario ID, atau endpoint path yang sudah disimpan oleh user di Postgres langsung tertimpa kembali ke nilai default `https://my445160-api.s4hana.cloud.sap`.
2. **Implementasi Perbaikan**:
   - **Non-destructive Seeding (`SeedDefaults`)**:
     - Menambahkan method `SeedDefaults()` pada `sapEndpointRepo` yang memeriksa `count == 0` per `service_code`. Jika record sudah ada di database Postgres, sistem **TIDAK AKAN** menimpanya sehingga data custom user selalu aman dan persisten saat restart program.
     - Memperbarui seeding tabel `configs` di `main.go` per key agar key penting (`sap_base_url`, `bot_base_url`, dll) otomatis terisi jika belum ada tanpa menimpa konfigurasi yang sudah diubah user.
   - **Direct SQL Update & Upsert Persistensi**:
     - `UpdateGlobalBaseURL()` di `sap_endpoint_repo.go` kini menjalankan `UPDATE sap_endpoints SET base_url = ?, updated_at = NOW()` secara langsung pada database Postgres dan melakukan upsert ke tabel `configs` key `sap_base_url`.
     - `Save()` pada `sap_endpoint_repo.go` langsung mengeksekusi update berbasis `UPPER(service_code)` dengan mempertahankan password dan CSRF path jika input form kosong.
   - **Peningkatan UX UI Panel (`sap_endpoints.html`)**:
     - Menambahkan loading state (spinner & disable tombol submit) pada form simpan endpoint dan penerapan global base URL.
     - Menutup modal Bootstrap sebelum reload halaman agar transisi halus dan sinkron dengan Postgres.
   - **Unit Testing**:
     - Menambahkan test suite `TestHandleSaveEndpoint_Success` dan `TestHandleUpdateGlobalBaseURL_Success` di `middleware-panel/controllers/sap_endpoint_controller_test.go` $\rightarrow$ 100% PASS.

---

### K. RICEF 7: Reimbursement & Business Trip Supplier Invoice Integration
1. **Analisis Template Excel Talenta**:
   - **Template 1 (`Form_Template_Reimbursement_1008.xlsx`)**: Berisi klaim reimbursement karyawan (Medical Claim Inpatient/Outpatient, Glasses, dll). Kolom kunci: `Transaction Id`, `Employee ID`, `Full Name`, `Reimbursement Policy Name`, `Reimbursement Notes`, `Date`, `Approved Amount`, `Reimbursement Status` (`Approved`). Baris referensi/appendiks `BPI_CIP` di baris bawah dihentikan otomatis saat parsing (`break`).
   - **Template 2 (`Form_Template_Bussiness_Trip_BT1008.xlsx`)**: Berisi klaim perjalanan dinas bisnis. Kolom kunci: `Trip ID`, `Requested By` (format: `NAMA - KODE_VENDOR`, contoh: `BAMBANG WIDJAJANTO - 22019`), `Trip Type` (Domestic/International), `Destination`, `Approval Request Date`, `Approved Expense Amount`, `Status` (`Awaiting Disbursement`).
2. **Penyelarasan Staging Database PostgreSQL (`reimbursement_requests`)**:
   - Membuat tabel staging terpadu `reimbursement_requests` di PostgreSQL (`migrations/finance.sql` & `schema.sql`) untuk menampung kedua format file secara bersamaan.
   - Kolom kunci: `doc_category` (`REIMBURSEMENT` / `BUSINESS_TRIP`), `transaction_or_trip_id`, `employee_id`, `employee_name`, `policy_or_trip_type`, `benefit_or_destination`, `item_text`, `document_date`, `posting_date`, `gross_amount`, `currency`, `company_code`, `doc_type` (`ZB`), `payment_terms` (`Z014`), `gl_account`, `cost_center`, `tax_code`, `status`, `sap_invoice_number`, `sap_fiscal_year`, `message`.
3. **Normalisasi & Mapping JSON OData v2 SAP `A_SupplierInvoice`**:
   - **Header**: `CompanyCode: "1000"`, `AccountingDocumentType: "ZB"`, `PaymentTerms: "Z014"`, `SupplierInvoiceStatus: "A"`, `SupplierInvoiceIDByInvcgParty` & `InvoicingParty` (diisi `Employee ID` untuk Reimburse, atau Vendor ID hasil ekstraksi `Requested By` untuk BT), `InvoiceGrossAmount`, `DocumentHeaderText: TransactionID / TripID`, `TaxIsCalculatedAutomatically: false`.
   - **Line Item (`to_SupplierInvoiceItemGLAcct`)**: `DebitCreditCode: "S"`, `TaxCode: "V0"`, `GLAccount` (MainAccountId), `CostCenter` (BudgetCode), `SupplierInvoiceItemText` (Notes / Destination), `AssignmentReference` (Policy / Destination).
4. **Enrichment Frontend UI Web Panel (`finance.html`)**:
   - Memperkaya tabel `#tblInvoice` pada Tab Reimbursement dengan 11 kolom informatif: `Tipe`, `Ref / Trans ID`, `Karyawan / Vendor`, `Kebijakan / Trip`, `Keterangan / Tujuan`, `Tanggal`, `GL / Cost Center`, `Gross (IDR)`, `Status`, `No. SAP`, `Pesan / Info`.
5. **Kredensial & Pengujian Live API SAP S/4HANA**:
   - Service: `SUPPLIER_INVOICE`
   - Base URL: `https://my446666-api.s4hana.cloud.sap`
   - Endpoint Path: `/sap/opu/odata/sap/API_SUPPLIERINVOICE_PROCESS_SRV/A_SupplierInvoice`
   - User: `Y_BPI_CU_SAP_COM_0057` / `PowerofOne@123456789`
   - Testing handshake CSRF fetch & OData deep insert terkonfirmasi valid (`200 OK`).

### L. Fix Material Config AutoMigrate & Valuation Class Auto-Seed
1. **Root Cause Error SQLSTATE 42703**:
   - Error `column "inventory_posting_gl" of relation "material_config_mappings" does not exist` terjadi karena model `MaterialConfigMapping` belum didaftarkan pada `config.DB.AutoMigrate` di `middleware-panel/main.go`.
   - Akibatnya, 4 kolom financial (`inventory_posting_gl`, `goods_issue_gl`, `stock_adj_gl`, `cost_center`) belum terbentuk di PostgreSQL user saat panel pertama kali dijalankan.
2. **Perbaikan & Self-Healing Database**:
   - Mendaftarkan `&models.MaterialConfigMapping{}`, `&models.VendorMaster{}`, `&models.VendorBankAccount{}`, dan `&models.DefaultValue{}` ke `config.DB.AutoMigrate` di `middleware-panel/main.go`.
   - Menambahkan DDL auto-healing (`ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS ...`) dan auto-seed 7 Valuation Class (S001-S011) di `NewMaterialConfigMappingRepository` serta retry logic di `Save`.
   - Menambahkan skrip DDL & update migrasi idempotent di `middleware/internal/repository/migrations/finance.sql`.
3. **Verifikasi Data 7 Valuation Class (DJ-MM-ENH-ZME003)**:
   - Seluruh 7 Valuation Class (`S001`, `S002`, `S003`, `S004`, `S006`, `S009`, `S011`) telah lengkap dan cocok 100% dengan dokumen spesifikasi dan tabel user:
     - S001: Inventory 114001, GI 512033, Stock Adj 512033, CC 71098
     - S002: Inventory 114004, GI 512030, Stock Adj 512030, CC 71099
     - S003: Inventory 114009, GI 512031, Stock Adj 512031, CC 71099
     - S004: Inventory 114012, GI 512032, Stock Adj 512032, CC 71099
     - S006: Inventory 114006, GI 512034, Stock Adj 512034, CC 71099
     - S009: Inventory 114017, GI 512028, Stock Adj 512028, CC 71099
     - S011: Inventory 114016, GI 512023, Stock Adj 512023, CC 71099

### M. Pembaruan Spesifikasi Payload OData v4 Purchase Order (Catatan PO 1 - 8)
1. **PurchaseOrderType (Catatan 1)**:
   - Dihardcode menjadi `"ZBPI"`. Aturan lama yang mengubah prefix `45` ke `ZPRC` dan `47` ke `ZCTR` telah dinonaktifkan.
2. **CompanyCode (Catatan 2)**:
   - Dihardcode menjadi `"1000"` (default value `PO_COMPANY_CODE` diselaraskan ke `1000`).
3. **PurchasingOrganization (Catatan 3)**:
   - Dihardcode / default diset ke `"BPI"` (default value `PO_PURCHASING_ORG` diselaraskan ke `BPI`).
4. **Header Language (Catatan 4)**:
   - Field `Language` pada header `SAPGrowPOPayload` di-take out dari output JSON (menggunakan tag `json:"Language,omitempty"` dan dibiarkan kosong, sementara `Language: "EN"` pada catatan line item `_PurchaseOrderItemNote` tetap dipertahankan).
5. **Item Plant Mapping (Catatan 5)**:
   - Nilai Plant pada `_PurchaseOrderItem.Plant` otomatis dikonversi:
     - Jika `SITEID` = `"JKT"` $\rightarrow$ `"1000"`
     - Jika `SITEID` = `"BTNG"` $\rightarrow$ `"2000"`
     - Fallback default value `PO_DEFAULT_PLANT` diselaraskan ke `"1000"`.
6. **Deskripsi Item & Note (Catatan 6)**:
   - Membaca deskripsi dari `pol_desc_ld` (atau `DESCRIPTION` bila kosong, fallback `remark_ld`).
   - Perbaikan ekstraksi SQL Server (`extractPO` di `sqlserver.go`): Menambahkan kolom `pol_desc_ld` dan `remark_ld` ke dalam query `SELECT` dari tabel `axmxpo` dengan proteksi `COL_LENGTH`. Sebelumnya kolom ini belum disertakan dalam SELECT query sehingga pembacaan `pol_desc_ld` kosong dan jatuh ke fallback `itemnum`.
   - `_PurchaseOrderItem.PurchaseOrderItemText`: dipotong maksimal 40 karakter menggunakan `truncateRunes(fullDesc, 40)`.
   - `_PurchaseOrderItem._PurchaseOrderItemNote.PlainLongText`: menampung deskripsi lengkap (full desc) tanpa dipotong.
7. **Pricing Elements (Catatan 7)**:
   - Menghapus elemen `{ "ConditionType": "PMP0", "ConditionRateAmount": ... }` dari `_PurOrdPricingElement`.
   - Bila terdapat pajak (`TAXCODE1`), `_PurOrdPricingElement` kini **hanya** mengirimkan satu elemen pajak (`ConditionType: "DRG1"`, `ConditionRateRatioUnit: "%"`).
8. **CostCenter Account Assignment (Catatan 8)**:
   - `_PurchaseOrderItem._PurOrdAccountAssignment.CostCenter` diambil dari `BPIBUDGETCODE` (dengan fallback ke `BUDGETCODE` / `DEPARTMENT` / default cost center).

---

## 4. Status Verifikasi & Testing

- **Middleware Core Tests**:
  - `cd middleware; go test ./...` $\rightarrow$ **100% PASS** (`client`, `controller`, `repository`, `routes`, `service`).
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
