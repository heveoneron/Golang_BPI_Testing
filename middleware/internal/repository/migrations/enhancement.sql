-- PRD "Enhancement MW Panel & Golang Middleware" (Close Period, PO Outstanding < 2025, Idempotency).
-- Idempotent: aman dijalankan setiap start Engine.

-- ===================== Fitur 2: Close Period (FSD Close Period Service) =====================
-- Satu baris per (tahun, bulan). Bulan tanpa baris = terbuka.
CREATE TABLE IF NOT EXISTS close_periods (
    period_year  INT NOT NULL,
    period_month INT NOT NULL CHECK (period_month BETWEEN 1 AND 12),
    is_closed    BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by   VARCHAR(100),
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (period_year, period_month)
);

-- ===================== Fitur 4: Idempotency flag pada documents =====================
ALTER TABLE documents ADD COLUMN IF NOT EXISTS is_sent BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS is_processing BOOLEAN NOT NULL DEFAULT FALSE;
-- Kunci idempotency = DOC_TYPE:DOC_NUM:DIRECTION (flow_type), selalu terisi untuk semua jalur insert.
-- Revisi (REVISIONNUM) memakai baris yang sama: payload yang berubah me-reset is_sent agar revisi terkirim.
ALTER TABLE documents ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(255)
    GENERATED ALWAYS AS (doc_type || ':' || document_no || ':' || COALESCE(flow_type, 'forward')) STORED;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS sent_at TIMESTAMP WITH TIME ZONE;
-- Data lama: dokumen yang sudah sukses dianggap sudah terkirim.
UPDATE documents SET is_sent = TRUE, sent_at = COALESCE(sent_at, updated_at)
WHERE status = 'success' AND is_sent = FALSE;
CREATE UNIQUE INDEX IF NOT EXISTS uq_documents_idempotency_key ON documents (idempotency_key);

-- ===================== Fitur 3: PO Outstanding < 2025 (migrasi historis) =====================
-- Satu baris per line item sesuai "Template PO Outstanding and AX.xlsx".
CREATE TABLE IF NOT EXISTS stg_po_outstanding_legacy (
    id                  BIGSERIAL PRIMARY KEY,
    batch_id            VARCHAR(50) NOT NULL,
    source_system       VARCHAR(10) NOT NULL,              -- MAXIMO / AX
    row_number          INT,
    currencycode        VARCHAR(10),
    orderdate           DATE NOT NULL,
    dp_percentage       NUMERIC(9, 4),
    ponum               VARCHAR(50) NOT NULL,
    siteid              VARCHAR(20),
    vendor              VARCHAR(50),
    bpibudgetcode       VARCHAR(50),
    pol_desc_ld         TEXT,
    itemnum             VARCHAR(50),
    orderqty            NUMERIC(18, 4),
    orderunit           VARCHAR(20),
    polinenum           INT NOT NULL,
    taxcode1            VARCHAR(20),
    unitcost            NUMERIC(18, 4),
    sync_status         VARCHAR(20) NOT NULL DEFAULT 'PENDING', -- PENDING / PROCESSING / SYNCED / FAILED
    sap_po_number       VARCHAR(20),
    error_message       TEXT,
    is_sent             BOOLEAN NOT NULL DEFAULT FALSE,
    is_processing       BOOLEAN NOT NULL DEFAULT FALSE,
    sent_at             TIMESTAMP WITH TIME ZONE,
    retry_count         INT NOT NULL DEFAULT 0,
    uploaded_by         VARCHAR(100),
    created_at          TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at          TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT uq_po_outstanding_line UNIQUE (source_system, ponum, polinenum),
    CONSTRAINT ck_po_outstanding_before_2025 CHECK (orderdate < DATE '2025-01-01')
);
CREATE INDEX IF NOT EXISTS idx_po_outstanding_status ON stg_po_outstanding_legacy (sync_status);
CREATE INDEX IF NOT EXISTS idx_po_outstanding_ponum ON stg_po_outstanding_legacy (ponum);
