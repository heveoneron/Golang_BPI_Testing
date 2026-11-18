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

-- Outbound Master Data Vendor: multiple bank accounts per vendor (11 Field OData to_BusinessPartnerBank)
CREATE TABLE IF NOT EXISTS vendor_bank_accounts (
    id BIGSERIAL PRIMARY KEY,
    vendor_code VARCHAR(50) NOT NULL REFERENCES employee_vendor_mappings(vendor_code) ON DELETE CASCADE,
    bank_identification VARCHAR(20) NOT NULL,        -- 4. <d:BankIdentification>
    bank_country_key VARCHAR(10),                    -- 5. <d:BankCountryKey>
    bank_number VARCHAR(50),                         -- 1. d:BankNumber (Bank Key)
    bank_account VARCHAR(100) NOT NULL,              -- 2. d:BankAccount (Bank Account)
    swift_code VARCHAR(50),                          -- 6. <d:SWIFTCode>
    bank_name VARCHAR(150),                          -- 7. <d:BankName>
    bank_control_key VARCHAR(20),                    -- 8. <d:BankControlKey>
    iban VARCHAR(50),                                -- 9. <d:IBAN>
    bank_account_reference_text VARCHAR(255),        -- 10. <d:BankAccountReferenceText>
    bank_account_holder_name VARCHAR(150),           -- 3. d:BankAccountHolderName
    bank_account_name VARCHAR(150),                  -- 11. <d:BankAccountName>
    is_default BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT uq_vendor_bank_account UNIQUE (vendor_code, bank_identification)
);
CREATE INDEX IF NOT EXISTS idx_vendor_bank_vendor ON vendor_bank_accounts(vendor_code);

-- Seed Outbound Master Data Vendor Header
INSERT INTO employee_vendor_mappings (vendor_code, cip_flag, email, bank_key, bank_account, account_holder, description, is_active)
VALUES ('VND-0009', 'CIP', 'mrauff341@gmail.com', 'bank key1', '1010101', 'Mandiri', 'Vendor Sample Multi-Bank Mandiri/BNI/Muamalat', true)
ON CONFLICT (vendor_code) DO NOTHING;

INSERT INTO employee_vendor_mappings (vendor_code, cip_flag, email, bank_key, bank_account, account_holder, description, is_active)
VALUES ('VND-0061', 'NON_CIP', 'soltius@gmail.com', 'MGAJKT001', '1202228837', 'PT.BANK MUAMALAT INDONESIA TBK', 'PT Chevron Indonesia', true)
ON CONFLICT (vendor_code) DO NOTHING;

-- Seed Multiple Bank Accounts untuk VND-0009 (sesuai contoh data)
INSERT INTO vendor_bank_accounts (
    vendor_code, bank_identification, bank_country_key, bank_number, bank_account,
    swift_code, bank_name, bank_control_key, iban, bank_account_reference_text,
    bank_account_holder_name, bank_account_name, is_default
) VALUES 
('VND-0009', '0001', 'ID', 'bank key1', '1010101', 'BMRIIDJASTL', 'Bank 2 - SAMPLE BANK', 'CO', '', 'Reference', 'Mandiri', 'PT. Chevron', true),
('VND-0009', '0002', 'ID', 'BK2', '20202020', 'BMRIIDJASTL', 'Bank 2 - SAMPLE BANK', 'CO', '', 'Reference 2', 'BNI', 'PT. Bhimasena', false),
('VND-0009', '0003', 'ID', 'BK3', '3030303', 'BMRIIDJASTL', 'Bank 2 - SAMPLE BANK', 'CO', '', 'Reference 3', 'Muamalat', 'PT. Oligarki', false),
('VND-0061', '0001', 'ID', 'MGAJKT001', '1202228837', 'MEGAIDJA', 'PT.BANK MUAMALAT INDONESIA TBK', 'CO', '', 'Ref Chevron', 'PT.BANK MUAMALAT INDONESIA TBK', 'PT Chevron Indonesia', true)
ON CONFLICT (vendor_code, bank_identification) DO UPDATE SET
    bank_country_key = EXCLUDED.bank_country_key,
    bank_number = EXCLUDED.bank_number,
    bank_account = EXCLUDED.bank_account,
    swift_code = EXCLUDED.swift_code,
    bank_name = EXCLUDED.bank_name,
    bank_control_key = EXCLUDED.bank_control_key,
    iban = EXCLUDED.iban,
    bank_account_reference_text = EXCLUDED.bank_account_reference_text,
    bank_account_holder_name = EXCLUDED.bank_account_holder_name,
    bank_account_name = EXCLUDED.bank_account_name,
    is_default = EXCLUDED.is_default,
    updated_at = NOW();

-- Seed Sample GL Document & 7 Line Items (Gambar 3 & 4)
INSERT INTO sap_gl_line_items (
    gl_line_item_key, company_code, fiscal_year, accounting_document, ledger_gl_line_item,
    accounting_document_type, posting_date, document_date, gl_account, cost_center, profit_center,
    supplier, debit_credit_code, amount_in_company_code_currency, company_code_currency,
    amount_in_transaction_currency, transaction_currency, document_item_text, raw, synced_at
) VALUES 
('1000~2026~1500000017~000001', '1000', '2026', '1500000017', '1', 'KZ', '2026-10-11', '2026-10-11', '11020101', '1030', '', '', 'S', 15000000.00, 'IDR', 15000000.00, 'IDR', 'pop up item 1', '{"CompanyCode":"1000","FiscalYear":"2026","AccountingDocument":"1500000017","AccountingDocumentItem":"1","CompanyCodeCurrency":"IDR","GlobalCurrency":"IDR","BaseUnit":"","Quantity":0,"Product":"","GLAccount":"11020101","CostCenter":"1030","ProfitCenter":"","FunctionalArea":"","AmountInTransactionCurrency":15000000.00,"AmountInCompanyCodeCurrency":15000000.00,"AmountInGlobalCurrency":15000000.00,"DebitCreditCode":"S","AssignmentReference":"pop up","ReversalReferenceDocument":"","ReversalReferenceDocumentItem":"","DocumentItemText":"pop up item 1","Supplier":"","Customer":"","FinancialAccountType":"S","SpecialGLCode":"","HouseBank":"","HouseBankAccount":"","MasterFixedAsset":"","FixedAsset":"","WithholdingTaxCode":"","FiscalPeriod":"10","PostingDate":"/Date(1791676800000)/","DocumentDate":"/Date(1791676800000)/","AccountingDocumentType":"KZ","AccountingDocumentHeaderText":"Pembayaran Vendor","Reference":"REF-1500000017","AccountingDocCreatedByUser":"SAPUSER","CreationDateTime":"11/10/2026 10:04","FlaggingSendToMaximo":"X","TimeStampSendToMaximo":""}'::jsonb, NOW()),
('1000~2026~1500000017~000002', '1000', '2026', '1500000017', '2', 'KZ', '2026-10-11', '2026-10-11', '11020101', '1038', '', '', 'H', 5000000.00, 'IDR', 5000000.00, 'IDR', 'pop up item 2', '{"CompanyCode":"1000","FiscalYear":"2026","AccountingDocument":"1500000017","AccountingDocumentItem":"2","CompanyCodeCurrency":"IDR","GlobalCurrency":"IDR","BaseUnit":"","Quantity":0,"Product":"","GLAccount":"11020101","CostCenter":"1038","ProfitCenter":"","FunctionalArea":"","AmountInTransactionCurrency":5000000.00,"AmountInCompanyCodeCurrency":5000000.00,"AmountInGlobalCurrency":5000000.00,"DebitCreditCode":"H","AssignmentReference":"pop up","ReversalReferenceDocument":"","ReversalReferenceDocumentItem":"","DocumentItemText":"pop up item 2","Supplier":"","Customer":"","FinancialAccountType":"S","SpecialGLCode":"","HouseBank":"","HouseBankAccount":"","MasterFixedAsset":"","FixedAsset":"","WithholdingTaxCode":"","FiscalPeriod":"10","PostingDate":"/Date(1791676800000)/","DocumentDate":"/Date(1791676800000)/","AccountingDocumentType":"KZ","AccountingDocumentHeaderText":"Pembayaran Vendor","Reference":"REF-1500000017","AccountingDocCreatedByUser":"SAPUSER","CreationDateTime":"11/10/2026 10:04","FlaggingSendToMaximo":"X","TimeStampSendToMaximo":""}'::jsonb, NOW()),
('1000~2026~1500000017~000003', '1000', '2026', '1500000017', '3', 'KZ', '2026-10-11', '2026-10-11', '11020101', '1039', '', '', 'H', 10000000.00, 'IDR', 10000000.00, 'IDR', 'pop up item 3', '{"CompanyCode":"1000","FiscalYear":"2026","AccountingDocument":"1500000017","AccountingDocumentItem":"3","CompanyCodeCurrency":"IDR","GlobalCurrency":"IDR","BaseUnit":"","Quantity":0,"Product":"","GLAccount":"11020101","CostCenter":"1039","ProfitCenter":"","FunctionalArea":"","AmountInTransactionCurrency":10000000.00,"AmountInCompanyCodeCurrency":10000000.00,"AmountInGlobalCurrency":10000000.00,"DebitCreditCode":"H","AssignmentReference":"pop up","ReversalReferenceDocument":"","ReversalReferenceDocumentItem":"","DocumentItemText":"pop up item 3","Supplier":"","Customer":"","FinancialAccountType":"S","SpecialGLCode":"","HouseBank":"","HouseBankAccount":"","MasterFixedAsset":"","FixedAsset":"","WithholdingTaxCode":"","FiscalPeriod":"10","PostingDate":"/Date(1791676800000)/","DocumentDate":"/Date(1791676800000)/","AccountingDocumentType":"KZ","AccountingDocumentHeaderText":"Pembayaran Vendor","Reference":"REF-1500000017","AccountingDocCreatedByUser":"SAPUSER","CreationDateTime":"11/10/2026 10:04","FlaggingSendToMaximo":"X","TimeStampSendToMaximo":""}'::jsonb, NOW()),
('1000~2026~1500000017~000004', '1000', '2026', '1500000017', '4', 'KZ', '2026-10-11', '2026-10-11', '21010101', '1030', '', '1000006', 'H', 20000000.00, 'IDR', 20000000.00, 'IDR', 'copy dan p', '{"CompanyCode":"1000","FiscalYear":"2026","AccountingDocument":"1500000017","AccountingDocumentItem":"4","CompanyCodeCurrency":"IDR","GlobalCurrency":"IDR","BaseUnit":"","Quantity":0,"Product":"","GLAccount":"21010101","CostCenter":"1030","ProfitCenter":"","FunctionalArea":"","AmountInTransactionCurrency":20000000.00,"AmountInCompanyCodeCurrency":20000000.00,"AmountInGlobalCurrency":20000000.00,"DebitCreditCode":"H","AssignmentReference":"pop up","ReversalReferenceDocument":"","ReversalReferenceDocumentItem":"","DocumentItemText":"copy dan p","Supplier":"1000006","Customer":"","FinancialAccountType":"K","SpecialGLCode":"","HouseBank":"","HouseBankAccount":"","MasterFixedAsset":"","FixedAsset":"","WithholdingTaxCode":"","FiscalPeriod":"10","PostingDate":"/Date(1791676800000)/","DocumentDate":"/Date(1791676800000)/","AccountingDocumentType":"KZ","AccountingDocumentHeaderText":"Pembayaran Vendor","Reference":"REF-1500000017","AccountingDocCreatedByUser":"SAPUSER","CreationDateTime":"11/10/2026 10:04","FlaggingSendToMaximo":"X","TimeStampSendToMaximo":""}'::jsonb, NOW()),
('1000~2026~1500000017~000005', '1000', '2026', '1500000017', '5', 'KZ', '2026-10-11', '2026-10-11', '21010101', '1038', '', '1000000', 'S', 8000000.00, 'IDR', 8000000.00, 'IDR', 'Pop Up', '{"CompanyCode":"1000","FiscalYear":"2026","AccountingDocument":"1500000017","AccountingDocumentItem":"5","CompanyCodeCurrency":"IDR","GlobalCurrency":"IDR","BaseUnit":"","Quantity":0,"Product":"","GLAccount":"21010101","CostCenter":"1038","ProfitCenter":"","FunctionalArea":"","AmountInTransactionCurrency":8000000.00,"AmountInCompanyCodeCurrency":8000000.00,"AmountInGlobalCurrency":8000000.00,"DebitCreditCode":"S","AssignmentReference":"","ReversalReferenceDocument":"","ReversalReferenceDocumentItem":"","DocumentItemText":"Pop Up","Supplier":"1000000","Customer":"","FinancialAccountType":"K","SpecialGLCode":"","HouseBank":"","HouseBankAccount":"","MasterFixedAsset":"","FixedAsset":"","WithholdingTaxCode":"","FiscalPeriod":"10","PostingDate":"/Date(1791676800000)/","DocumentDate":"/Date(1791676800000)/","AccountingDocumentType":"KZ","AccountingDocumentHeaderText":"Pembayaran Vendor","Reference":"REF-1500000017","AccountingDocCreatedByUser":"SAPUSER","CreationDateTime":"11/10/2026 10:04","FlaggingSendToMaximo":"X","TimeStampSendToMaximo":""}'::jsonb, NOW()),
('1000~2026~1500000017~000006', '1000', '2026', '1500000017', '6', 'KZ', '2026-10-11', '2026-10-11', '21010101', '1039', '', '1000000', 'S', 12000000.00, 'IDR', 12000000.00, 'IDR', 'Pop Up', '{"CompanyCode":"1000","FiscalYear":"2026","AccountingDocument":"1500000017","AccountingDocumentItem":"6","CompanyCodeCurrency":"IDR","GlobalCurrency":"IDR","BaseUnit":"","Quantity":0,"Product":"","GLAccount":"21010101","CostCenter":"1039","ProfitCenter":"","FunctionalArea":"","AmountInTransactionCurrency":12000000.00,"AmountInCompanyCodeCurrency":12000000.00,"AmountInGlobalCurrency":12000000.00,"DebitCreditCode":"S","AssignmentReference":"","ReversalReferenceDocument":"","ReversalReferenceDocumentItem":"","DocumentItemText":"Pop Up","Supplier":"1000000","Customer":"","FinancialAccountType":"K","SpecialGLCode":"","HouseBank":"","HouseBankAccount":"","MasterFixedAsset":"","FixedAsset":"","WithholdingTaxCode":"","FiscalPeriod":"10","PostingDate":"/Date(1791676800000)/","DocumentDate":"/Date(1791676800000)/","AccountingDocumentType":"KZ","AccountingDocumentHeaderText":"Pembayaran Vendor","Reference":"REF-1500000017","AccountingDocCreatedByUser":"SAPUSER","CreationDateTime":"11/10/2026 10:04","FlaggingSendToMaximo":"X","TimeStampSendToMaximo":""}'::jsonb, NOW()),
('1000~2026~1500000017~000007', '1000', '2026', '1500000017', '7', 'KZ', '2026-10-11', '2026-10-11', '21010101', '1030', '', '1000006', 'S', 10000000.00, 'IDR', 10000000.00, 'IDR', 'Pop Up', '{"CompanyCode":"1000","FiscalYear":"2026","AccountingDocument":"1500000017","AccountingDocumentItem":"7","CompanyCodeCurrency":"IDR","GlobalCurrency":"IDR","BaseUnit":"","Quantity":0,"Product":"","GLAccount":"21010101","CostCenter":"1030","ProfitCenter":"","FunctionalArea":"","AmountInTransactionCurrency":10000000.00,"AmountInCompanyCodeCurrency":10000000.00,"AmountInGlobalCurrency":10000000.00,"DebitCreditCode":"S","AssignmentReference":"","ReversalReferenceDocument":"","ReversalReferenceDocumentItem":"","DocumentItemText":"Pop Up","Supplier":"1000006","Customer":"","FinancialAccountType":"K","SpecialGLCode":"","HouseBank":"","HouseBankAccount":"","MasterFixedAsset":"","FixedAsset":"","WithholdingTaxCode":"","FiscalPeriod":"10","PostingDate":"/Date(1791676800000)/","DocumentDate":"/Date(1791676800000)/","AccountingDocumentType":"KZ","AccountingDocumentHeaderText":"Pembayaran Vendor","Reference":"REF-1500000017","AccountingDocCreatedByUser":"SAPUSER","CreationDateTime":"11/10/2026 10:04","FlaggingSendToMaximo":"X","TimeStampSendToMaximo":""}'::jsonb, NOW())
ON CONFLICT (gl_line_item_key) DO NOTHING;

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

-- RICEF 7: Staging penampung Reimbursement & Business Trip
CREATE TABLE IF NOT EXISTS reimbursement_requests (
    id BIGSERIAL PRIMARY KEY,
    batch_id BIGINT REFERENCES excel_upload_batches(id) ON DELETE SET NULL,
    doc_type VARCHAR(30) NOT NULL,              -- 'REIMBURSEMENT' / 'BUSINESS_TRIP'
    transaction_id VARCHAR(50) NOT NULL,        -- Transaction Id atau Trip ID
    employee_id VARCHAR(50) NOT NULL,           -- Employee ID atau vendor code
    employee_name VARCHAR(150),                 -- Full Name atau Requested By Name
    policy_or_trip_type VARCHAR(100),           -- Policy Name atau Trip Type (Domestic/International)
    benefit_or_destination VARCHAR(150),        -- Benefit Name 1..7 / Destination
    description_or_notes TEXT,                  -- Notes / Keterangan
    effective_date DATE,                        -- Document / Effective Date
    posting_date DATE,                          -- Posting Date
    currency VARCHAR(10) DEFAULT 'IDR',
    gross_amount NUMERIC(18, 2) NOT NULL,
    paid_amount NUMERIC(18, 2) NOT NULL,
    cip_flag VARCHAR(20),                       -- CIP / NON_CIP
    gl_account VARCHAR(20),                     -- Akun GL dari mapping
    cost_center VARCHAR(20),                    -- Budget Code / Cost Center dari mapping
    status VARCHAR(30) DEFAULT 'PENDING',       -- PENDING, QUEUED, SUCCESS, ERROR
    sap_document_no VARCHAR(50),
    error_message TEXT,
    raw_data JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_reimb_req_tx ON reimbursement_requests(transaction_id);
CREATE INDEX IF NOT EXISTS idx_reimb_req_emp ON reimbursement_requests(employee_id);
CREATE INDEX IF NOT EXISTS idx_reimb_req_type ON reimbursement_requests(doc_type);



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

-- Sinkronisasi kolom financial ke material_config_mappings untuk Panel UI
ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS inventory_posting_gl VARCHAR(50);
ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS goods_issue_gl VARCHAR(50);
ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS stock_adj_gl VARCHAR(50);
ALTER TABLE material_config_mappings ADD COLUMN IF NOT EXISTS cost_center VARCHAR(50);

UPDATE material_config_mappings SET inventory_posting_gl = '114001', goods_issue_gl = '512033', stock_adj_gl = '512033', cost_center = '71098' WHERE UPPER(sap_value) = 'S001' AND (inventory_posting_gl IS NULL OR inventory_posting_gl = '');
UPDATE material_config_mappings SET inventory_posting_gl = '114004', goods_issue_gl = '512030', stock_adj_gl = '512030', cost_center = '71099' WHERE UPPER(sap_value) = 'S002' AND (inventory_posting_gl IS NULL OR inventory_posting_gl = '');
UPDATE material_config_mappings SET inventory_posting_gl = '114009', goods_issue_gl = '512031', stock_adj_gl = '512031', cost_center = '71099' WHERE UPPER(sap_value) = 'S003' AND (inventory_posting_gl IS NULL OR inventory_posting_gl = '');
UPDATE material_config_mappings SET inventory_posting_gl = '114012', goods_issue_gl = '512032', stock_adj_gl = '512032', cost_center = '71099' WHERE UPPER(sap_value) = 'S004' AND (inventory_posting_gl IS NULL OR inventory_posting_gl = '');
UPDATE material_config_mappings SET inventory_posting_gl = '114006', goods_issue_gl = '512034', stock_adj_gl = '512034', cost_center = '71099' WHERE UPPER(sap_value) = 'S006' AND (inventory_posting_gl IS NULL OR inventory_posting_gl = '');
UPDATE material_config_mappings SET inventory_posting_gl = '114017', goods_issue_gl = '512028', stock_adj_gl = '512028', cost_center = '71099' WHERE UPPER(sap_value) = 'S009' AND (inventory_posting_gl IS NULL OR inventory_posting_gl = '');
UPDATE material_config_mappings SET inventory_posting_gl = '114016', goods_issue_gl = '512023', stock_adj_gl = '512023', cost_center = '71099' WHERE UPPER(sap_value) = 'S011' AND (inventory_posting_gl IS NULL OR inventory_posting_gl = '');

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

-- TABEL APP CONFIGS (Runtime System Configuration / Panel System)
CREATE TABLE IF NOT EXISTS configs (
    key VARCHAR(100) PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

INSERT INTO configs (key, value, description, updated_at) VALUES
('bot_base_url', 'http://localhost:8085/api/v1', 'Base URL untuk Bot Poller / Ingestion Engine integrasi Maximo', NOW()),
('bpa_base_url', 'https://spa-api.eu10.process-automation.sap.biz', 'Base URL SAP Build Process Automation (BPA) trigger API', NOW()),
('sap_base_url', 'https://my445160-api.s4hana.cloud.sap', 'Host sentral SAP S/4HANA Grow Cloud untuk seluruh layanan OData', NOW()),
('sap_username', 'CU_SAP_COM_0053', 'Communication User default untuk koneksi SAP S/4HANA', NOW()),
('sap_password', 'PowerofOne@123456789', 'Communication Password default untuk koneksi SAP S/4HANA', NOW()),
('erp_callback_url', 'http://host.docker.internal:1433/maximo/api/webhook/status', 'Webhook callback URL ke IBM Maximo untuk update status dokumen SAP', NOW()),
('prefix_bot_mapping', '{"45": "BPA_PO_NORMAL_TRIGGER", "47": "BPA_PO_CONTRACT_TRIGGER"}', 'Mapping prefix nomor PO Maximo ke BPA Workflow Trigger ID', NOW()),
('doc_type_bot_mapping', '{"NB": "WF_STANDARD_PO", "ZPRC": "WF_PROJECT_PO", "ZCTR": "WF_CONTRACT_PO"}', 'Mapping Document Type Maximo ke Workflow Definition ID', NOW())
ON CONFLICT (key) DO NOTHING;

-- Sinkronisasi default value PRODUCT_PLANTS ke 1000, 2000
UPDATE sap_default_values 
SET value = '1000,2000', default_value = '1000,2000' 
WHERE key = 'PRODUCT_PLANTS' AND (value = '5110,BTNG' OR value = '');

-- Sinkronisasi default value PO ke 1000, BPI, ZBPI
UPDATE sap_default_values 
SET value = '1000', default_value = '1000' 
WHERE key = 'PO_COMPANY_CODE' AND (value = '5110' OR value = '' OR default_value = '5110');

UPDATE sap_default_values 
SET value = 'BPI', default_value = 'BPI' 
WHERE key = 'PO_PURCHASING_ORG' AND (value = '5110' OR value = '' OR default_value = '5110');

UPDATE sap_default_values 
SET value = 'ZBPI', default_value = 'ZBPI' 
WHERE key IN ('PO_TYPE_DEFAULT', 'PO_TYPE_PREFIX_45', 'PO_TYPE_PREFIX_47') AND value IN ('ZPRC', 'ZCTR', 'NB', '');

UPDATE sap_default_values 
SET value = '1000', default_value = '1000' 
WHERE key = 'PO_DEFAULT_PLANT' AND (value = '5110' OR value = '' OR default_value = '5110');
