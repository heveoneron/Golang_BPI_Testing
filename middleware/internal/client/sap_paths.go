package client

import (
	"fmt"
	"os"
	"strings"

	"golang-bpi-testing/internal/model"
)

// ServiceRoot mengembalikan root layanan OData dari endpoint_path yang terdaftar di sap_endpoints,
// misal "/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product" -> "/sap/opu/odata/sap/API_PRODUCT_SRV".
// Path lookup/sub-request (to_Valuation, A_MaterialDocumentItem, Cancel, ...) disusun dari root ini
// sehingga perubahan path layanan cukup dilakukan di sap_endpoints (Panel), bukan di kode.
// Nama entity set dan navigasi tetap kontrak API SAP yang dirilis.
func ServiceRoot(endpointPath string) string {
	p := strings.TrimSpace(endpointPath)
	if i := strings.IndexAny(p, "?"); i >= 0 {
		p = p[:i]
	}
	if i := strings.Index(p, "("); i >= 0 { // buang key segment, mis. A_Product('X')
		p = p[:i]
	}
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return ""
}

// ServicePath menggabungkan root layanan endpoint dengan suffix (entity set/navigasi/query).
func ServicePath(ep *model.SAPEndpoint, suffix string) (string, error) {
	if ep == nil {
		return "", fmt.Errorf("endpoint SAP kosong")
	}
	root := ServiceRoot(ep.EndpointPath)
	if root == "" {
		return "", fmt.Errorf("endpoint_path '%s' untuk %s tidak valid (harus /<root layanan>/<entity set>)", ep.EndpointPath, ep.ServiceCode)
	}
	return root + "/" + strings.TrimLeft(suffix, "/"), nil
}

// Urutan field nomor dokumen yang dibaca dari response OData (diganti lewat env, daftar dipisah koma).
// Untuk SupplierInvoice OData v2, FiscalYear ditambahkan sebagai "<inv>/<fy>".
const (
	defaultDocNoFieldsV2 = "BusinessPartner,PurchaseOrder,MaterialDocument,Product,SupplierInvoice"
	defaultDocNoFieldsV4 = "PurchaseOrder,BusinessPartner,AccountingDocument,SupplierInvoice,id"
)

// DocNoFields mengembalikan kandidat field nomor dokumen SAP_RESPONSE_DOCNO_FIELDS_V2 / _V4.
func DocNoFields(odataVersion string) []string {
	key, def := "SAP_RESPONSE_DOCNO_FIELDS_V4", defaultDocNoFieldsV4
	if strings.EqualFold(odataVersion, "v2") {
		key, def = "SAP_RESPONSE_DOCNO_FIELDS_V2", defaultDocNoFieldsV2
	}
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		raw = def
	}
	var out []string
	for _, f := range strings.Split(raw, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// extractDocNo mencari nomor dokumen pada objek response sesuai DocNoFields.
func extractDocNo(obj map[string]interface{}, fields []string, appendFiscalYear bool) string {
	for _, f := range fields {
		v, ok := obj[f].(string)
		if !ok || v == "" {
			continue
		}
		if appendFiscalYear && f == "SupplierInvoice" {
			if fy, ok := obj["FiscalYear"].(string); ok && fy != "" {
				return v + "/" + fy
			}
		}
		return v
	}
	return ""
}
