package service

import "strings"

// Kandidat nama field response SAP per kelompok. Setiap nama bisa diganti tanpa ubah kode lewat env
// FIN_<KELOMPOK>_FIELD_<NAMA> (daftar nama field dipisah koma, dicoba berurutan), mengikuti pola
// FIN_PAYMENT_FIELD_* yang dipakai file bank. Kelompok:
//   - PAYMENT: CDS custom YY1_PaymentProposal (nama field belum final, $metadata menyusul)
//   - ASSET:   api_fixedasset (field deskripsi/tanggal kapitalisasi beda antar rilis)
//   - VENDOR:  API_BUSINESS_PARTNER to_BusinessPartnerBank
var defaultSAPFieldKeys = map[string]map[string][]string{
	"PAYMENT": {
		// file bank (RICEF 5)
		"PAYEE_NAME": {"PayeeName", "SupplierName", "BeneficiaryName", "BusinessPartnerName", "SupplierFullName"},
		"SHORT_NAME": {"PayeeShortName", "SupplierShortName", "SearchTerm"},
		"BANK_NAME":  {"PayeeBankName", "BankName", "BeneficiaryBankName"},
		"ACCOUNT":    {"PayeeBankAccount", "BankAccount", "BankAccountNumber", "BeneficiaryAccount"},
		"BANK_CODE":  {"PayeeBankNumber", "BankNumber", "BankKey", "BankCode"},
		"CITY":       {"PayeeCityName", "CityName", "BankCity", "City"},
		"REMARK":     {"PaymentReference", "PaymentRemark", "DocumentItemText"},
		// kolom terstruktur sap_payment_proposals
		"RUN_ID":           {"PaymentRunID", "PaymentRunIdentification", "LAUFI"},
		"RUN_DATE":         {"PaymentRunDate", "PaymentRunExecutionDate", "LAUFD"},
		"COMPANY_CODE":     {"CompanyCode", "PayingCompanyCode", "BUKRS"},
		"SUPPLIER":         {"Supplier", "Vendor", "LIFNR"},
		"PAYMENT_DOCUMENT": {"PaymentDocument", "AccountingDocument", "VBLNR"},
		"AMOUNT":           {"PaidAmountInPaytCurrency", "PaymentAmount", "AmountInPaymentCurrency", "RBETR"},
		"CURRENCY":         {"PaymentCurrency", "Currency", "WAERS"},
	},
	"ASSET": {
		"DESCRIPTION":         {"FixedAssetDescription", "MasterFixedAssetDescription", "AssetDescription"},
		"ADDITIONAL_DESC":     {"AssetAdditionalDescription", "FixedAssetAdditionalDescription"},
		"CAPITALIZATION_DATE": {"AssetCapitalizationDate", "CapitalizationDate"},
	},
	"VENDOR": {
		"BANK_KEY":       {"BankName", "BankNumber", "BankInternalID"},
		"BANK_ACCOUNT":   {"BankAccount"},
		"ACCOUNT_HOLDER": {"BankAccountHolderName"},
	},
}

// sapFieldKeys mengembalikan kandidat field untuk group/name: env FIN_<GROUP>_FIELD_<NAME>, atau default.
func sapFieldKeys(group, name string) []string {
	if v := strings.TrimSpace(envOr("FIN_"+group+"_FIELD_"+name, "")); v != "" {
		var keys []string
		for _, k := range strings.Split(v, ",") {
			if k = strings.TrimSpace(k); k != "" {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			return keys
		}
	}
	return defaultSAPFieldKeys[group][name]
}

// SAPFieldMapping menampilkan pemetaan field efektif (setelah override env) untuk Panel/diagnosa.
// Kunci map luar: "FIN_<GROUP>_FIELD_<NAME>".
func SAPFieldMapping() map[string][]string {
	out := map[string][]string{}
	for group, names := range defaultSAPFieldKeys {
		for name := range names {
			out["FIN_"+group+"_FIELD_"+name] = sapFieldKeys(group, name)
		}
	}
	return out
}

// paymentEntitySet adalah nama entity set CDS proposal pembayaran (FIN_PAYMENT_ENTITY_SET); dipakai untuk
// memotong record key dari __metadata.uri agar tidak bergantung host tenant.
func paymentEntitySet() string {
	return envOr("FIN_PAYMENT_ENTITY_SET", "YY1_PaymentProposal")
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
