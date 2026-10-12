-- ============================================================================
-- DATABASE DDL: BPI MIDDLEWARE (SHOETOWN-STYLE HYBRID JSONB ARCHITECTURE)
-- Database: PostgreSQL 14+ / 16
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 1. TABEL INGESTION STAGING (Mirip staging_documents ShoeTown)
-- Menampung tarikan delta raw dari SQL Server Maximo (3-minute poller)
CREATE TABLE IF NOT EXISTS staging_documents (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    doc_type VARCHAR(50) NOT NULL,            -- 'PO', 'GRN', 'ITEM', 'UOM', 'PR', 'INVOICE'
    document_no VARCHAR(50) NOT NULL,         -- e.g. PONUM, GRNNUM, ITEMNUM
    flow_type VARCHAR(20) NOT NULL DEFAULT 'forward', -- 'forward' (MX->SAP) / 'reverse' (SAP->MX)
    raw_payload JSONB NOT NULL,               -- JSON mentah persis dari SQL Server Maximo
    status VARCHAR(30) NOT NULL DEFAULT 'PENDING', -- 'PENDING', 'PROCESSED', 'FAILED'
    error_msg TEXT DEFAULT '-',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT uq_staging_doc UNIQUE (doc_type, document_no, flow_type)
);

CREATE INDEX IF NOT EXISTS idx_staging_status ON staging_documents(status);
CREATE INDEX IF NOT EXISTS idx_staging_doc_type ON staging_documents(doc_type);

-- 2. TABEL CORE DOCUMENTS (Mirip documents ShoeTown)
-- Menampung payload siap kirim ke SAP Grow OData + Tracking status respon SAP
CREATE TABLE IF NOT EXISTS documents (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    doc_type VARCHAR(50) NOT NULL,            -- 'PO', 'GRN', 'ITEM', 'UOM', 'PR', 'INVOICE'
    document_no VARCHAR(50) NOT NULL,         -- Nomor Dokumen
    flow_type VARCHAR(20) NOT NULL DEFAULT 'forward',
    group_id VARCHAR(100) NOT NULL,           -- e.g. 'GRP-PO-10293'
    parent_doc_type VARCHAR(50),              -- e.g. 'PO' (jika dokumen ini GRN)
    parent_document_no VARCHAR(50),           -- e.g. 'PO-10293'
    
    -- DUA PAYLOAD JSONB INTI:
    raw_payload JSONB NOT NULL,               -- JSON mentah sumber (Maximo)
    sap_payload JSONB,                        -- JSON hasil transformasi yang ditembak ke SAP OData
    
    -- State Machine
    status VARCHAR(30) NOT NULL DEFAULT 'pending', 
    -- 'pending', 'processing', 'success', 'error_data', 'error_conn'
    
    retry_count INT DEFAULT 0,
    last_attempt_at TIMESTAMP WITH TIME ZONE,
    
    -- Hasil dari SAP Grow
    sap_document_no VARCHAR(50),              -- No Dokumen resmi yang digenerate SAP
    sap_response JSONB,                       -- Body respon utuh dari SAP OData
    error_message TEXT,                       -- Pesan error detail jika gagal
    
    -- Flagging Balik ke Maximo (mxout_inter_trans: ax = 1 / ax = 3)
    sync_back_status VARCHAR(20) DEFAULT 'pending', -- 'pending', 'synced', 'failed'
    
    document_date DATE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    
    CONSTRAINT uq_core_documents UNIQUE (doc_type, document_no, flow_type)
);

CREATE INDEX IF NOT EXISTS idx_doc_lifecycle ON documents(status, retry_count);
CREATE INDEX IF NOT EXISTS idx_doc_lookup ON documents(doc_type, document_no);
CREATE INDEX IF NOT EXISTS idx_doc_group ON documents(group_id);
CREATE INDEX IF NOT EXISTS idx_doc_sap_payload_gin ON documents USING GIN (sap_payload);

-- 3. TABEL BUDGET TRANSACTION LOCK (Spesifik Logika BPI)
-- Menahan pagu anggaran agar tidak jebol sebelum dokumen resmi terposting di SAP
CREATE TABLE IF NOT EXISTS budget_masters (
    id BIGSERIAL PRIMARY KEY,
    budget_combination VARCHAR(50) NOT NULL,  -- COA - BudgetCode (e.g. '513043-28011')
    budget_year INT NOT NULL,
    approved_budget NUMERIC(18, 2) NOT NULL,
    remaining_budget NUMERIC(18, 2) NOT NULL,
    status VARCHAR(20) DEFAULT 'APPR',
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT uq_budget_key UNIQUE (budget_combination, budget_year)
);

CREATE TABLE IF NOT EXISTS budget_transaction_locks (
    id BIGSERIAL PRIMARY KEY,
    budget_combination VARCHAR(50) NOT NULL,
    budget_year INT NOT NULL,
    reference_doc_type VARCHAR(30) NOT NULL,  -- 'PO', 'GRN', 'MANUAL_JOURNAL'
    reference_doc_no VARCHAR(50) NOT NULL,
    lock_amount NUMERIC(18, 2) NOT NULL,
    lock_status VARCHAR(20) NOT NULL DEFAULT 'RESERVED', -- 'RESERVED', 'COMMITTED', 'RELEASED'
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT uq_budget_lock_ref UNIQUE (reference_doc_type, reference_doc_no)
);

CREATE INDEX IF NOT EXISTS idx_budget_lock_query ON budget_transaction_locks(budget_combination, budget_year, lock_status);

-- 4. TABEL MAPPING LOOKUP
CREATE TABLE IF NOT EXISTS integration_mappings (
    id BIGSERIAL PRIMARY KEY,
    mapping_type VARCHAR(50) NOT NULL,        -- 'UOM', 'GL_ACCOUNT', 'COST_CENTER'
    source_key VARCHAR(100) NOT NULL,
    target_key VARCHAR(100) NOT NULL,
    description VARCHAR(255),
    is_active BOOLEAN DEFAULT TRUE,
    CONSTRAINT uq_mapping_key UNIQUE (mapping_type, source_key)
);

-- 5. TABEL SYNC WATERMARK TRACKER
CREATE TABLE IF NOT EXISTS sap_sync_tracker (
    interface_id VARCHAR(50) PRIMARY KEY,
    last_sync_timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 6. TABEL SCHEDULER CONFIG (Runtime Settings)
CREATE TABLE IF NOT EXISTS scheduler_configs (
    id INT PRIMARY KEY DEFAULT 1,
    pull_is_active BOOLEAN DEFAULT TRUE,
    pull_interval INT DEFAULT 3,              -- Poller 3 menit
    push_is_active BOOLEAN DEFAULT TRUE,
    push_interval INT DEFAULT 1,              -- Push poller 1 menit
    last_check_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

INSERT INTO scheduler_configs (id, pull_is_active, pull_interval, push_is_active, push_interval)
VALUES (1, TRUE, 3, TRUE, 1)
ON CONFLICT (id) DO NOTHING;

-- Seed contoh watermark tracker
INSERT INTO sap_sync_tracker (interface_id, last_sync_timestamp)
VALUES 
    ('INT_BPI_PO_01', NOW() - INTERVAL '1 day'),
    ('INT_BPI_GRN_01', NOW() - INTERVAL '1 day'),
    ('INT_BPI_ITEM_01', NOW() - INTERVAL '1 day')
ON CONFLICT (interface_id) DO NOTHING;
