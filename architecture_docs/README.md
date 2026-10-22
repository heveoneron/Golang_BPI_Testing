# Dokumentasi Arsitektur & Spesifikasi Sistem Middleware Integrasi

Dokumentasi ini menyajikan arsitektur lengkap, alur data, siklus hidup dokumen, dan urutan komunikasi dari solusi **BPI Testing Integration Middleware**. Sistem ini menghubungkan **Maximo Enterprise Asset Management (EAM)** dengan **SAP Grow S/4HANA Cloud** melalui **PostgreSQL 3-Tier Staging Buffer** berbasis **Golang**.

Semua visualisasi interaktif pada folder ini dirancang dan divalidasi menggunakan engine **Archify** dengan standar *Showcase Quality* (100% lolos 9/9 validasi teknis layout SVG, bebas error dan bebas warning).

---

## 🧭 Daftar Diagram Interaktif (Archify Artifacts)

Anda dapat membuka file-file HTML berikut langsung di browser (klik ganda atau buka melalui web server) untuk menjelajahi diagram interaktif lengkap dengan fitur **Dark/Light Mode**, **Trace Animation**, **Pan & Zoom**, dan **PNG/SVG Export**:

| No | Tipe Diagram | File HTML Interaktif | File Spesifikasi JSON | Deskripsi Singkat |
|:---|:---|:---|:---|:---|
| 1 | **Architecture** | [`system_architecture.html`](./system_architecture.html) | [`system_architecture.json`](./system_architecture.json) | Topologi arsitektur sistem menyeluruh, komponen subsystem, trust boundary, dan interface port. |
| 2 | **Dataflow** | [`data_pipeline_flow.html`](./data_pipeline_flow.html) | [`data_pipeline_flow.json`](./data_pipeline_flow.json) | Alur 3-Tier Pipeline 5-tahap dari deteksi antrian transaksi, relational gate, hingga SAP Cloud. |
| 3 | **Lifecycle** | [`sync_lifecycle.html`](./sync_lifecycle.html) | [`sync_lifecycle.json`](./sync_lifecycle.json) | Mesin status (State Machine) dokumen, penanganan anomali orphan record, retry backoff, dan dead letter. |
| 4 | **Sequence** | [`dispatch_sequence.html`](./dispatch_sequence.html) | [`dispatch_sequence.json`](./dispatch_sequence.json) | Urutan panggilan API, CSRF token & cookie session handshake, row-level locking, dan two-phase update. |

> **Portal Navigasi Utama:** Anda juga dapat membuka [`index.html`](./index.html) untuk mengakses dashboard navigasi terpadu dengan metrik arsitektur.

---

## 🏗️ Topologi Arsitektur Sistem (High-Level Overview)

Sistem middleware terdiri dari 4 zona utama:

1. **Enterprise Asset Management (Maximo EAM & SQL Server Outbound Queue)**:
   - Data operasional seperti Purchase Order (PO), Purchase Requisition (PR), Material Receipt, dan Storeroom dicatat oleh Maximo ke dalam antrian SQL Server.
   - Tabel antrian utama: `mxout_inter_trans`.
   - Tabel detail relasional: `stg_po`, `stg_pr`, `stg_receipt`, `stg_storeroom`, dll (direferensikan secara dinamis via kolom `ifacetbname` dan foreign key `transid`).
   - Flag antrian: `0 = Pending (belum diproses)`, `1 = Processed (selesai diproses)`.

2. **Middleware Core Engine (Golang Runtime)**:
   - **Dual-Thread Engine**:
     - **Pull Worker (Poller)**: Bekerja pada interval terjadwal (default: 3 menit) untuk mengambil antrian yang belum diproses.
     - **Push Worker (Dispatcher)**: Bekerja pada interval cepat (default: 1 menit) untuk mengirimkan dokumen yang sudah siap ke SAP.
   - **Relational Integrity Guard (Orphan Check)**: Memastikan header antrian memiliki data detail pada tabel yang bersangkutan sebelum data diteruskan.
   - **Transform Core**: Mengonversi snapshot data mentah ke dalam format skema kanonikal enterprise.
   - **Web Operations Panel (:8091)**: Antarmuka visual untuk tim operasi untuk memonitor antrian, melihat status log, dan memicu sinkronisasi manual.
   - **Swagger REST API (:8085)**: Antarmuka RESTful standar untuk pengujian terprogram.

3. **PostgreSQL 3-Tier Staging Buffer**:
   - **Tier 1 (`staging_documents`)**: Menyimpan raw JSON snapshot tanpa modifikasi langsung dari SQL Server untuk menjamin keaslian data audit.
   - **Tier 2 (`documents`)**: Tabel siap kirim yang dinormalisasi dengan kontrol status (`PENDING`, `PROCESSING`, `SUCCESS`, `FAILED`).
   - **Tier 3 (`integration_logs`)**: Jejak audit komprehensif yang merekam payload request, response SAP, waktu latensi, dan pesan error.

4. **Target Cloud ERP (SAP Grow S/4HANA Cloud)**:
   - Komunikasi berbasis HTTPS aman menggunakan OData Service v2/v4.
   - Otentikasi dua langkah: Fetch dynamic CSRF Token + Session Cookie, diikuti dengan transactional POST payload.

---

## 🛡️ Mekanisme Utama & Peningkatan Rekayasa Sistem

### 1. Relational Integrity Guard (Pencegahan Kirim Data Kosong / Orphan Record)
- **Permasalahan Sebelumnya**: Data antrian pada `mxout_inter_trans` sering kali tidak memiliki baris detail pada tabel detail terkait, namun tetap dikirimkan ke cloud sehingga menghasilkan error di sistem target.
- **Solusi yang Diterapkan**:
  - Middleware membaca kolom `ifacetbname` dan `transid` dari `mxout_inter_trans`.
  - Sistem melakukan query verifikasi ke tabel detail bersangkutan.
  - **Jika baris detail TIDAK ADA**: Antrian ditandai sebagai di-bypass (flag diupdate menjadi `1` dengan catatan bahwa record tidak memiliki baris detail), dicatat di `integration_logs`, dan **0 data dikirim ke staging downstream**.
  - **Jika baris detail ADA**: Data ditarik, dibungkus menjadi payload JSON terstruktur, dan dimasukkan ke staging buffer.

### 2. Concurrency Control & Single-Delivery Guarantee
- Untuk mencegah pengiriman ganda pada lingkungan multi-instance atau overlap interval ticker, Push Dispatcher menggunakan mekanisme locking baris:
  ```sql
  SELECT * FROM documents 
  WHERE status = 'PENDING' 
  ORDER BY created_at ASC 
  FOR UPDATE SKIP LOCKED 
  LIMIT 50;
  ```
- Status langsung diubah menjadi `PROCESSING` seketika saat lock diperoleh.

### 3. Otentikasi & Handshake SAP Cloud
- SAP Cloud ERP memerlukan perlindungan CSRF (Cross-Site Request Forgery) untuk semua mutasi HTTP POST/PUT:
  1. Middleware mengirimkan request `GET` ke service root dengan header `X-CSRF-Token: Fetch`.
  2. SAP merespons dengan token CSRF aktif dan sekumpulan session cookies (`SAP_SESSIONID`, `MYSAPSSO2`).
  3. Middleware menyimpan token dan cookie tersebut di dalam memory session.
  4. Middleware mengirimkan payload OData dengan menyertakan token dan cookies tersebut.
  5. Jika respons berhasil (HTTP 201 Created), SAP Document Number diekstrak dan disimpan di middleware serta antrian sumber diperbarui ke status selesai.

### 4. Siklus Status Dokumen (State Machine)
- `DETECTED`: Record terdeteksi pada antrian SQL Server.
- `ORPHAN_SKIPPED`: Baris detail tidak ditemukan; transaksi diakhiri dengan log audit.
- `STAGED_RAW`: Snapshot raw JSON tersimpan di Tier 1 `staging_documents`.
- `PENDING`: Skema kanonikal siap kirim di Tier 2 `documents`.
- `PROCESSING`: Dokumen sedang dikunci dan diproses oleh Push Dispatcher.
- `SUCCESS`: Berhasil dibuat di SAP Grow Cloud (menyimpan nomor dokumen SAP).
- `FAILED / RETRY`: Gagal kirim (misal timeout atau 5xx); backoff retry dijalankan otomatis hingga 3 kali percobaan sebelum masuk ke `DEAD_LETTER`.

---

## 📂 Struktur File Direktori `architecture_docs/`

```text
architecture_docs/
├── index.html                 # Portal web dashboard navigasi dokumentasi
├── README.md                  # Panduan teknis komprehensif (dokumen ini)
├── system_architecture.html   # Diagram Arsitektur Komponen (Archify Showcase)
├── system_architecture.json   # Spesifikasi JSON Arsitektur Komponen
├── data_pipeline_flow.html    # Diagram Alur Data 3-Tier (Archify Showcase)
├── data_pipeline_flow.json    # Spesifikasi JSON Alur Data 3-Tier
├── sync_lifecycle.html        # Diagram State Machine Lifecycle (Archify Showcase)
├── sync_lifecycle.json        # Spesifikasi JSON State Machine Lifecycle
├── dispatch_sequence.html     # Diagram Urutan Komunikasi & Handshake (Archify Showcase)
└── dispatch_sequence.json     # Spesifikasi JSON Urutan Komunikasi & Handshake
```

---

## 🚀 Cara Menjalankan & Memverifikasi

1. **Membuka Portal Dokumentasi**:
   - Buka file [`index.html`](./index.html) menggunakan peramban web modern apa pun (Chrome, Edge, Firefox).
2. **Membuka Diagram Langsung**:
   - Buka salah satu file `.html` yang ada di dalam folder ini.
   - Manfaatkan tombol interaktif di kanan atas diagram untuk beralih antara tema gelap (*Dark*) dan terang (*Light*).
   - Gunakan fitur *View Focus* di bagian bawah diagram untuk menyoroti tahapan spesifik secara bergantian.
3. **Memvalidasi Ulang dengan CLI Archify**:
   ```bash
   # Validasi seluruh spesifikasi arsitektur
   node "C:\Users\muham\.gemini\config\skills\archify\bin\archify.mjs" validate architecture .\system_architecture.json --quality showcase
   node "C:\Users\muham\.gemini\config\skills\archify\bin\archify.mjs" validate dataflow .\data_pipeline_flow.json --quality showcase
   node "C:\Users\muham\.gemini\config\skills\archify\bin\archify.mjs" validate lifecycle .\sync_lifecycle.json --quality showcase
   node "C:\Users\muham\.gemini\config\skills\archify\bin\archify.mjs" validate sequence .\dispatch_sequence.json --quality showcase
   ```

---
*Dibuat untuk project BPI Testing Integration Middleware (Maximo EAM &harr; PostgreSQL &harr; SAP Grow S/4HANA Cloud).*
