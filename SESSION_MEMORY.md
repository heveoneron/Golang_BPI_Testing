# SESSION_MEMORY: BPI Middleware & Panel Integration

Dokumen ini adalah ringkasan status, arsitektur, progres implementasi terakhir, dan rencana kerja berikutnya untuk integrasi IBM Maximo ERP $\leftrightarrow$ SAP S/4HANA Grow.

---

## 1. Arsitektur & Spesifikasi Sistem

- **Middleware Core** (`middleware/` - Port `:8085`):
  - Ingestion API: Menampung data PO, GRN, Material/Item Master, Vendor BP, dan Invoice dari Maximo / DB Staging.
  - Transformation Layer: Mengonversi data Maximo ke format JSON payload SAP S/4HANA Grow (OData v2 & v4).
  - Validation Layer: Menjalankan pre-check (seperti pengecekan Material Document sebelum PATCH PO).
  - SAP Client Dispatcher: Menangani autentikasi Basic Auth, CSRF token fetch (`x-csrf-token: fetch`), dan dispatch ke SAP endpoint.
  - Storage: PostgreSQL `localhost:5432` (`bpi_middleware_db`, user: `postgres`).

- **Middleware Panel** (`middleware-panel/` - Port `:8091`):
  - Framework: Go Gin Engine + HTML Template (Tailwind & Bootstrap UI).
  - Fitur:
    - Tracking Data (Monitoring status kirim, payload viewer, JSON compare, retry mechanism).
    - Master Data Mapping:
      - UOM Mapping (Maximo $\leftrightarrow$ SAP standard unit).
      - Material Config (ITEMGROUPID $\rightarrow$ ValuationClass, StockType $\rightarrow$ ProductGroup, ItemType $\rightarrow$ ProductType).
      - Recon Account & BP Grouping.
      - Exchange Rate.
    - Scheduler & System Configuration.
    - Audit Trail & User Access.

---

## 2. Kredensial & Endpoint SAP S/4HANA Grow

| Service | OData Ver | Endpoint URL | User ID | Password |
|---|---|---|---|---|
| **Purchase Order (PO)** | OData v4 | `/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder` | `CU_SAP_COM_0053` | `PowerofOne@123456789` |
| **Material Doc / GRN / Return** | OData v2 | `/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader` | `Y_BPI_CU_SAP_COM_0108` | `PowerofOne@123456789` |
| **Product / Material Master** | OData v2 | `/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product` | `CU_SAP_COM_0009` | `PowerofOne@123456789` |
| **Business Partner / Vendor** | OData v2 | `/sap/opu/odata/sap/API_BUSINESS_PARTNER` | `CU_SAP_COM_0008` | `PowerofOne@123456789` |
| **G/L Account Line Items** | OData v2 | `/sap/opu/odata/sap/API_GLACCOUNTLINEITEM_SRV` | `Y_BPI_CU_SAP_COM_0303` | `PowerofOne@123456789` |

---

## 3. Progres Implementasi Terakhir

### A. Purchase Order (PO) Transformation (Type 1 – Type 4)
- **Tipe PO yang didukung**:
  1. *Type 1 (Normal PO)*: Standard PO (NB / ZPRC / ZCTR / ZBPI). Prefix `45*` $\rightarrow$ `ZPRC`, Prefix `47*` $\rightarrow$ `ZCTR`.
  2. *Type 2 (PO with Tax)*: Tax code terisi, memuat `_PurOrdPricingElement` dengan `DRG1` (tax percentage unit `%`) dan `PMP0` (unit cost). Item `NetPriceAmount` diambil dari `UnitCost`.
  3. *Type 3 (PO with Down Payment)*: Mengisi Header `DownPaymentType: "V"`, `DownPaymentPercentageOfTotAmt`, dan `DownPaymentDueDate`.
  4. *Type 4 (PO with Tax & Down Payment)*: Kombinasi Tax dan DP. Payload contoh sudah ditambahkan ke [Task_Project.md](file:///c:/Users/muham/Downloads/Middleware_SLI%20(30-09-26)/BPI_Testing/Golang_BPI_Testing/Task_Project.md).
- **Aturan Pricing Element & Tax**:
  - Jika ada Tax (`TAXCODE1` terisi): Buat pricing element `DRG1` + `PMP0`, `NetPriceAmount` = `UnitCost`.
  - Jika **tidak ada Tax**: `_PurOrdPricingElement` di-omit (`nil`), `TaxCode` di-set `"V0"`, `NetPriceAmount` = `UnitCost`.
- **Contract & Text Formatting**:
  - Jika nomor PO mengandung `-` (contoh: `4700000012-63`), nomor kontrak diisi ke `SupplierRespSalesPersonName`.
  - Text item dipotong $\le 40$ karakter untuk `PurchaseOrderItemText`, dan deskripsi lengkap disimpan di `_PurchaseOrderItemNote[].PlainLongText` (TextObjectType: `"F01"`).
- **Status Dokumen**: Checkbox [x] pada baris 305 di [Task_Project.md](file:///c:/Users/muham/Downloads/Middleware_SLI%20(30-09-26)/BPI_Testing/Golang_BPI_Testing/Task_Project.md) sudah dicentang.

### B. Validasi PO Change (PATCH PO)
- Sebelum mengirim update PO ke SAP via `PATCH`, middleware melakukan verifikasi pergerakan material di SAP:
  $$\text{GET } \text{/sap/opu/odata/sap/API\_MATERIAL\_DOCUMENT\_SRV/A\_MaterialDocumentItem?\$filter=PurchaseOrder eq '<PONUM>'}$$
- Perhitungan net movement:
  $$\text{Hasil} = \sum \text{Qty}(101) - \sum \text{Qty}(102) - \sum \text{Qty}(122)$$
- Logika validasi:
  - Jika $\text{Hasil} > 0$: PO diblokir untuk di-update, status diset `error_data`, dan middleware mencatat log alasan bahwa barang sudah diterima (GRN aktif).
  - Jika $\text{Hasil} \le 0$ atau tidak ada dokumen GR: Perubahan PO diizinkan dan dikirim via `PATCH`.

### C. GRN Return & Void / Cancel
- Penambahan field `InvtryMgmtReferenceDocument` pada payload Goods Movement type 122 (Return).
- Lookup otomatis nomor Material Document dan Material Document Year dari SAP berdasarkan referensi nomor BPI GRN Maximo.
- Endpoint GRN menggunakan user `Y_BPI_CU_SAP_COM_0108`.

### D. Master Data & Sinkronisasi Ulang Maximo
- Mapping Product/Material:
  - `ProductGroup` diambil dari `StockType` Maximo.
  - `ValuationClass` diambil dari `ITEMGROUPID` Maximo.
  - `to_Plant`: extend otomatis ke Plant `1000` dan `2000`.
- Staging update: Jika ada update data di table `mxout_inter_trans` dengan header ID yang sama, record di middleware diperbarui sehingga data terbaru dapat diproses ulang tanpa duplikasi.

### E. Tampilan UI Middleware Panel
- Pagination tabel diseragamkan dengan limit 20 item per halaman (Material Config, UOM Mapping, Tracking Data) agar layout sidebar tetap rapi dan tidak terdistorsi.
- Desain sidebar diharmonisasikan di seluruh halaman.

---

## 4. Status Verifikasi & Testing

- **Middleware Core Tests**:
  - `cd middleware; go test ./...` $\rightarrow$ **100% PASS** (`client`, `controller`, `repository`, `routes`, `service`).
- **Middleware Panel Tests**:
  - `cd middleware-panel; go test ./...` $\rightarrow$ **100% PASS** (`main`, `controllers`, `repository`).

---

## 5. Rencana & Task Selanjutnya

1. **End-to-End Live Verification**:
   - Uji coba push PO Type 1 sampai Type 4 langsung ke tenant SAP S/4HANA Grow.
   - Uji coba skenario PO Change saat sudah ada GRN (memastikan blocking berhasil) vs belum ada GRN (memastikan PATCH berhasil).
2. **GRN Void & Return Real Testing**:
   - Verifikasi lookup `InvtryMgmtReferenceDocument` dari live SAP data dan posting movement type 102 (cancel) / 122 (return).
3. **Product / Material Master Sync**:
   - Pengujian sinkronisasi Item Master dari Maximo ke SAP dengan konfigurasi 200+ ITEMGROUPID dan UOM mapping.
4. **Finance RICEF Integrations**:
   - Melanjutkan monitoring dan pengetesan batch transfer invoice & G/L account line items.

---
*Generated automatically by Antigravity Session Memory System.*

