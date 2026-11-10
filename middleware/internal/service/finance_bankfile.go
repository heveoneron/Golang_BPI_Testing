package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
)

// Nama field CDS YY1_PaymentProposal belum final ($metadata menyusul), jadi beberapa kandidat dicoba berurutan.
var (
	bankKeysPayeeName = []string{"PayeeName", "SupplierName", "BeneficiaryName", "BusinessPartnerName", "SupplierFullName"}
	bankKeysShortName = []string{"PayeeShortName", "SupplierShortName", "SearchTerm"}
	bankKeysBankName  = []string{"PayeeBankName", "BankName", "BeneficiaryBankName"}
	bankKeysAccount   = []string{"PayeeBankAccount", "BankAccount", "BankAccountNumber", "BeneficiaryAccount"}
	bankKeysBankCode  = []string{"PayeeBankNumber", "BankNumber", "BankKey", "BankCode"}
	bankKeysCity      = []string{"PayeeCityName", "CityName", "BankCity", "City"}
	bankKeysClearing  = []string{"ClearingCode", "BankControlKey"}
	bankKeysRemark    = []string{"PaymentReference", "PaymentRemark", "DocumentItemText"}
)

func rawString(raw json.RawMessage, keys []string) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
				return s
			}
		}
	}
	return ""
}

func bankValueDate(p model.PaymentProposal, override time.Time) time.Time {
	if !override.IsZero() {
		return override
	}
	if p.PaymentRunDate != nil {
		if s, ok := parseFlexibleDate(*p.PaymentRunDate); ok {
			if t, err := time.Parse("2006-01-02", s[:min(len(s), 10)]); err == nil {
				return t
			}
		}
	}
	return time.Now()
}

func quoteCSV(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func bankAmount(v float64) string {
	if v < 0 {
		v = -v
	}
	return fmt.Sprintf("%.2f", v)
}

// BuildBTMUFile menghasilkan file 30 kolom MUFG/BTMU: kolom 1 tanpa kutip, kolom 2–30 berkutip ganda.
func BuildBTMUFile(items []model.PaymentProposal, valueDate time.Time) ([]byte, error) {
	debit := envOr("FIN_BTMU_DEBIT_ACCOUNT", "")
	if debit == "" {
		return nil, &ValidationError{Messages: []string{"FIN_BTMU_DEBIT_ACCOUNT belum diisi"}}
	}
	var b strings.Builder
	for _, p := range items {
		name := rawString(p.Raw, bankKeysPayeeName)
		cols := make([]string, 30)
		cols[0] = debit
		cols[1] = bankValueDate(p, valueDate).Format("060102")
		cols[2] = truncate(firstNonEmpty(rawString(p.Raw, bankKeysShortName), name, p.Supplier), 35)
		cols[3] = envOr("FIN_BTMU_TRANSFER_TYPE", "Domestic")
		cols[4] = strings.ToUpper(firstNonEmpty(p.Currency, "IDR"))
		cols[5] = bankAmount(p.Amount)
		cols[10] = rawString(p.Raw, bankKeysBankName)
		cols[12] = rawString(p.Raw, bankKeysAccount)
		cols[13] = strings.Trim(firstNonEmpty(name, p.Supplier)+"/"+rawString(p.Raw, bankKeysCity), "/")
		cols[15] = firstNonEmpty(rawString(p.Raw, bankKeysClearing), envOr("FIN_BTMU_CLEARING_CODE", "03"))
		cols[16] = firstNonEmpty(envOr("FIN_BTMU_INSTRUCTION", ""), p.PaymentDocument)
		cols[17] = envOr("FIN_BTMU_CHARGE_BEARER", "OUR")
		b.WriteString(cols[0])
		for _, c := range cols[1:] {
			b.WriteString("," + quoteCSV(c))
		}
		b.WriteString("\r\n")
	}
	return []byte(b.String()), nil
}

// BuildBRIFile menghasilkan file CMS BRI 16 kolom (A–P), kolom P kosong sebagai penutup baris.
func BuildBRIFile(items []model.PaymentProposal, valueDate time.Time) ([]byte, error) {
	debit := envOr("FIN_BRI_DEBIT_ACCOUNT", "")
	if debit == "" {
		return nil, &ValidationError{Messages: []string{"FIN_BRI_DEBIT_ACCOUNT belum diisi"}}
	}
	clean := func(s string) string {
		return strings.NewReplacer(",", " ", "\r", " ", "\n", " ").Replace(strings.TrimSpace(s))
	}
	var b strings.Builder
	for _, p := range items {
		bankCode := rawString(p.Raw, bankKeysBankCode)
		instr := "2"
		if bankCode == "002" {
			instr = "1"
		}
		cols := []string{
			debit,
			bankValueDate(p, valueDate).Format("020106"),
			strings.ToUpper(firstNonEmpty(p.Currency, "IDR")),
			bankAmount(p.Amount),
			"0000000000",
			clean(firstNonEmpty(rawString(p.Raw, bankKeysPayeeName), p.Supplier)),
			clean(firstNonEmpty(rawString(p.Raw, bankKeysCity), envOr("FIN_BRI_DEFAULT_CITY", "JAKARTA"))),
			bankCode,
			rawString(p.Raw, bankKeysAccount),
			clean(envOr("FIN_BRI_REMITTER_NAME", "PT BHIMASENA POWER INDONESIA")),
			clean(firstNonEmpty(rawString(p.Raw, bankKeysRemark), envOr("FIN_BRI_REMARK", "PEMBAYARAN TAGIHAN"))),
			instr, "0", "0", "0", "",
		}
		b.WriteString(strings.Join(cols, ",") + "\r\n")
	}
	return []byte(b.String()), nil
}
