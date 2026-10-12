# 🚀 Golang_BPI_Testing

> **Enterprise Integration Middleware: IBM Maximo $\leftrightarrow$ PostgreSQL Staging $\leftrightarrow$ SAP Grow (S/4HANA Cloud)**  
> Mengadopsi arsitektur dual-worker dari **Nojoroso** dan hybrid JSONB state machine dari **ShoeTown Middleware**.

---

## 📌 Fitur Utama

1. **Dual-Tier JSONB PostgreSQL Staging**:
   - `staging_documents`: Menampung delta raw dari SQL Server Maximo (`raw_payload`).
   - `documents`: State machine dokumen siap kirim ke SAP Grow (`sap_payload`) dengan tracking respon & audit log.
2. **Transaction Lock Engine (`budget_transaction_locks`)**:
   - Menahan pagu anggaran secara realtime sebelum PO / Jurnal dikirim ke SAP Grow untuk mencegah defisit anggaran.
3. **Dual-Thread Worker (Pull & Push Architecture)**:
   - **Pull Worker (3-Menit Ticker)**: Mengambil data delta dari SQL Server dan mengubahnya ke `staging_documents`.
   - **Push Worker (1-Menit Ticker)**: Mengambil antrean `pending` via atomic row locking (`FOR UPDATE SKIP LOCKED`) dan mengeksekusi request OData ke SAP Grow.
4. **Resilient SAP Client**:
   - Otomatis melakukan **CSRF Token Handshake** (`x-csrf-token: fetch`) dengan cookie session management.
   - Dilengkapi **Mock Mode** (`SAP_BASE_URL=MOCK`) untuk kemudahan pengujian di lokal.
5. **REST API Interface**:
   - `GET /api/v1/health`: Cek status service.
   - `GET /api/v1/documents`: Monitoring daftar dokumen & status posting SAP.
   - `POST /api/v1/ingest/po`: Ingestion payload PO Maximo secara manual untuk testing lokal.
   - `POST /api/v1/trigger/push`: Trigger manual pengiriman dokumen ke SAP Grow.

---

## 🗄️ Inisialisasi Database PostgreSQL

Jalankan script schema yang ada di `database/schema.sql` pada PostgreSQL lokal Anda:

```bash
# 1. Buat database baru di PostgreSQL
createdb -U postgres bpi_middleware_db

# 2. Eksekusi script DDL
psql -U postgres -d bpi_middleware_db -f database/schema.sql
```

---

## ⚙️ Cara Menjalankan

1. Salin konfigurasi environment:
   ```bash
   cp .env.example .env
   ```
2. Sesuaikan konfigurasi `PG_CONN_STRING` di dalam file `.env`.
3. Jalankan aplikasi:
   ```bash
   go run cmd/server/main.go
   ```

---

## 📖 Dokumentasi & Testing Interaktif via Swagger UI

Service telah terintegrasi dengan **Swagger UI** interaktif:
- **URL Swagger UI**: [http://localhost:8085/swagger/index.html](http://localhost:8085/swagger/index.html)
- **URL Shortcut**: Akses langsung ke `http://localhost:8085/` atau `http://localhost:8085/swagger` akan otomatis dialihkan (redirect) ke Swagger UI.
- Semua endpoint dilengkapi contoh payload Maximo PO dan skema DTO yang siap diuji langsung lewat tombol **"Try it out"**.

Untuk memperbarui dokumentasi swagger jika ada perubahan anotasi:
```bash
swag init -g cmd/server/main.go -o ./docs --parseDependency --parseInternal
```

---

## 🧪 Menjalankan Automated Unit Tests

Jalankan test suite seluruh modul (client, controller, router, service):
```bash
go test -v ./...
```

---

## 🧪 Contoh Testing Ingest Manual via cURL

Kirim contoh dokumen PO Maximo ke service lokal:

```bash
curl -X POST http://localhost:8085/api/v1/ingest/po \
  -H "Content-Type: application/json" \
  -d '{
    "PONUM": "PO-BPI-2026-001",
    "VENDOR": "VEND-PLTU-01",
    "ORDERDATE": "2026-10-12",
    "CURRENCYCODE": "IDR",
    "STATUS": "APPR",
    "REVISIONNUM": 0,
    "LINES": [
      {
        "POLINENUM": 1,
        "ITEMNUM": "BEARING-TURBINE-6204",
        "ORDERUNIT": "PCS",
        "QTY": 5,
        "UNITCOST": 150000,
        "BUDGETCODE": "10081",
        "DEPARTMENT": "3000",
        "MAINACCOUNT": "513043"
      }
    ]
  }'
```

Cek dokumen yang terbentuk:
```bash
curl http://localhost:8085/api/v1/documents
```

---

## 📁 Struktur Direktori

```text
Golang_BPI_Testing/
├── cmd/
│   └── server/
│       └── main.go               # Entry point background service & HTTP server
├── database/
│   └── schema.sql                # DDL PostgreSQL (Staging, Documents, Budget Lock)
├── internal/
│   ├── client/
│   │   └── sap_client.go         # SAP OData Client (CSRF Handshake & Mock)
│   ├── controller/
│   │   └── web_controller.go     # HTTP Handler & Manual Ingestion
│   ├── model/
│   │   └── model.go              # Struct Staging, Documents, PO, & SAP OData
│   ├── repository/
│   │   ├── postgres.go           # Repository Postgres (Atomic Lock & Tracking)
│   │   └── sqlserver.go          # Repository SQL Server Maximo Delta
│   ├── routes/
│   │   └── router.go             # Gin Router Mapping
│   └── service/
│       └── sync_service.go       # Transformasi JSON & Orkestrasi Sync
├── .env.example
├── go.mod
└── README.md
```
