# Panduan Deploy & Operasional BPI Middleware di Server Linux

Dokumen ini berisi panduan langkah demi langkah untuk melakukan deploy, menjalankan, dan memantau aplikasi BPI Middleware serta database PostgreSQL di server Linux target.

---

## 1. Informasi Target Server
- **IP Server**: `192.168.214.210`
- **Username**: `administrator`
- **Port Layanan**:
  - Web Management Panel: `:8091` (`http://192.168.214.210:8091`)
  - Middleware Engine API: `:8085` (`http://192.168.214.210:8085`)
  - PostgreSQL Database: `:5432` (`192.168.214.210:5432`)

---

## 2. Persiapan di Server Linux (Jika Docker belum terpasang)

Login via SSH:
```bash
ssh administrator@192.168.214.210
```

Cek apakah Docker & Docker Compose sudah ada:
```bash
docker --version
docker compose version
```

Jika belum terpasang, instal otomatis via official script:
```bash
sudo apt-get update && sudo apt-get install -y curl
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker administrator
newgrp docker
```

---

## 3. Transfer File Project ke Server

Jalankan perintah ini dari terminal lokal komputer kamu (PowerShell / Git Bash):

### Opsi A (Kompresi Zip - Paling Cepat & Rapi):
```powershell
# Di PowerShell lokal:
Compress-Archive -Path * -DestinationPath bpi_deploy.zip -Force
scp bpi_deploy.zip administrator@192.168.214.210:~/
ssh administrator@192.168.214.210 "mkdir -p ~/bpi-middleware && unzip -o ~/bpi_deploy.zip -d ~/bpi-middleware && rm ~/bpi_deploy.zip"
```

### Opsi B (Git Clone langsung di server):
```bash
ssh administrator@192.168.214.210
git clone <URL_REPO_GIT> ~/bpi-middleware
```

---

## 4. Menjalankan Aplikasi dengan Docker Compose

Masuk ke folder project di server:
```bash
cd ~/bpi-middleware
```

*(Opsional)* Jika ingin mengubah konfigurasi kredensial atau IP database:
```bash
cp .env.example .env
nano .env
```

Jalankan stack container:
```bash
docker compose up -d --build
```

Cek status container:
```bash
docker compose ps
```
Semua container (`bpi-postgres`, `bpi-middleware`, `bpi-middleware-panel`) harus berstatus `Up` / `healthy`.

---

## 5. Cara Cek & Monitoring PostgreSQL

### A. Cek Status Database & Health
```bash
docker compose ps postgres
```

### B. Masuk ke PostgreSQL CLI (psql)
```bash
docker exec -it bpi-postgres psql -U postgres -d bpi_middleware_db
```

Query penting di dalam psql:
```sql
-- 1. Lihat seluruh daftar tabel
\dt

-- 2. Cek data mapping Valuation Class & GL Account (GI & Stock Adj)
SELECT * FROM valuation_class_gl_mappings;

-- 3. Cek user administrator Web Panel
SELECT id, username, role, is_active FROM users;

-- 4. Cek ringkasan dokumen staging per tipe dan status
SELECT doc_type, status, count(*) FROM staging_documents GROUP BY doc_type, status;

-- 5. Cek tabel endpoint SAP yang aktif
SELECT service_code, base_url, is_active FROM sap_endpoints;

-- 6. Keluar dari psql
\q
```

### C. Quick Query Tanpa Masuk ke psql
```bash
docker exec -it bpi-postgres psql -U postgres -d bpi_middleware_db -c "SELECT doc_type, status, count(*) FROM staging_documents GROUP BY doc_type, status;"
```

### D. Akses GUI dari Laptop (DBeaver / Navicat / pgAdmin)
- **Host**: `192.168.214.210`
- **Port**: `5432`
- **Database**: `bpi_middleware_db`
- **Username**: `postgres`
- **Password**: `postgres123`

---

## 6. Operasional Harian Container

| Perintah | Fungsi |
|---|---|
| `docker compose logs -f middleware` | Pantau log real-time Middleware Engine |
| `docker compose logs -f middleware-panel` | Pantau log Web Management Panel |
| `docker compose logs -f postgres` | Pantau log Database PostgreSQL |
| `docker compose restart middleware` | Restart layanan Middleware Engine |
| `docker compose down` | Mematikan seluruh stack (data DB tetap aman di volume) |
| `docker compose up -d --build` | Re-build & deploy ulang saat ada update kode |
