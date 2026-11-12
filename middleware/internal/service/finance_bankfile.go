package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
)

// Nama field CDS YY1_PaymentProposal belum final ($metadata menyusul). Default ada di
// defaultSAPFieldKeys["PAYMENT"] (finance_fieldmap.go) dan bisa diganti lewat env FIN_PAYMENT_FIELD_<NAMA>.
func paymentFieldKeys(name string) []string {
	return sapFieldKeys("PAYMENT", name)
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

// VendorBankLookup mengembalikan Vendor Master (employee_vendor_mappings) untuk kode supplier payload:
// email, nama bank (account_holder), bank key, rekening.
type VendorBankLookup func(supplier string) (model.EmployeeVendorMapping, bool)

func (l VendorBankLookup) get(supplier string) model.EmployeeVendorMapping {
	if l == nil || strings.TrimSpace(supplier) == "" {
		return model.EmployeeVendorMapping{}
	}
	v, _ := l(strings.TrimSpace(supplier))
	return v
}

// VendorBankLookupFromList membuat lookup dari daftar Vendor Master aktif.
func VendorBankLookupFromList(items []model.EmployeeVendorMapping) VendorBankLookup {
	m := make(map[string]model.EmployeeVendorMapping, len(items))
	for _, v := range items {
		if v.IsActive {
			m[strings.ToUpper(strings.TrimSpace(v.VendorCode))] = v
		}
	}
	return func(supplier string) (model.EmployeeVendorMapping, bool) {
		v, ok := m[strings.ToUpper(supplier)]
		return v, ok
	}
}

// bankNow dapat diganti di test agar jam di file BRI deterministik.
var bankNow = time.Now

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
	return bankNow()
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

func intDefault(key string, def int) int {
	if n, err := strconv.Atoi(defaultValue(key)); err == nil && n > 0 {
		return n
	}
	return def
}

// BuildBTMUFile menghasilkan file MUFG/BTMU 30 kolom (pemisah FIN_BTMU_SEPARATOR, kolom 1 tanpa kutip,
// kolom 2–30 berkutip ganda) sesuai "BPI - Mapping file bank MUFG". Template ini HANYA untuk transfer
// domestik (cabang 3665, mata uang FIN_BTMU_CURRENCY); baris mata uang lain ditolak. Nilai tetap dari Default Value:
//   - 1  cabang + mata uang + tipe + rekening debit BPI (contoh 3665IDRCUA5100015546)
//   - 2  PaymentRunDate (YYMMDD)       - 3  OrganizationBPName1 (FIN_BTMU_NAME_MAX_LEN, 0 = tanpa batas)
//   - 4  FIN_BTMU_TRANSFER_TYPE (Domestic)
//   - 5  PaymentCurrency  - 6 PaymentAmountInPaytCurrency
//   - 11 nama bank vendor: Vendor Master (account_holder) bila supplier ada, selain itu field payload
//   - 13 PayeeBankAccount - 14 nama vendor/kota - 16 kliring - 17 instruksi - 18 charge bearer
func BuildBTMUFile(items []model.PaymentProposal, valueDate time.Time, vendors VendorBankLookup) ([]byte, error) {
	sep := defaultValue("FIN_BTMU_SEPARATOR")
	if sep == "" {
		sep = ";"
	}
	nameMax, _ := strconv.Atoi(defaultValue("FIN_BTMU_NAME_MAX_LEN")) // 0/kosong = tanpa batas
	domestic := strings.ToUpper(defaultValue("FIN_BTMU_CURRENCY"))
	branch, account := defaultValue("FIN_BTMU_BRANCH"), defaultValue("FIN_BTMU_ACCOUNT")
	if branch == "" || account == "" {
		return nil, &ValidationError{Messages: []string{"FIN_BTMU_BRANCH / FIN_BTMU_ACCOUNT belum diatur di Default Value"}}
	}
	var msgs []string
	var b strings.Builder
	for i, p := range items {
		cur := strings.ToUpper(firstNonEmpty(p.Currency, domestic))
		if domestic != "" && cur != domestic {
			msgs = append(msgs, fmt.Sprintf("baris %d (%s): template MUFG hanya untuk transfer domestik %s, mata uang %s tidak didukung", i+1, firstNonEmpty(p.PaymentDocument, p.Supplier), domestic, cur))
			continue
		}
		v := vendors.get(p.Supplier)
		name := firstNonEmpty(paymentField(p, "PAYEE_NAME"), paymentField(p, "SHORT_NAME"), p.Supplier)
		bankName := firstNonEmpty(v.AccountHolder, paymentField(p, "BANK_NAME"))
		transfer := defaultValue("FIN_BTMU_TRANSFER_TYPE")

		cols := make([]string, 30)
		cols[0] = branch + cur + defaultValue("FIN_BTMU_ACCOUNT_TYPE") + account
		cols[1] = bankValueDate(p, valueDate).Format("060102")
		cols[2] = strings.TrimSpace(name)
		if nameMax > 0 {
			cols[2] = truncate(name, nameMax)
		}
		cols[3] = transfer
		cols[4] = cur
		cols[5] = bankAmount(p.Amount)
		cols[10] = bankName
		cols[12] = firstNonEmpty(paymentField(p, "ACCOUNT"), v.BankAccount)
		cols[13] = strings.Trim(name+"/"+paymentField(p, "CITY"), "/")
		cols[15] = defaultValue("FIN_BTMU_CLEARING_CODE")
		cols[16] = defaultValue("FIN_BTMU_INSTRUCTION")
		cols[17] = defaultValue("FIN_BTMU_CHARGE_BEARER")
		b.WriteString(cols[0])
		for _, c := range cols[1:] {
			b.WriteString(sep + quoteCSV(c))
		}
		b.WriteString("\r\n")
	}
	if len(msgs) > 0 {
		return nil, &ValidationError{Messages: msgs}
	}
	return []byte(b.String()), nil
}

// BuildBRIFile menghasilkan file BRI 21 kolom (pemisah FIN_BRI_SEPARATOR) sesuai "BPI - Mapping file bank BRI":
//   - 1  nomor urut  - 2 FIN_BRI_REF_PREFIX + nomor urut kolom 1 berisi nol FIN_BRI_REF_DIGITS digit (BRI0000000001)
//   - 3  IFT bila PayeeSwiftCode = FIN_BRI_SAME_BANK_SWIFT (atau kode FIN_BRI_SAME_BANK_CODE), selain itu LLG
//   - 4  PaymentRunDate + jam (yyyyMMddHHmm)  - 5 BankAccount (rekening BPI)  - 6 nama pemilik rekening BPI
//   - 7  SWIFT bank vendor  - 8 PayeeBankAccount  - 9 OrganizationBPName1  - 10 kota
//   - 11 PaymentAmountInPaytCurrency  - 12 PaymentCurrency  - 13 nama vendor  - 14 email Vendor Master
//   - 15–21 nilai tetap menu Default Value
func BuildBRIFile(items []model.PaymentProposal, valueDate time.Time, vendors VendorBankLookup) ([]byte, error) {
	sep := defaultValue("FIN_BRI_SEPARATOR")
	if sep == "" {
		sep = "|"
	}
	clean := func(s string) string {
		return strings.NewReplacer(sep, " ", "\r", " ", "\n", " ").Replace(strings.TrimSpace(s))
	}
	sameSwift := strings.ToUpper(defaultValue("FIN_BRI_SAME_BANK_SWIFT"))
	sameCode := defaultValue("FIN_BRI_SAME_BANK_CODE")
	clock := bankNow()
	refDigits := intDefault("FIN_BRI_REF_DIGITS", 10)
	var msgs []string
	var b strings.Builder
	for i, p := range items {
		debit := firstNonEmpty(paymentField(p, "HOUSE_ACCOUNT"), defaultValue("FIN_BRI_DEBIT_ACCOUNT"))
		if debit == "" {
			msgs = append(msgs, fmt.Sprintf("baris %d: rekening BPI kosong (field BankAccount payload / FIN_BRI_DEBIT_ACCOUNT)", i+1))
			continue
		}
		v := vendors.get(p.Supplier)
		swift := strings.ToUpper(paymentField(p, "SWIFT_CODE"))
		transfer := defaultValue("FIN_BRI_TRANSFER_OTHER")
		if (sameSwift != "" && strings.HasPrefix(swift, sameSwift)) || (sameCode != "" && paymentField(p, "BANK_CODE") == sameCode) {
			transfer = defaultValue("FIN_BRI_TRANSFER_SAME")
		}
		d := bankValueDate(p, valueDate)
		stamp := time.Date(d.Year(), d.Month(), d.Day(), clock.Hour(), clock.Minute(), 0, 0, time.Local)
		name := firstNonEmpty(paymentField(p, "PAYEE_NAME"), p.Supplier)
		cols := []string{
			strconv.Itoa(i + 1),
			defaultValue("FIN_BRI_REF_PREFIX") + fmt.Sprintf("%0*d", refDigits, i+1),
			transfer,
			stamp.Format("200601021504"),
			debit,
			defaultValue("FIN_BRI_REMITTER_NAME"),
			swift,
			firstNonEmpty(paymentField(p, "ACCOUNT"), v.BankAccount),
			name,
			defaultValue("FIN_BRI_CITY"),
			bankAmount(p.Amount),
			strings.ToUpper(firstNonEmpty(p.Currency, "IDR")),
			name,
			firstNonEmpty(v.Email, paymentField(p, "EMAIL")),
			defaultValue("FIN_BRI_CHARGE_BEARER"),
			defaultValue("FIN_BRI_COL16"),
			defaultValue("FIN_BRI_COL17"),
			defaultValue("FIN_BRI_REMITTER_ADDRESS"),
			defaultValue("FIN_BRI_REMITTER_PHONE"),
			defaultValue("FIN_BRI_COL20"),
			defaultValue("FIN_BRI_COL21"),
		}
		for j := range cols {
			cols[j] = clean(cols[j])
		}
		b.WriteString(strings.Join(cols, sep) + "\r\n")
	}
	if len(msgs) > 0 {
		return nil, &ValidationError{Messages: msgs}
	}
	return []byte(b.String()), nil
}
