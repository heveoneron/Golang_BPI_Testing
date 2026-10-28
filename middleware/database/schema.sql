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
    is_active BOOLEAN DEFAULT TRUE,           -- Kompatibilitas dengan middleware-panel toggle
    batch_limit INT DEFAULT 1,                -- Limit data ditarik per siklus
    last_check_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 7. TABEL INTEGRATION LOGS (AUDIT TRAIL)
CREATE TABLE IF NOT EXISTS integration_logs (
    id BIGSERIAL PRIMARY KEY,
    trace_id VARCHAR(100),
    source_system VARCHAR(50) NOT NULL,       -- 'MAXIMO', 'MIDDLEWARE', 'SAP GROW'
    target_system VARCHAR(50) NOT NULL,       -- 'MIDDLEWARE', 'SAP GROW', 'MAXIMO'
    document_type VARCHAR(50) NOT NULL,       -- 'PO', 'GRN', 'ITEM', dll
    document_no VARCHAR(100) NOT NULL,        -- Nomor dokumen
    status VARCHAR(30) NOT NULL,              -- 'RECEIVED', 'SENT', 'SUCCESS', 'FAILED'
    error_message TEXT,
    metadata TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_logs_doc ON integration_logs(document_type, document_no);
CREATE INDEX IF NOT EXISTS idx_integration_logs_created ON integration_logs(created_at);

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

-- 8. TABEL SAP ENDPOINTS & MULTI-CREDENTIAL REGISTRY
CREATE TABLE IF NOT EXISTS sap_endpoints (
    id SERIAL PRIMARY KEY,
    service_code VARCHAR(50) NOT NULL UNIQUE,     -- 'PO', 'BP', 'GRN', 'PR'
    service_name VARCHAR(100) NOT NULL,           -- 'Purchase Order OData v4', 'Business Partner OData v2'
    scenario_id VARCHAR(50) NOT NULL,             -- 'SAP_COM_0053', 'SAP_COM_0008'
    odata_version VARCHAR(10) NOT NULL DEFAULT 'v4', -- 'v2' atau 'v4'
    base_url TEXT NOT NULL DEFAULT 'https://my445160-api.s4hana.cloud.sap',
    endpoint_path TEXT NOT NULL,
    csrf_path TEXT,                               -- Path untuk fetch token (default = endpoint_path)
    username VARCHAR(100) NOT NULL,
    password TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    last_status VARCHAR(30) DEFAULT 'UNTESTED',   -- 'SUCCESS', 'FAILED', 'UNTESTED'
    last_response TEXT,
    last_tested_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Seed kredensial resmi SAP Grow untuk PO (OData v4) dan BP (OData v2)
INSERT INTO sap_endpoints (service_code, service_name, scenario_id, odata_version, base_url, endpoint_path, username, password)
VALUES 
(
    'PO', 
    'Purchase Order Integration (OData v4)', 
    'SAP_COM_0053', 
    'v4', 
    'https://my445160-api.s4hana.cloud.sap',
    '/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder',
    'CU_SAP_COM_0053',
    'PowerofOne@123456789'
),
(
    'BP', 
    'Business Partner Integration (OData v2)', 
    'SAP_COM_0008', 
    'v2', 
    'https://my445160-api.s4hana.cloud.sap',
    '/sap/opu/odata/sap/API_BUSINESS_PARTNER/A_BusinessPartner',
    'CU_SAP_COM_0008',
    'PowerofOne@123456789'
),
(
    'PRODUCT', 
    'Product / Material Master Integration (OData v2)', 
    'SAP_COM_0009', 
    'v2', 
    'https://my445160-api.s4hana.cloud.sap',
    '/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product',
    'CU_SAP_COM_0009',
    'PowerofOne@123456789'
)
ON CONFLICT (service_code) DO UPDATE 
SET username = EXCLUDED.username, 
    password = EXCLUDED.password, 
    base_url = EXCLUDED.base_url,
    endpoint_path = EXCLUDED.endpoint_path, 
    odata_version = EXCLUDED.odata_version,
    updated_at = NOW();

-- 9. TABEL PEMETAAN SUMBER TABEL MAXIMO KE TARGET API SAP GROW (Table-to-API Mapping Matrix)
CREATE TABLE IF NOT EXISTS table_api_mappings (
    id SERIAL PRIMARY KEY,
    source_table VARCHAR(100) NOT NULL UNIQUE,     -- 'ITEM', 'PO', 'COMPANIES', 'MATRECTRANS', 'PR', 'AXUOM'
    source_desc VARCHAR(255),
    target_service_code VARCHAR(50) NOT NULL,      -- FK ke sap_endpoints: 'PRODUCT', 'PO', 'BP'
    direction VARCHAR(30) DEFAULT 'OUTBOUND',
    sync_mode VARCHAR(30) DEFAULT 'BATCH_SCHEDULE',
    is_active BOOLEAN DEFAULT TRUE,
    notes TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

INSERT INTO table_api_mappings (source_table, source_desc, target_service_code, direction, sync_mode, is_active, notes)
VALUES
    ('ITEM', 'Master Item, Material & Spare Parts', 'PRODUCT', 'OUTBOUND', 'BATCH_SCHEDULE', true, 'Sinkronisasi master data item/material ke SAP Grow API_PRODUCT_SRV'),
    ('PO', 'Purchase Order Header & Lines', 'PO', 'OUTBOUND', 'BATCH_SCHEDULE', true, 'Dokumen pemesanan barang ke SAP Grow api_purchaseorder_2'),
    ('COMPANIES', 'Vendor, Supplier & Business Partner', 'BP', 'OUTBOUND', 'REALTIME', true, 'Sinkronisasi mitra bisnis & supplier ke SAP Grow API_BUSINESS_PARTNER'),
    ('MATRECTRANS', 'Goods Receipt (GRN) & Material Movements', 'PO', 'OUTBOUND', 'MANUAL', true, 'Penerimaan barang fisik dari gudang Maximo terhadap PO SAP Grow'),
    ('PR', 'Purchase Requisition (Permintaan Pembelian)', 'PO', 'OUTBOUND', 'MANUAL', true, 'Pengajuan PR sebelum dikonversi menjadi PO di SAP'),
    ('AXUOM', 'Unit of Measure (UOM) Standar', 'PRODUCT', 'OUTBOUND', 'MANUAL', true, 'Katalog satuan ukuran Maximo ke SAP Product Master UOM')
ON CONFLICT (source_table) DO UPDATE
SET target_service_code = EXCLUDED.target_service_code,
    source_desc = EXCLUDED.source_desc,
    updated_at = NOW();

-- 10. TABEL EXCHANGE RATES (KURS PAJAK)
CREATE TABLE IF NOT EXISTS exchange_rates (
    id BIGSERIAL PRIMARY KEY,
    event VARCHAR(50),
    source VARCHAR(50),
    kmk_number VARCHAR(100),
    valid_from DATE,
    valid_to DATE,
    document_url TEXT,
    currency_code VARCHAR(10) NOT NULL,
    currency_name VARCHAR(100),
    rate NUMERIC(18, 4) NOT NULL,
    unit INT DEFAULT 1,
    rate_per_unit NUMERIC(18, 6),
    change NUMERIC(18, 4),
    raw_payload JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_exchange_rates_curr ON exchange_rates(currency_code);
CREATE INDEX IF NOT EXISTS idx_exchange_rates_kmk ON exchange_rates(kmk_number);

-- 11. TABEL UOM MAPPINGS (SATUAN UKURAN)
CREATE TABLE IF NOT EXISTS uom_mappings (
    id SERIAL PRIMARY KEY,
    maximo_uom VARCHAR(50) NOT NULL UNIQUE,
    sap_uom VARCHAR(50) NOT NULL,
    description VARCHAR(255),
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active)
VALUES 
    ('PC', 'PCE', 'Piece', true),
    ('EA', 'PCE', 'Each', true),
    ('MT', 'MAT', 'Meter', true),
    ('KG', 'KGM', 'Kilogram', true)
ON CONFLICT (maximo_uom) DO NOTHING;

-- 12. TABEL USERS & RBAC ACCESS
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    username VARCHAR(50) NOT NULL UNIQUE,
    password VARCHAR(255) NOT NULL,
    full_name VARCHAR(100),
    email VARCHAR(100),
    role VARCHAR(30) DEFAULT 'operator',
    allowed_menus TEXT DEFAULT '*',
    is_active BOOLEAN DEFAULT TRUE,
    last_login_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users(deleted_at);

INSERT INTO users (username, password, full_name, role, allowed_menus, is_active)
VALUES ('admin', 'admin', 'System Administrator', 'admin', '*', true)
ON CONFLICT (username) DO NOTHING;

-- 13. TABEL MATERIAL GL MAPPINGS
CREATE TABLE IF NOT EXISTS material_gl_mappings (
    id SERIAL PRIMARY KEY,
    ctg_item VARCHAR(100),
    gl_account VARCHAR(50),
    description TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 14. TABEL BACKUP SCHEDULES
CREATE TABLE IF NOT EXISTS backup_schedules (
    id SERIAL PRIMARY KEY,
    database_name VARCHAR(100),
    database_type VARCHAR(50),
    backup_level VARCHAR(50),
    backup_location VARCHAR(50),
    schedule_mode VARCHAR(50),
    execution_time VARCHAR(20),
    retention_days INT DEFAULT 7,
    is_active BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

INSERT INTO backup_schedules (id, database_name, database_type, backup_level, backup_location, schedule_mode, execution_time, retention_days, is_active)
VALUES (1, 'postgres', 'postgresql', 'full', 'local', 'daily', '02:00', 7, false)
ON CONFLICT (id) DO NOTHING;

-- 15. TABEL APP CONFIGS (.env key-value store)
CREATE TABLE IF NOT EXISTS configs (
    key VARCHAR(100) PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 16. TABEL SYSTEM LOGS
CREATE TABLE IF NOT EXISTS system_logs (
    id BIGSERIAL PRIMARY KEY,
    level VARCHAR(20),
    message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);



