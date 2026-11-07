---
trigger: always_on
---
# Global Automatic Skills & Rules

## 1. Auto Fast-Fix & Anti-Freeze (Jalan Otomatis)
- Setiap kali diminta memperbaiki kode atau tambah fitur, HANYA baca file yang relevan atau sedang dibuka.
- DILARANG KERAS menjalankan server daemon atau aplikasi secara langsung (seperti server.exe, middleware-panel.exe, atau go run).
- Untuk mengetes kode, cukup gunakan gopls-mcp-server/go_diagnostics atau jalankan go build / unit test spesifik dengan batas waktu maksimal 30 detik.

## 2. Auto Context & Directory Ignore (Jalan Otomatis)
- Selalu abaikan folder endor/, 
ode_modules/, in/, obj/, .git/, dan file .exe.
- Di setiap akhir tugas yang mengubah kode, selalu update secara singkat file SESSION_MEMORY.md berisi ringkasan perubahan terakhir agar histori tetap rapi tanpa perlu diminta.

## 3. Code & Discussion Style
- Jawab dengan Bahasa Indonesia yang santai dan to-the-point, gunakan Bahasa Inggris untuk kode.
