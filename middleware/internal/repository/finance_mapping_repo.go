package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang-bpi-testing/internal/model"

	"github.com/jackc/pgx/v5"
)

// defaultReimbursementMappings berasal dari dokumen "Mapping Table Reimbers & Business trip to golang"
// (sheet BT): kebijakan reimbursement x CIP/Non CIP -> GL account & budget code.
var defaultReimbursementMappings = []model.ReimbursementGLMapping{
	{PolicyName: "Medical Claim - Outpatient", CIPFlag: "CIP", GLAccount: "511006", BudgetCode: "10081"},
	{PolicyName: "Medical Claim - Outpatient", CIPFlag: "NON_CIP", GLAccount: "522108", BudgetCode: "10081"},
	{PolicyName: "Medical Claim - Inpatient", CIPFlag: "CIP", GLAccount: "511006", BudgetCode: "10081"},
	{PolicyName: "Medical Claim - Inpatient", CIPFlag: "NON_CIP", GLAccount: "522108", BudgetCode: "10081"},
	{PolicyName: "Maternity", CIPFlag: "CIP", GLAccount: "511006", BudgetCode: "10081"},
	{PolicyName: "Maternity", CIPFlag: "NON_CIP", GLAccount: "522108", BudgetCode: "10081"},
	{PolicyName: "Glasses – Frame & Lense", CIPFlag: "CIP", GLAccount: "511006", BudgetCode: "10081"},
	{PolicyName: "Glasses – Frame & Lense", CIPFlag: "NON_CIP", GLAccount: "522108", BudgetCode: "10081"},
	{PolicyName: "Club Membership", CIPFlag: "CIP", GLAccount: "522106", BudgetCode: "10131"},
	{PolicyName: "Club Membership", CIPFlag: "NON_CIP", GLAccount: "522106", BudgetCode: "10131"},
	{PolicyName: "Club Membership - Register", CIPFlag: "CIP", GLAccount: "522106", BudgetCode: "10131"},
	{PolicyName: "Club Membership - Register", CIPFlag: "NON_CIP", GLAccount: "522106", BudgetCode: "10131"},
	{PolicyName: "Glasess – Lense", CIPFlag: "CIP", GLAccount: "511006", BudgetCode: "10081"},
	{PolicyName: "Glasess – Lense", CIPFlag: "NON_CIP", GLAccount: "522108", BudgetCode: "10081"},
	{PolicyName: "Bantuan Menikah", CIPFlag: "CIP", GLAccount: "522106", BudgetCode: "10131"},
	{PolicyName: "Bantuan Menikah", CIPFlag: "NON_CIP", GLAccount: "522106", BudgetCode: "10131"},
	{PolicyName: "Bantuan Dukacita", CIPFlag: "CIP", GLAccount: "522106", BudgetCode: "10131"},
	{PolicyName: "Bantuan Dukacita", CIPFlag: "NON_CIP", GLAccount: "522106", BudgetCode: "10131"},
	{PolicyName: "Domestic", CIPFlag: "CIP", GLAccount: "511018", BudgetCode: "19031"},
	{PolicyName: "Domestic", CIPFlag: "NON_CIP", GLAccount: "522234", BudgetCode: "19031"},
	{PolicyName: "International", CIPFlag: "CIP", GLAccount: "511018", BudgetCode: "19031"},
	{PolicyName: "International", CIPFlag: "NON_CIP", GLAccount: "522234", BudgetCode: "19031"},
}

// seedReimbursementMappings mengisi mapping default tanpa menimpa perubahan dari Panel.
func (r *PostgresRepository) seedReimbursementMappings(ctx context.Context) error {
	batch := &pgx.Batch{}
	for _, m := range defaultReimbursementMappings {
		batch.Queue(`
			INSERT INTO reimbursement_gl_mappings (policy_name, policy_key, cip_flag, gl_account, budget_code)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (policy_key, cip_flag) DO NOTHING
		`, m.PolicyName, model.NormalizePolicyName(m.PolicyName), m.CIPFlag, m.GLAccount, m.BudgetCode)
	}
	_, err := execBatch(ctx, r, batch, len(defaultReimbursementMappings))
	return err
}

// ===================== Reimbursement GL mapping =====================

func (r *PostgresRepository) ListReimbursementMappings(ctx context.Context) ([]model.ReimbursementGLMapping, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT id, policy_name, cip_flag, gl_account, budget_code, is_active, updated_at
		FROM reimbursement_gl_mappings ORDER BY policy_name, cip_flag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ReimbursementGLMapping
	for rows.Next() {
		var m model.ReimbursementGLMapping
		if err := rows.Scan(&m.ID, &m.PolicyName, &m.CIPFlag, &m.GLAccount, &m.BudgetCode, &m.IsActive, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SaveReimbursementMapping menyimpan (insert / update berdasarkan nama kebijakan + CIP) satu mapping.
func (r *PostgresRepository) SaveReimbursementMapping(ctx context.Context, m model.ReimbursementGLMapping) error {
	if r.Pool == nil {
		return fmt.Errorf("postgres pool not available")
	}
	_, err := r.Pool.Exec(ctx, `
		INSERT INTO reimbursement_gl_mappings (policy_name, policy_key, cip_flag, gl_account, budget_code, is_active, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (policy_key, cip_flag) DO UPDATE SET
			policy_name = EXCLUDED.policy_name, gl_account = EXCLUDED.gl_account,
			budget_code = EXCLUDED.budget_code, is_active = EXCLUDED.is_active, updated_at = NOW()
	`, strings.TrimSpace(m.PolicyName), model.NormalizePolicyName(m.PolicyName), m.CIPFlag, m.GLAccount, m.BudgetCode, m.IsActive)
	return err
}

func (r *PostgresRepository) DeleteReimbursementMapping(ctx context.Context, id int) error {
	if r.Pool == nil {
		return fmt.Errorf("postgres pool not available")
	}
	tag, err := r.Pool.Exec(ctx, `DELETE FROM reimbursement_gl_mappings WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// LookupReimbursementGL mencari GL account & budget code untuk kebijakan + CIP. found=false bila tidak ada.
func (r *PostgresRepository) LookupReimbursementGL(ctx context.Context, policyName, cipFlag string) (string, string, bool, error) {
	if r.Pool == nil {
		return "", "", false, fmt.Errorf("postgres pool not available")
	}
	var gl, bc string
	err := r.Pool.QueryRow(ctx, `
		SELECT gl_account, budget_code FROM reimbursement_gl_mappings
		WHERE policy_key = $1 AND cip_flag = $2 AND is_active = true
	`, model.NormalizePolicyName(policyName), cipFlag).Scan(&gl, &bc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return gl, bc, true, nil
}

// ===================== Employee vendor (CIP / Non CIP) =====================

func (r *PostgresRepository) ListEmployeeVendors(ctx context.Context, search string, limit int) ([]model.EmployeeVendorMapping, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT vendor_code, cip_flag, COALESCE(description, ''), is_active, updated_at
		FROM employee_vendor_mappings
		WHERE ($1 = '' OR vendor_code ILIKE '%' || $1 || '%' OR description ILIKE '%' || $1 || '%')
		ORDER BY vendor_code LIMIT $2`, search, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.EmployeeVendorMapping
	for rows.Next() {
		var m model.EmployeeVendorMapping
		if err := rows.Scan(&m.VendorCode, &m.CIPFlag, &m.Description, &m.IsActive, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpsertEmployeeVendors menyimpan banyak vendor karyawan sekaligus (dipakai form dan import file).
func (r *PostgresRepository) UpsertEmployeeVendors(ctx context.Context, items []model.EmployeeVendorMapping) (int, error) {
	if r.Pool == nil {
		return 0, fmt.Errorf("postgres pool not available")
	}
	if len(items) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, m := range items {
		batch.Queue(`
			INSERT INTO employee_vendor_mappings (vendor_code, cip_flag, description, is_active, updated_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (vendor_code) DO UPDATE SET
				cip_flag = EXCLUDED.cip_flag, description = EXCLUDED.description, is_active = EXCLUDED.is_active, updated_at = NOW()
		`, strings.TrimSpace(m.VendorCode), m.CIPFlag, m.Description, m.IsActive)
	}
	return execBatch(ctx, r, batch, len(items))
}

func (r *PostgresRepository) DeleteEmployeeVendor(ctx context.Context, vendorCode string) error {
	if r.Pool == nil {
		return fmt.Errorf("postgres pool not available")
	}
	tag, err := r.Pool.Exec(ctx, `DELETE FROM employee_vendor_mappings WHERE vendor_code = $1`, vendorCode)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// LookupEmployeeVendorCIP mengembalikan status CIP vendor karyawan. found=false bila belum terdaftar.
func (r *PostgresRepository) LookupEmployeeVendorCIP(ctx context.Context, vendorCode string) (string, bool, error) {
	if r.Pool == nil {
		return "", false, fmt.Errorf("postgres pool not available")
	}
	var flag string
	err := r.Pool.QueryRow(ctx, `
		SELECT cip_flag FROM employee_vendor_mappings WHERE vendor_code = $1 AND is_active = true
	`, strings.TrimSpace(vendorCode)).Scan(&flag)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return flag, true, nil
}
