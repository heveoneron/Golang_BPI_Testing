# 🚀 Golang_BPI_Testing

> **Enterprise Integration Suite: IBM Maximo $\leftrightarrow$ PostgreSQL Staging $\leftrightarrow$ SAP Grow (S/4HANA Cloud)**  
> Terbagi menjadi 2 modul terpisah persis seperti standar arsitektur **ShoeTown Middleware**:
> 1. `middleware/` : Engine background worker (Nojoroso dual-thread poller + Swagger OpenAPI).
> 2. `middleware-panel/` : Web panel monitoring UI lengkap (Tracking Data, Metric Cards, Payload Inspector, System Config, Scheduler).

---

## 📁 Struktur Direktori Repositori

```text
Golang_BPI_Testing/
├── middleware/                   # Engine Middleware Golang (Port 8085)
│   ├── cmd/server/main.go        # Entry point background worker & REST API
│   ├── database/schema.sql       # DDL PostgreSQL Hybrid JSONB & Budget Lock
│   ├── docs/                     # Swagger OpenAPI 2.0 (JSON, YAML, Go)
│   ├── internal/                 # Client OData, Service Sync, Repositories
│   ├── .env.example              # Template environment engine
│   └── README.md
│
└── middleware-panel/             # Web Panel Monitoring UI (Port 8091)
    ├── controllers/              # Dashboard, Config, Scheduler, Auth
    ├── models/                   # GORM models (Document, Scheduler, SystemLog)
    ├── repository/               # Query tracking, metric sparklines, aggregation
    ├── services/                 # Business logic & sync trigger
    ├── static/                   # CSS (Bootstrap 5, Sidebar, Icons), JS, Fonts
    ├── templates/                # HTML Templates (dashboard.html, sidebar.html, login.html)
    ├── main.go                   # Web Panel Entry point
    └── .env.example              # Template environment web panel
```

---

## 🛠️ Modul 1: Middleware Engine (`middleware/`)

### Fitur Utama
1. **Dual-Thread Background Worker (Nojoroso Pattern)**:
   - **Pull Worker (3-Menit Ticker)**: Menarik data delta dari SQL Server Maximo berbasis watermark `lastSync` $\rightarrow$ masuk ke `staging_documents` $\rightarrow$ transformasi ke `documents`.
   - **Push Worker (1-Menit Ticker)**: Mengirimkan dokumen pending secara atomic (`FOR UPDATE SKIP LOCKED`) ke SAP Grow OData.
2. **Transaction Lock Engine (`budget_transaction_locks`)**:
   - Menahan pagu anggaran secara realtime sebelum PO / Jurnal diposting ke SAP Grow.
3. **Resilient SAP OData Client**:
   - Otomatis melakukan **CSRF Token Handshake** (`x-csrf-token: fetch`) dengan cookie session management dan Mock Mode (`SAP_BASE_URL=MOCK`).
4. **Swagger OpenAPI UI**:
   - Dokumentasi interaktif di `http://localhost:8085/swagger/index.html`.

### Cara Menjalankan Middleware Engine:
```bash
cd middleware

# Setup environment
cp .env.example .env

# Jalankan database PostgreSQL
createdb -U postgres bpi_middleware_db
psql -U postgres -d bpi_middleware_db -f database/schema.sql

# Jalankan engine
go run cmd/server/main.go
```

---

## 🖥️ Modul 2: Middleware Web Panel (`middleware-panel/`)

### Fitur Utama (Persis UI ShoeTown Middleware)
1. **Tracking Data & Realtime Metric Cards**:
   - Total Documents, SAP Success, Pending/Queue, dan Failed/Error.
2. **Full Responsive Data Table & Filter**:
   - Filter dokumen berdasarkan status, tipe dokumen, pencarian nomor dokumen/group, dan date range.
3. **Payload Inspector & Tree View**:
   - Inspeksi visual raw JSON Maximo dan transformasi payload OData SAP Grow.
4. **Interactive Controls**:
   - Trigger manual pengiriman ke SAP Grow via tombol web.
   - Dark & Light sidebar theme toggle.

### Cara Menjalankan Middleware Web Panel:
```bash
cd middleware-panel

# Setup environment
cp .env.example .env

# Jalankan Web Panel
go run main.go
```
Buka browser di: 👉 **`http://localhost:8091/login`** (atau langsung ke `/dashboard`).  
*(Kredensial default: `admin` / `admin`)*

---

## 🗄️ Panduan Koneksi ke SQL Server 2019 / 2012 (Lokal)

Middleware menggunakan driver `github.com/microsoft/go-mssqldb`. Konfigurasikan pada file `middleware/.env`:

```env
# Format URL standard:
SQL_CONN_STRING=sqlserver://<username>:<password>@<host>:<port>?database=<dbname>&encrypt=disable

# Contoh SQL Server 2019 di lokal (port default 1433):
SQL_CONN_STRING=sqlserver://sa:YourPassword123!@localhost:1433?database=MAXIMO_STAGING&encrypt=disable

# Jika menggunakan Named Instance (misal SQLEXPRESS):
SQL_CONN_STRING=sqlserver://sa:YourPassword123!@localhost:1433?database=MAXIMO_STAGING&instance=SQLEXPRESS&encrypt=disable
```

> **Checklist Pengaturan SQL Server 2019:**
> 1. Aktifkan **TCP/IP** di **SQL Server Configuration Manager** $\rightarrow$ *SQL Server Network Configuration* $\rightarrow$ *Protocols for MSSQLSERVER* (Port diset ke `1433`).
> 2. Aktifkan **SQL Server and Windows Authentication mode** (Mixed Mode) di SQL Server Properties $\rightarrow$ Security.
> 3. Restart service **SQL Server (MSSQLSERVER)**.
> 4. Parameter `encrypt=disable` wajib disertakan jika sertifikat SSL lokal self-signed belum terdaftar di CA Windows.
