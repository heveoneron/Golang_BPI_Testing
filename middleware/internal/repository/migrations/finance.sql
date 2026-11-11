-- ============================================================================
-- MIGRASI RICEF 1-9 (FINANCE INTEGRATION)
-- Dijalankan otomatis oleh Engine saat start (idempotent).
-- ============================================================================

-- RICEF 1: catatan supplement / transfer budget
CREATE TABLE IF NOT EXISTS budget_adjustments (
    id BIGSERIAL PRIMARY KEY,
    document_no VARCHAR(50) NOT NULL UNIQUE,
    adjustment_type VARCHAR(20) NOT NULL,          -- SUPPLEMENT, TRANSFER
    budget_year INT NOT NULL,
    from_combination VARCHAR(50),
    to_combination VARCHAR(50) NOT NULL,
    amount NUMERIC(18, 2) NOT NULL,
    reason TEXT,
    requested_by VARCHAR(100),
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING', -- PENDING, APPLIED, FAILED
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Lock jurnal per kombinasi memakai reference "<referensi>|<costcenter-budgetcode>"
ALTER TABLE budget_transaction_locks ALTER COLUMN reference_doc_no TYPE VARCHAR(120);

-- RICEF 3 & 4: GL Account Line Item dari SAP
CREATE TABLE IF NOT EXISTS sap_gl_line_items (
    gl_line_item_key VARCHAR(200) PRIMARY KEY,
    company_code VARCHAR(10),
    fiscal_year VARCHAR(4),
    accounting_document VARCHAR(20),
    ledger_gl_line_item VARCHAR(10),
    accounting_document_type VARCHAR(5),
    posting_date DATE,
    document_date DATE,
    gl_account VARCHAR(20),
    cost_center VARCHAR(20),
    profit_center VARCHAR(20),
    supplier VARCHAR(20),
    debit_credit_code VARCHAR(1),
    amount_in_company_code_currency NUMERIC(23, 2),
    company_code_currency VARCHAR(5),
    amount_in_transaction_currency NUMERIC(23, 2),
    transaction_currency VARCHAR(5),
    document_item_text TEXT,
    raw JSONB NOT NULL,
    synced_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_gl_items_doc ON sap_gl_line_items(company_code, fiscal_year, accounting_document);
CREATE INDEX IF NOT EXISTS idx_gl_items_type_date ON sap_gl_line_items(accounting_document_type, posting_date);

-- RICEF 5: Payment Proposal dari SAP (CDS custom)
CREATE TABLE IF NOT EXISTS sap_payment_proposals (
    record_key VARCHAR(400) PRIMARY KEY,
    payment_run_id VARCHAR(20),
    payment_run_date DATE,
    company_code VARCHAR(10),
    supplier VARCHAR(20),
    payment_document VARCHAR(20),
    amount NUMERIC(23, 2),
    currency VARCHAR(5),
    raw JSONB NOT NULL,
    synced_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_payment_run ON sap_payment_proposals(payment_run_date, payment_run_id);

-- RICEF 9: Fixed Asset master dari SAP
CREATE TABLE IF NOT EXISTS sap_fixed_assets (
    company_code VARCHAR(10) NOT NULL,
    master_fixed_asset VARCHAR(20) NOT NULL,
    fixed_asset VARCHAR(10) NOT NULL,
    description TEXT,
    asset_class VARCHAR(20),
    capitalization_date DATE,
    raw JSONB NOT NULL,
    synced_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (company_code, master_fixed_asset, fixed_asset)
);

-- RICEF 8: Parking & approval jurnal pengakuan aset
CREATE TABLE IF NOT EXISTS asset_acquisitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reference_no VARCHAR(50) NOT NULL UNIQUE,
    company_code VARCHAR(10) NOT NULL,
    master_fixed_asset VARCHAR(20) NOT NULL,
    fixed_asset VARCHAR(10) NOT NULL DEFAULT '0',
    document_date DATE NOT NULL,
    posting_date DATE NOT NULL,
    asset_value_date DATE NOT NULL,
    amount NUMERIC(18, 2) NOT NULL,
    currency VARCHAR(5) NOT NULL,
    offsetting_account VARCHAR(20) NOT NULL,
    item_text TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'PARKED', -- PARKED, APPROVED, POSTED, FAILED, REJECTED
    parked_by VARCHAR(100) NOT NULL,
    approved_by VARCHAR(100),
    approved_at TIMESTAMP WITH TIME ZONE,
    rejected_by VARCHAR(100),
    reject_reason TEXT,
    sap_document_no VARCHAR(50),
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_asset_acq_status ON asset_acquisitions(status);

-- RICEF 1: atribut pagu tambahan (dokumen "BPI_MAPPING_API_MAXIMO_to_SAP (FinanceModule)", sheet Budget)
-- budget_combination berformat [MainAccount]-[BudgetCode], contoh 513043-28011.
ALTER TABLE budget_masters ADD COLUMN IF NOT EXISTS budget_code VARCHAR(20);
ALTER TABLE budget_masters ADD COLUMN IF NOT EXISTS budget_type VARCHAR(3);          -- OPX / CPX
ALTER TABLE budget_masters ADD COLUMN IF NOT EXISTS remaining_budget_payment_based NUMERIC(18, 2);

-- RICEF 7: mapping kebijakan reimbursement -> GL account & budget code
-- (dokumen "Mapping Table Reimbers & Business trip to golang", sheet BT)
CREATE TABLE IF NOT EXISTS reimbursement_gl_mappings (
    id SERIAL PRIMARY KEY,
    policy_name VARCHAR(150) NOT NULL,
    policy_key VARCHAR(150) NOT NULL,              -- nama ternormalisasi untuk pencocokan
    cip_flag VARCHAR(10) NOT NULL,                 -- CIP / NON_CIP
    gl_account VARCHAR(20) NOT NULL,
    budget_code VARCHAR(20) NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT uq_reimb_policy UNIQUE (policy_key, cip_flag)
);

-- RICEF 7: vendor karyawan -> status CIP / Non CIP & Outbound Master Data Vendor
CREATE TABLE IF NOT EXISTS employee_vendor_mappings (
    vendor_code VARCHAR(50) PRIMARY KEY,
    cip_flag VARCHAR(20) NOT NULL,                 -- CIP / NON_CIP
    email VARCHAR(150),
    bank_key VARCHAR(100),
    bank_account VARCHAR(100),
    account_holder VARCHAR(150),
    description VARCHAR(255),
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
ALTER TABLE employee_vendor_mappings ADD COLUMN IF NOT EXISTS email VARCHAR(150);
ALTER TABLE employee_vendor_mappings ADD COLUMN IF NOT EXISTS bank_key VARCHAR(100);
ALTER TABLE employee_vendor_mappings ADD COLUMN IF NOT EXISTS bank_account VARCHAR(100);
ALTER TABLE employee_vendor_mappings ADD COLUMN IF NOT EXISTS account_holder VARCHAR(150);

-- Seed Outbound Master Data Vendor
INSERT INTO employee_vendor_mappings (vendor_code, cip_flag, email, bank_key, bank_account, account_holder, description, is_active)
VALUES ('VND-0009', 'CIP', 'mrauff341@gmail.com', 'bank key1', '1010101', 'Mandiri', 'Vendor Sample Mandiri', true)
ON CONFLICT (vendor_code) DO UPDATE SET 
    cip_flag = EXCLUDED.cip_flag,
    email = EXCLUDED.email,
    bank_key = EXCLUDED.bank_key,
    bank_account = EXCLUDED.bank_account,
    account_holder = EXCLUDED.account_holder,
    updated_at = NOW();

INSERT INTO employee_vendor_mappings (vendor_code, cip_flag, email, bank_key, bank_account, account_holder, description, is_active)
VALUES ('VND-0061', 'NON_CIP', 'soltius@gmail.com', 'MGAJKT001', '1202228837', 'PT.BANK MUAMALAT INDONESIA TBK', 'PT Chevron Indonesia', true)
ON CONFLICT (vendor_code) DO NOTHING;

-- SAP -> Maximo: supplier invoice yang ditarik dari SAP (API_SUPPLIERINVOICE_PROCESS_SRV)
CREATE TABLE IF NOT EXISTS sap_supplier_invoices (
    supplier_invoice VARCHAR(10) NOT NULL,
    fiscal_year VARCHAR(4) NOT NULL,
    company_code VARCHAR(10),
    posting_date DATE,
    raw JSONB NOT NULL,
    synced_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (supplier_invoice, fiscal_year)
);

-- SAP -> Maximo: riwayat penulisan ke dbo.mxinvoice (mencegah dokumen ditulis dua kali)
CREATE TABLE IF NOT EXISTS maximo_invoice_exports (
    source_type VARCHAR(10) NOT NULL,             -- INVOICE / JOURNAL
    source_key VARCHAR(60) NOT NULL,
    status VARCHAR(10) NOT NULL,                  -- EXPORTED / FAILED
    line_count INT DEFAULT 0,
    trans_ids TEXT,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (source_type, source_key)
);

-- RICEF 7: Riwayat upload Excel
CREATE TABLE IF NOT EXISTS excel_upload_batches (
    id BIGSERIAL PRIMARY KEY,
    file_name VARCHAR(255),
    uploaded_by VARCHAR(100),
    total_rows INT DEFAULT 0,
    total_invoices INT DEFAULT 0,
    status VARCHAR(20) NOT NULL,                  -- QUEUED, REJECTED
    errors JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);


-- DJ-MM-ENH-ZME003: Pemetaan Valuation Class ke Akun GL & Cost Center (Goods Issue & Stock Adjustment)
CREATE TABLE IF NOT EXISTS valuation_class_gl_mappings (
    id BIGSERIAL PRIMARY KEY,
    valuation_class VARCHAR(20) NOT NULL UNIQUE,
    inventory_posting_gl VARCHAR(30) NOT NULL,
    goods_issue_gl VARCHAR(30) NOT NULL,
    stock_adj_gl VARCHAR(30) NOT NULL,
    cost_center VARCHAR(30) NOT NULL,
    description VARCHAR(100),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_val_class_gl ON valuation_class_gl_mappings(valuation_class);

-- RICEF 9: AssetAdditionalDescription
ALTER TABLE sap_fixed_assets ADD COLUMN IF NOT EXISTS additional_description VARCHAR(100);

-- RICEF 8: Approve/Reject hanya setelah Print Form (RICEF 11) dicetak
CREATE TABLE IF NOT EXISTS asset_acquisition_prints (
    id BIGSERIAL PRIMARY KEY,
    acquisition_id UUID NOT NULL REFERENCES asset_acquisitions(id) ON DELETE CASCADE,
    printed_by VARCHAR(100) NOT NULL,
    printed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_asset_acq_prints ON asset_acquisition_prints(acquisition_id);
