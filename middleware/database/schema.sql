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
),
(
    'GRN', 
    'Material Document / Goods Movement (OData v2)', 
    'SAP_COM_0108', 
    'v2', 
    'https://my445160-api.s4hana.cloud.sap',
    '/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader',
    'Y_BPI_CU_SAP_COM_0108',
    'PowerofOne@123456789'
),
(
    'CANCEL_GR',
    'Cancel Goods Movement / Material Document (OData v2)',
    'SAP_COM_0108',
    'v2',
    'https://my445160-api.s4hana.cloud.sap',
    '/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/Cancel',
    'Y_BPI_CU_SAP_COM_0108',
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
    ('MATRECTRANS', 'Goods Receipt (GRN) & Material Movements', 'GRN', 'OUTBOUND', 'MANUAL', true, 'Penerimaan barang fisik dari gudang Maximo terhadap PO SAP Grow'),
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

-- 17. TABEL BP GROUP MAPPINGS (Business Partner Grouping)
CREATE TABLE IF NOT EXISTS bp_group_mappings (
    id SERIAL PRIMARY KEY,
    vendor_group VARCHAR(50) NOT NULL UNIQUE,     -- Maximo bpivendorgroup (e.g. '01')
    bp_grouping VARCHAR(50) NOT NULL,              -- SAP BusinessPartnerGrouping (e.g. 'BPAB')
    description VARCHAR(255),
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

INSERT INTO bp_group_mappings (vendor_group, bp_grouping, description, is_active)
VALUES ('01', 'BPAB', 'Vendor Lokal BPI - BPAB', true)
ON CONFLICT (vendor_group) DO UPDATE SET bp_grouping = EXCLUDED.bp_grouping, updated_at = NOW();

-- 18. TABEL RECON ACCOUNT & BP GROUPING MAPPINGS (Reconciliation Account & Business Partner Grouping)
CREATE TABLE IF NOT EXISTS recon_account_mappings (
    id SERIAL PRIMARY KEY,
    vendor_group VARCHAR(50) NOT NULL UNIQUE,     -- Maximo bpivendorgroup (e.g. '01', '02', '03', '04', '05')
    bp_grouping VARCHAR(50) NOT NULL DEFAULT 'Z001', -- SAP BusinessPartnerGrouping (e.g. 'Z001', 'Z002')
    company_code VARCHAR(20) DEFAULT '5110',
    recon_account VARCHAR(50) NOT NULL,           -- SAP Reconciliation Account (e.g. '211001', '211003')
    description VARCHAR(255),
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Seed dari Matriks Pemetaan SAP BP Grouping & Recon Account (Gambar 2)
INSERT INTO recon_account_mappings (vendor_group, bp_grouping, company_code, recon_account, description, is_active)
VALUES 
    ('01', 'Z001', '5110', '211001', 'Vendor Type 01 - Trade Payables (Z001)', true),
    ('02', 'Z002', '5110', '211003', 'Vendor Type 02 - Trade Payables (Z002)', true),
    ('03', 'Z003', '5110', '211005', 'Vendor Type 03 - Trade Payables (Z003)', true),
    ('04', 'Z004', '5110', '211006', 'Vendor Type 04 - Trade Payables (Z004)', true),
    ('05', 'Z005', '5110', '',       'Vendor Type 05 - Trade Payables (Z005)', true)
ON CONFLICT (vendor_group) DO UPDATE 
SET bp_grouping = EXCLUDED.bp_grouping,
    recon_account = EXCLUDED.recon_account,
    company_code = EXCLUDED.company_code,
    description = EXCLUDED.description,
    updated_at = NOW();





-- 2. UOM Mappings (35 pairs)
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('BAG', 'BAG', 'UOM Mapping BAG -> BAG', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('BOT', 'BOT', 'UOM Mapping BOT -> BOT', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('BOX', 'BOX', 'UOM Mapping BOX -> BOX', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('CAN', 'CAN', 'UOM Mapping CAN -> CAN', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('CYL', 'CYL', 'UOM Mapping CYL -> CYL', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('DR', 'DR', 'UOM Mapping DR -> DR', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('EA', 'EA', 'UOM Mapping EA -> EA', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('LOT', 'LOT', 'UOM Mapping LOT -> LOT', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('PAC', 'PAC', 'UOM Mapping PAC -> PAC', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('PCS', 'PC', 'UOM Mapping PCS -> PC', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('PC', 'PC', 'UOM Mapping PC -> PC', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('PL', 'PL', 'UOM Mapping PL -> PL', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('PR', 'PR', 'UOM Mapping PR -> PR', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('ROL', 'ROL', 'UOM Mapping ROL -> ROL', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('SET', 'SET', 'UOM Mapping SET -> SET', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('SHT', 'SHT', 'UOM Mapping SHT -> SHT', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('TIN', 'TIN', 'UOM Mapping TIN -> TIN', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('TUB', 'TUB', 'UOM Mapping TUB -> TUB', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('UNIT', 'UNT', 'UOM Mapping UNIT -> UNT', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('CM', 'CM', 'UOM Mapping CM -> CM', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('L', 'L', 'UOM Mapping L -> L', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('LT', 'L', 'UOM Mapping LT -> L', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('LG', 'LG', 'UOM Mapping LG -> LG', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('M', 'M', 'UOM Mapping M -> M', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('M2', 'M2', 'UOM Mapping M2 -> M2', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('M3', 'M3', 'UOM Mapping M3 -> M3', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('KG', 'KG', 'UOM Mapping KG -> KG', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('TON', 'TON', 'UOM Mapping TON -> TON', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('GAL', 'GAL', 'UOM Mapping GAL -> GAL', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('Sack', 'SAK', 'UOM Mapping Sack -> SAK', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('Pail', 'PL', 'UOM Mapping Pail -> PL', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('Ls', 'LS', 'UOM Mapping Ls -> LS', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('Liter', 'L', 'UOM Mapping Liter -> L', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('LIC', 'LIC', 'UOM Mapping LIC -> LIC', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;
INSERT INTO uom_mappings (maximo_uom, sap_uom, description, is_active, created_at, updated_at) VALUES ('Pack', 'PAC', 'UOM Mapping Pack -> PAC', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (maximo_uom) DO UPDATE SET sap_uom = EXCLUDED.sap_uom, updated_at = CURRENT_TIMESTAMP;

-- 3. Material Config Mappings Table
CREATE TABLE IF NOT EXISTS material_config_mappings (
    id SERIAL PRIMARY KEY,
    config_type VARCHAR(50) NOT NULL,
    maximo_value VARCHAR(50) NOT NULL,
    sap_value VARCHAR(50) NOT NULL,
    description VARCHAR(255),
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_material_config UNIQUE (config_type, maximo_value)
);
CREATE INDEX IF NOT EXISTS idx_mat_cfg_type ON material_config_mappings(config_type);
CREATE INDEX IF NOT EXISTS idx_mat_cfg_maximo ON material_config_mappings(maximo_value);

-- Seed Material Config Mappings
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_TYPE', 'Item', 'ZITM', 'ITEM_TYPE Mapping: Item -> ZITM', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IN-01', 'N001', 'ITEM_GROUP Mapping: IN-01 -> N001', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('STOCK_TYPE', 'Stock', 'ZSTK', 'STOCK_TYPE Mapping: Stock -> ZSTK', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IN-03', 'N003', 'ITEM_GROUP Mapping: IN-03 -> N003', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('STOCK_TYPE', 'Non-stock', 'ZNST', 'STOCK_TYPE Mapping: Non-stock -> ZNST', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IN-04', 'N004', 'ITEM_GROUP Mapping: IN-04 -> N004', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IN-05', 'N005', 'ITEM_GROUP Mapping: IN-05 -> N005', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IN-06', 'N006', 'ITEM_GROUP Mapping: IN-06 -> N006', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IN-09', 'N009', 'ITEM_GROUP Mapping: IN-09 -> N009', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IN-10', 'N010', 'ITEM_GROUP Mapping: IN-10 -> N010', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IN-11', 'N011', 'ITEM_GROUP Mapping: IN-11 -> N011', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IS-01', 'S001', 'ITEM_GROUP Mapping: IS-01 -> S001', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IS-02', 'S002', 'ITEM_GROUP Mapping: IS-02 -> S002', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IS-03', 'S003', 'ITEM_GROUP Mapping: IS-03 -> S003', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IS-04', 'S004', 'ITEM_GROUP Mapping: IS-04 -> S004', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IS-06', 'S006', 'ITEM_GROUP Mapping: IS-06 -> S006', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IS-07', 'S007', 'ITEM_GROUP Mapping: IS-07 -> S007', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IS-08', 'S008', 'ITEM_GROUP Mapping: IS-08 -> S008', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IS-09', 'S009', 'ITEM_GROUP Mapping: IS-09 -> S009', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'IS-11', 'S011', 'ITEM_GROUP Mapping: IS-11 -> S011', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-001', 'G001', 'ITEM_GROUP Mapping: SG-001 -> G001', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-002', 'G002', 'ITEM_GROUP Mapping: SG-002 -> G002', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-003', 'G003', 'ITEM_GROUP Mapping: SG-003 -> G003', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-004', 'G004', 'ITEM_GROUP Mapping: SG-004 -> G004', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-005', 'G005', 'ITEM_GROUP Mapping: SG-005 -> G005', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-006', 'G006', 'ITEM_GROUP Mapping: SG-006 -> G006', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-007', 'G007', 'ITEM_GROUP Mapping: SG-007 -> G007', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-008', 'G008', 'ITEM_GROUP Mapping: SG-008 -> G008', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-009', 'G009', 'ITEM_GROUP Mapping: SG-009 -> G009', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-010', 'G010', 'ITEM_GROUP Mapping: SG-010 -> G010', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-011', 'G011', 'ITEM_GROUP Mapping: SG-011 -> G011', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-012', 'G012', 'ITEM_GROUP Mapping: SG-012 -> G012', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-013', 'G013', 'ITEM_GROUP Mapping: SG-013 -> G013', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-014', 'G014', 'ITEM_GROUP Mapping: SG-014 -> G014', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-015', 'G015', 'ITEM_GROUP Mapping: SG-015 -> G015', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-016', 'G016', 'ITEM_GROUP Mapping: SG-016 -> G016', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-017', 'G017', 'ITEM_GROUP Mapping: SG-017 -> G017', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-018', 'G018', 'ITEM_GROUP Mapping: SG-018 -> G018', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-019', 'G019', 'ITEM_GROUP Mapping: SG-019 -> G019', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-020', 'G020', 'ITEM_GROUP Mapping: SG-020 -> G020', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-021', 'G021', 'ITEM_GROUP Mapping: SG-021 -> G021', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-022', 'G022', 'ITEM_GROUP Mapping: SG-022 -> G022', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-023', 'G023', 'ITEM_GROUP Mapping: SG-023 -> G023', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-024', 'G024', 'ITEM_GROUP Mapping: SG-024 -> G024', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-025', 'G025', 'ITEM_GROUP Mapping: SG-025 -> G025', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-026', 'G026', 'ITEM_GROUP Mapping: SG-026 -> G026', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-027', 'G027', 'ITEM_GROUP Mapping: SG-027 -> G027', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-028', 'G028', 'ITEM_GROUP Mapping: SG-028 -> G028', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-029', 'G029', 'ITEM_GROUP Mapping: SG-029 -> G029', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-030', 'G030', 'ITEM_GROUP Mapping: SG-030 -> G030', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-031', 'G031', 'ITEM_GROUP Mapping: SG-031 -> G031', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-032', 'G032', 'ITEM_GROUP Mapping: SG-032 -> G032', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-033', 'G033', 'ITEM_GROUP Mapping: SG-033 -> G033', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-034', 'G034', 'ITEM_GROUP Mapping: SG-034 -> G034', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-035', 'G035', 'ITEM_GROUP Mapping: SG-035 -> G035', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-036', 'G036', 'ITEM_GROUP Mapping: SG-036 -> G036', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-037', 'G037', 'ITEM_GROUP Mapping: SG-037 -> G037', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-038', 'G038', 'ITEM_GROUP Mapping: SG-038 -> G038', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-039', 'G039', 'ITEM_GROUP Mapping: SG-039 -> G039', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-040', 'G040', 'ITEM_GROUP Mapping: SG-040 -> G040', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-041', 'G041', 'ITEM_GROUP Mapping: SG-041 -> G041', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-042', 'G042', 'ITEM_GROUP Mapping: SG-042 -> G042', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-043', 'G043', 'ITEM_GROUP Mapping: SG-043 -> G043', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-044', 'G044', 'ITEM_GROUP Mapping: SG-044 -> G044', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-045', 'G045', 'ITEM_GROUP Mapping: SG-045 -> G045', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-046', 'G046', 'ITEM_GROUP Mapping: SG-046 -> G046', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-047', 'G047', 'ITEM_GROUP Mapping: SG-047 -> G047', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-048', 'G048', 'ITEM_GROUP Mapping: SG-048 -> G048', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-049', 'G049', 'ITEM_GROUP Mapping: SG-049 -> G049', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-050', 'G050', 'ITEM_GROUP Mapping: SG-050 -> G050', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-051', 'G051', 'ITEM_GROUP Mapping: SG-051 -> G051', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-052', 'G052', 'ITEM_GROUP Mapping: SG-052 -> G052', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-053', 'G053', 'ITEM_GROUP Mapping: SG-053 -> G053', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-054', 'G054', 'ITEM_GROUP Mapping: SG-054 -> G054', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-055', 'G055', 'ITEM_GROUP Mapping: SG-055 -> G055', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-056', 'G056', 'ITEM_GROUP Mapping: SG-056 -> G056', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-057', 'G057', 'ITEM_GROUP Mapping: SG-057 -> G057', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-058', 'G058', 'ITEM_GROUP Mapping: SG-058 -> G058', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-059', 'G059', 'ITEM_GROUP Mapping: SG-059 -> G059', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-060', 'G060', 'ITEM_GROUP Mapping: SG-060 -> G060', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-061', 'G061', 'ITEM_GROUP Mapping: SG-061 -> G061', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-062', 'G062', 'ITEM_GROUP Mapping: SG-062 -> G062', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-063', 'G063', 'ITEM_GROUP Mapping: SG-063 -> G063', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-064', 'G064', 'ITEM_GROUP Mapping: SG-064 -> G064', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-065', 'G065', 'ITEM_GROUP Mapping: SG-065 -> G065', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-066', 'G066', 'ITEM_GROUP Mapping: SG-066 -> G066', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-067', 'G067', 'ITEM_GROUP Mapping: SG-067 -> G067', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-068', 'G068', 'ITEM_GROUP Mapping: SG-068 -> G068', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-069', 'G069', 'ITEM_GROUP Mapping: SG-069 -> G069', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-070', 'G070', 'ITEM_GROUP Mapping: SG-070 -> G070', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-071', 'G071', 'ITEM_GROUP Mapping: SG-071 -> G071', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-072', 'G072', 'ITEM_GROUP Mapping: SG-072 -> G072', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-073', 'G073', 'ITEM_GROUP Mapping: SG-073 -> G073', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-074', 'G074', 'ITEM_GROUP Mapping: SG-074 -> G074', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-075', 'G075', 'ITEM_GROUP Mapping: SG-075 -> G075', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-076', 'G076', 'ITEM_GROUP Mapping: SG-076 -> G076', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-077', 'G077', 'ITEM_GROUP Mapping: SG-077 -> G077', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-078', 'G078', 'ITEM_GROUP Mapping: SG-078 -> G078', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-079', 'G079', 'ITEM_GROUP Mapping: SG-079 -> G079', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-080', 'G080', 'ITEM_GROUP Mapping: SG-080 -> G080', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-081', 'G081', 'ITEM_GROUP Mapping: SG-081 -> G081', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-082', 'G082', 'ITEM_GROUP Mapping: SG-082 -> G082', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-083', 'G083', 'ITEM_GROUP Mapping: SG-083 -> G083', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-084', 'G084', 'ITEM_GROUP Mapping: SG-084 -> G084', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-085', 'G085', 'ITEM_GROUP Mapping: SG-085 -> G085', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-086', 'G086', 'ITEM_GROUP Mapping: SG-086 -> G086', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-087', 'G087', 'ITEM_GROUP Mapping: SG-087 -> G087', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SG-088', 'G088', 'ITEM_GROUP Mapping: SG-088 -> G088', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-001', 'O001', 'ITEM_GROUP Mapping: SO-001 -> O001', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-002', 'O002', 'ITEM_GROUP Mapping: SO-002 -> O002', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-003', 'O003', 'ITEM_GROUP Mapping: SO-003 -> O003', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-004', 'O004', 'ITEM_GROUP Mapping: SO-004 -> O004', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-005', 'O005', 'ITEM_GROUP Mapping: SO-005 -> O005', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-006', 'O006', 'ITEM_GROUP Mapping: SO-006 -> O006', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-007', 'O007', 'ITEM_GROUP Mapping: SO-007 -> O007', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-008', 'O008', 'ITEM_GROUP Mapping: SO-008 -> O008', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-009', 'O009', 'ITEM_GROUP Mapping: SO-009 -> O009', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-010', 'O010', 'ITEM_GROUP Mapping: SO-010 -> O010', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-011', 'O011', 'ITEM_GROUP Mapping: SO-011 -> O011', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-012', 'O012', 'ITEM_GROUP Mapping: SO-012 -> O012', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-013', 'O013', 'ITEM_GROUP Mapping: SO-013 -> O013', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-014', 'O014', 'ITEM_GROUP Mapping: SO-014 -> O014', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-015', 'O015', 'ITEM_GROUP Mapping: SO-015 -> O015', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-016', 'O016', 'ITEM_GROUP Mapping: SO-016 -> O016', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-017', 'O017', 'ITEM_GROUP Mapping: SO-017 -> O017', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-018', 'O018', 'ITEM_GROUP Mapping: SO-018 -> O018', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-019', 'O019', 'ITEM_GROUP Mapping: SO-019 -> O019', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-020', 'O020', 'ITEM_GROUP Mapping: SO-020 -> O020', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-021', 'O021', 'ITEM_GROUP Mapping: SO-021 -> O021', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-022', 'O022', 'ITEM_GROUP Mapping: SO-022 -> O022', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-023', 'O023', 'ITEM_GROUP Mapping: SO-023 -> O023', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-024', 'O024', 'ITEM_GROUP Mapping: SO-024 -> O024', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-025', 'O025', 'ITEM_GROUP Mapping: SO-025 -> O025', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-026', 'O026', 'ITEM_GROUP Mapping: SO-026 -> O026', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-027', 'O027', 'ITEM_GROUP Mapping: SO-027 -> O027', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-028', 'O028', 'ITEM_GROUP Mapping: SO-028 -> O028', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-029', 'O029', 'ITEM_GROUP Mapping: SO-029 -> O029', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-030', 'O030', 'ITEM_GROUP Mapping: SO-030 -> O030', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-031', 'O031', 'ITEM_GROUP Mapping: SO-031 -> O031', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-032', 'O032', 'ITEM_GROUP Mapping: SO-032 -> O032', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-033', 'O033', 'ITEM_GROUP Mapping: SO-033 -> O033', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-034', 'O034', 'ITEM_GROUP Mapping: SO-034 -> O034', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-035', 'O035', 'ITEM_GROUP Mapping: SO-035 -> O035', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-036', 'O036', 'ITEM_GROUP Mapping: SO-036 -> O036', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-037', 'O037', 'ITEM_GROUP Mapping: SO-037 -> O037', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-038', 'O038', 'ITEM_GROUP Mapping: SO-038 -> O038', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-039', 'O039', 'ITEM_GROUP Mapping: SO-039 -> O039', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-040', 'O040', 'ITEM_GROUP Mapping: SO-040 -> O040', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-041', 'O041', 'ITEM_GROUP Mapping: SO-041 -> O041', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-042', 'O042', 'ITEM_GROUP Mapping: SO-042 -> O042', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-043', 'O043', 'ITEM_GROUP Mapping: SO-043 -> O043', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-044', 'O044', 'ITEM_GROUP Mapping: SO-044 -> O044', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-045', 'O045', 'ITEM_GROUP Mapping: SO-045 -> O045', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-046', 'O046', 'ITEM_GROUP Mapping: SO-046 -> O046', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-047', 'O047', 'ITEM_GROUP Mapping: SO-047 -> O047', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-048', 'O048', 'ITEM_GROUP Mapping: SO-048 -> O048', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-049', 'O049', 'ITEM_GROUP Mapping: SO-049 -> O049', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-050', 'O050', 'ITEM_GROUP Mapping: SO-050 -> O050', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-051', 'O051', 'ITEM_GROUP Mapping: SO-051 -> O051', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-052', 'O052', 'ITEM_GROUP Mapping: SO-052 -> O052', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-053', 'O053', 'ITEM_GROUP Mapping: SO-053 -> O053', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-054', 'O054', 'ITEM_GROUP Mapping: SO-054 -> O054', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-055', 'O055', 'ITEM_GROUP Mapping: SO-055 -> O055', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-057', 'O057', 'ITEM_GROUP Mapping: SO-057 -> O057', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-061', 'O061', 'ITEM_GROUP Mapping: SO-061 -> O061', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-062', 'O062', 'ITEM_GROUP Mapping: SO-062 -> O062', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-063', 'O063', 'ITEM_GROUP Mapping: SO-063 -> O063', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-064', 'O064', 'ITEM_GROUP Mapping: SO-064 -> O064', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-065', 'O065', 'ITEM_GROUP Mapping: SO-065 -> O065', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-066', 'O066', 'ITEM_GROUP Mapping: SO-066 -> O066', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-067', 'O067', 'ITEM_GROUP Mapping: SO-067 -> O067', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-068', 'O068', 'ITEM_GROUP Mapping: SO-068 -> O068', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-069', 'O069', 'ITEM_GROUP Mapping: SO-069 -> O069', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-070', 'O070', 'ITEM_GROUP Mapping: SO-070 -> O070', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-071', 'O071', 'ITEM_GROUP Mapping: SO-071 -> O071', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-072', 'O072', 'ITEM_GROUP Mapping: SO-072 -> O072', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-073', 'O073', 'ITEM_GROUP Mapping: SO-073 -> O073', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-074', 'O074', 'ITEM_GROUP Mapping: SO-074 -> O074', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-075', 'O075', 'ITEM_GROUP Mapping: SO-075 -> O075', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-076', 'O076', 'ITEM_GROUP Mapping: SO-076 -> O076', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-077', 'O077', 'ITEM_GROUP Mapping: SO-077 -> O077', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-078', 'O078', 'ITEM_GROUP Mapping: SO-078 -> O078', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-079', 'O079', 'ITEM_GROUP Mapping: SO-079 -> O079', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-080', 'O080', 'ITEM_GROUP Mapping: SO-080 -> O080', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-081', 'O081', 'ITEM_GROUP Mapping: SO-081 -> O081', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-082', 'O082', 'ITEM_GROUP Mapping: SO-082 -> O082', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-083', 'O083', 'ITEM_GROUP Mapping: SO-083 -> O083', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-084', 'O084', 'ITEM_GROUP Mapping: SO-084 -> O084', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-085', 'O085', 'ITEM_GROUP Mapping: SO-085 -> O085', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-086', 'O086', 'ITEM_GROUP Mapping: SO-086 -> O086', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-087', 'O087', 'ITEM_GROUP Mapping: SO-087 -> O087', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-088', 'O088', 'ITEM_GROUP Mapping: SO-088 -> O088', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-090', 'O090', 'ITEM_GROUP Mapping: SO-090 -> O090', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-091', 'O091', 'ITEM_GROUP Mapping: SO-091 -> O091', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-092', 'O092', 'ITEM_GROUP Mapping: SO-092 -> O092', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-093', 'O093', 'ITEM_GROUP Mapping: SO-093 -> O093', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-094', 'O094', 'ITEM_GROUP Mapping: SO-094 -> O094', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-095', 'O095', 'ITEM_GROUP Mapping: SO-095 -> O095', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-096', 'O096', 'ITEM_GROUP Mapping: SO-096 -> O096', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-097', 'O097', 'ITEM_GROUP Mapping: SO-097 -> O097', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-098', 'O098', 'ITEM_GROUP Mapping: SO-098 -> O098', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-099', 'O099', 'ITEM_GROUP Mapping: SO-099 -> O099', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-100', 'O100', 'ITEM_GROUP Mapping: SO-100 -> O100', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-101', 'O101', 'ITEM_GROUP Mapping: SO-101 -> O101', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-102', 'O102', 'ITEM_GROUP Mapping: SO-102 -> O102', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-103', 'O103', 'ITEM_GROUP Mapping: SO-103 -> O103', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-104', 'O104', 'ITEM_GROUP Mapping: SO-104 -> O104', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-105', 'O105', 'ITEM_GROUP Mapping: SO-105 -> O105', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-106', 'O106', 'ITEM_GROUP Mapping: SO-106 -> O106', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-107', 'O107', 'ITEM_GROUP Mapping: SO-107 -> O107', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-108', 'O108', 'ITEM_GROUP Mapping: SO-108 -> O108', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-109', 'O109', 'ITEM_GROUP Mapping: SO-109 -> O109', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-110', 'O110', 'ITEM_GROUP Mapping: SO-110 -> O110', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-111', 'O111', 'ITEM_GROUP Mapping: SO-111 -> O111', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-112', 'O112', 'ITEM_GROUP Mapping: SO-112 -> O112', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-113', 'O113', 'ITEM_GROUP Mapping: SO-113 -> O113', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-114', 'O114', 'ITEM_GROUP Mapping: SO-114 -> O114', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-115', 'O115', 'ITEM_GROUP Mapping: SO-115 -> O115', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-116', 'O116', 'ITEM_GROUP Mapping: SO-116 -> O116', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-117', 'O117', 'ITEM_GROUP Mapping: SO-117 -> O117', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-118', 'O118', 'ITEM_GROUP Mapping: SO-118 -> O118', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-119', 'O119', 'ITEM_GROUP Mapping: SO-119 -> O119', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-120', 'O120', 'ITEM_GROUP Mapping: SO-120 -> O120', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-121', 'O121', 'ITEM_GROUP Mapping: SO-121 -> O121', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-122', 'O122', 'ITEM_GROUP Mapping: SO-122 -> O122', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-123', 'O123', 'ITEM_GROUP Mapping: SO-123 -> O123', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-124', 'O124', 'ITEM_GROUP Mapping: SO-124 -> O124', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-125', 'O125', 'ITEM_GROUP Mapping: SO-125 -> O125', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-126', 'O126', 'ITEM_GROUP Mapping: SO-126 -> O126', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-127', 'O127', 'ITEM_GROUP Mapping: SO-127 -> O127', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-128', 'O128', 'ITEM_GROUP Mapping: SO-128 -> O128', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-129', 'O129', 'ITEM_GROUP Mapping: SO-129 -> O129', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-130', 'O130', 'ITEM_GROUP Mapping: SO-130 -> O130', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-131', 'O131', 'ITEM_GROUP Mapping: SO-131 -> O131', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-132', 'O132', 'ITEM_GROUP Mapping: SO-132 -> O132', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-133', 'O133', 'ITEM_GROUP Mapping: SO-133 -> O133', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-134', 'O134', 'ITEM_GROUP Mapping: SO-134 -> O134', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-135', 'O135', 'ITEM_GROUP Mapping: SO-135 -> O135', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-136', 'O136', 'ITEM_GROUP Mapping: SO-136 -> O136', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-137', 'O137', 'ITEM_GROUP Mapping: SO-137 -> O137', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-138', 'O138', 'ITEM_GROUP Mapping: SO-138 -> O138', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-139', 'O139', 'ITEM_GROUP Mapping: SO-139 -> O139', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-140', 'O140', 'ITEM_GROUP Mapping: SO-140 -> O140', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-201', 'O201', 'ITEM_GROUP Mapping: SO-201 -> O201', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-202', 'O202', 'ITEM_GROUP Mapping: SO-202 -> O202', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-203', 'O203', 'ITEM_GROUP Mapping: SO-203 -> O203', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-204', 'O204', 'ITEM_GROUP Mapping: SO-204 -> O204', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-205', 'O205', 'ITEM_GROUP Mapping: SO-205 -> O205', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-206', 'O206', 'ITEM_GROUP Mapping: SO-206 -> O206', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-207', 'O207', 'ITEM_GROUP Mapping: SO-207 -> O207', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-208', 'O208', 'ITEM_GROUP Mapping: SO-208 -> O208', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-209', 'O209', 'ITEM_GROUP Mapping: SO-209 -> O209', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-210', 'O210', 'ITEM_GROUP Mapping: SO-210 -> O210', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-211', 'O211', 'ITEM_GROUP Mapping: SO-211 -> O211', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-212', 'O212', 'ITEM_GROUP Mapping: SO-212 -> O212', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-213', 'O213', 'ITEM_GROUP Mapping: SO-213 -> O213', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-214', 'O214', 'ITEM_GROUP Mapping: SO-214 -> O214', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-215', 'O215', 'ITEM_GROUP Mapping: SO-215 -> O215', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-216', 'O216', 'ITEM_GROUP Mapping: SO-216 -> O216', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-217', 'O217', 'ITEM_GROUP Mapping: SO-217 -> O217', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-218', 'O218', 'ITEM_GROUP Mapping: SO-218 -> O218', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-219', 'O219', 'ITEM_GROUP Mapping: SO-219 -> O219', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-220', 'O220', 'ITEM_GROUP Mapping: SO-220 -> O220', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-221', 'O221', 'ITEM_GROUP Mapping: SO-221 -> O221', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-222', 'O222', 'ITEM_GROUP Mapping: SO-222 -> O222', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-223', 'O223', 'ITEM_GROUP Mapping: SO-223 -> O223', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-224', 'O224', 'ITEM_GROUP Mapping: SO-224 -> O224', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-225', 'O225', 'ITEM_GROUP Mapping: SO-225 -> O225', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-226', 'O226', 'ITEM_GROUP Mapping: SO-226 -> O226', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-227', 'O227', 'ITEM_GROUP Mapping: SO-227 -> O227', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_GROUP', 'SO-228', 'O228', 'ITEM_GROUP Mapping: SO-228 -> O228', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('ITEM_TYPE', 'Service', 'ZSVC', 'ITEM_TYPE Mapping: Service -> ZSVC', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
INSERT INTO material_config_mappings (config_type, maximo_value, sap_value, description, is_active, created_at, updated_at) VALUES ('STOCK_TYPE', 'Service', 'ZSVC', 'STOCK_TYPE Mapping: Service -> ZSVC', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (config_type, maximo_value) DO UPDATE SET sap_value = EXCLUDED.sap_value, updated_at = CURRENT_TIMESTAMP;
