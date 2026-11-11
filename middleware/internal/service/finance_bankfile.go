package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
)

// Nama field CDS YY1_PaymentProposal belum final ($metadata menyusul). Default di bawah bisa diganti tanpa
// ubah kode lewat env FIN_PAYMENT_FIELD_<NAMA> (daftar nama field dipisah koma, dicoba berurutan).
var defaultPaymentFieldKeys = map[string][]string{
	"PAYEE_NAME": {"PayeeName", "SupplierName", "BeneficiaryName", "BusinessPartnerName", "SupplierFullName"},
	"SHORT_NAME": {"PayeeShortName", "SupplierShortName", "SearchTerm"},
	"BANK_NAME":  {"PayeeBankName", "BankName", "BeneficiaryBankName"},
	"ACCOUNT":    {"PayeeBankAccount", "BankAccount", "BankAccountNumber", "BeneficiaryAccount"},
	"BANK_CODE":  {"PayeeBankNumber", "BankNumber", "BankKey", "BankCode"},
	"CITY":       {"PayeeCityName", "CityName", "BankCity", "City"},
	"REMARK":     {"PaymentReference", "PaymentRemark", "DocumentItemText"},
}

func paymentFieldKeys(name string) []string {
	if v := strings.TrimSpace(envOr("FIN_PAYMENT_FIELD_"+name, "")); v != "" {
		var keys []string
		for _, k := range strings.Split(v, ",") {
			if k = strings.TrimSpace(k); k != "" {
				keys = append(keys, k)
			}
		}
		return keys
	}
	return defaultPaymentFieldKeys[name]
}

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

func paymentField(p model.PaymentProposal, name string) string {
	return rawString(p.Raw, paymentFieldKeys(name))
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

// BuildBTMUFile menghasilkan file 30 kolom MUFG/BTMU dengan pemisah FIN_BTMU_SEPARATOR (default ";"):
// kolom 1 tanpa kutip, kolom 2–30 berkutip ganda. Kliring, charge bearer & kode instruksi selalu nilai default.
func BuildBTMUFile(items []model.PaymentProposal, valueDate time.Time) ([]byte, error) {
	debit := envOr("FIN_BTMU_DEBIT_ACCOUNT", "")
	if debit == "" {
		return nil, &ValidationError{Messages: []string{"FIN_BTMU_DEBIT_ACCOUNT belum diisi"}}
	}
	sep := envOr("FIN_BTMU_SEPARATOR", ";")
	var b strings.Builder
	for _, p := range items {
		name := paymentField(p, "PAYEE_NAME")
		cols := make([]string, 30)
		cols[0] = debit
		cols[1] = bankValueDate(p, valueDate).Format("060102")
		cols[2] = truncate(firstNonEmpty(paymentField(p, "SHORT_NAME"), name, p.Supplier), 35)
		cols[3] = envOr("FIN_BTMU_TRANSFER_TYPE", "Domestic")
		cols[4] = strings.ToUpper(firstNonEmpty(p.Currency, "IDR"))
		cols[5] = bankAmount(p.Amount)
		cols[10] = paymentField(p, "BANK_NAME")
		cols[12] = paymentField(p, "ACCOUNT")
		cols[13] = strings.Trim(firstNonEmpty(name, p.Supplier)+"/"+paymentField(p, "CITY"), "/")
		cols[15] = envOr("FIN_BTMU_CLEARING_CODE", "03")
		cols[16] = envOr("FIN_BTMU_INSTRUCTION", "RRCR0142+")
		cols[17] = envOr("FIN_BTMU_CHARGE_BEARER", "OUR")
		b.WriteString(cols[0])
		for _, c := range cols[1:] {
			b.WriteString(sep + quoteCSV(c))
		}
		b.WriteString("\r\n")
	}
	return []byte(b.String()), nil
}

// BuildBRIFile menghasilkan file CMS BRI 16 kolom (A–P) dengan pemisah FIN_BRI_SEPARATOR (default "|"),
// kolom P kosong sebagai penutup baris.
func BuildBRIFile(items []model.PaymentProposal, valueDate time.Time) ([]byte, error) {
	debit := envOr("FIN_BRI_DEBIT_ACCOUNT", "")
	if debit == "" {
		return nil, &ValidationError{Messages: []string{"FIN_BRI_DEBIT_ACCOUNT belum diisi"}}
	}
	sep := envOr("FIN_BRI_SEPARATOR", "|")
	clean := func(s string) string {
		return strings.NewReplacer(sep, " ", "\r", " ", "\n", " ").Replace(strings.TrimSpace(s))
	}
	sameBank := envOr("FIN_BRI_SAME_BANK_CODE", "002")
	var b strings.Builder
	for _, p := range items {
		bankCode := paymentField(p, "BANK_CODE")
		instr := envOr("FIN_BRI_TRANSFER_TYPE_OTHER", "2")
		if bankCode == sameBank {
			instr = envOr("FIN_BRI_TRANSFER_TYPE_SAME", "1")
		}
		cols := []string{
			clean(debit),
			bankValueDate(p, valueDate).Format("020106"),
			strings.ToUpper(firstNonEmpty(p.Currency, "IDR")),
			bankAmount(p.Amount),
			clean(envOr("FIN_BRI_REFERENCE", "0000000000")),
			clean(firstNonEmpty(paymentField(p, "PAYEE_NAME"), p.Supplier)),
			clean(firstNonEmpty(paymentField(p, "CITY"), envOr("FIN_BRI_DEFAULT_CITY", "JAKARTA"))),
			clean(bankCode),
			clean(paymentField(p, "ACCOUNT")),
			clean(envOr("FIN_BRI_REMITTER_NAME", "PT BHIMASENA POWER INDONESIA")),
			clean(firstNonEmpty(paymentField(p, "REMARK"), envOr("FIN_BRI_REMARK", "PEMBAYARAN TAGIHAN"))),
			instr, "0", "0", "0", "",
		}
		b.WriteString(strings.Join(cols, sep) + "\r\n")
	}
	return []byte(b.String()), nil
}
