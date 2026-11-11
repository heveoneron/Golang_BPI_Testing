package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang-bpi-testing/internal/model"
)

func TestServiceRoot(t *testing.T) {
	cases := map[string]string{
		"/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product":                                 "/sap/opu/odata/sap/API_PRODUCT_SRV",
		"/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader?$top=1": "/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV",
		"/sap/opu/odata/sap/API_BUSINESS_PARTNER/A_BusinessPartner('1')":               "/sap/opu/odata/sap/API_BUSINESS_PARTNER",
		"/sap/opu/odata/sap/API_SUPPLIERINVOICE_PROCESS_SRV/":                          "/sap/opu/odata/sap",
		"/A_Product": "",
		"":           "",
	}
	for in, want := range cases {
		if got := ServiceRoot(in); got != want {
			t.Errorf("ServiceRoot(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ServicePath(&model.SAPEndpoint{ServiceCode: "X", EndpointPath: ""}, "A_Product"); err == nil {
		t.Error("empty endpoint_path must error")
	}
}

func TestDocNoFieldsOverride(t *testing.T) {
	if got := DocNoFields("v2"); strings.Join(got, ",") != defaultDocNoFieldsV2 {
		t.Fatalf("default v2: %v", got)
	}
	t.Setenv("SAP_RESPONSE_DOCNO_FIELDS_V4", " CustomDoc , id ")
	if got := DocNoFields("v4"); len(got) != 2 || got[0] != "CustomDoc" {
		t.Fatalf("override v4: %v", got)
	}
	if no := extractDocNo(map[string]interface{}{"SupplierInvoice": "51", "FiscalYear": "2026"}, DocNoFields("v2"), true); no != "51/2026" {
		t.Fatalf("v2 supplier invoice: %q", no)
	}
}

// Path lookup mengikuti endpoint_path yang terdaftar, bukan path literal.
func TestProductValuationUsesEndpointPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"d":{"results":[{"ValuationClass":"S001","InventoryValuationProcedure":"V","MovingAveragePrice":"10","PriceUnitQty":"1","Currency":"USD"}]}}`))
	}))
	defer srv.Close()
	c := NewSAPClient(srv.URL, "u", "p")

	ep := &model.SAPEndpoint{ServiceCode: "PRODUCT", BaseURL: srv.URL, EndpointPath: "/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product"}
	if v, err := c.GetProductValuation(ep, "SP001"); err != nil || v.Class != "S001" {
		t.Fatalf("valuation: %+v %v", v, err)
	}
	if gotPath != "/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product('SP001')/to_Valuation" {
		t.Fatalf("default path changed: %s", gotPath)
	}

	ep.EndpointPath = "/sap/opu/odata/sap/ZAPI_PRODUCT_SRV/A_Product"
	if _, err := c.GetProductValuation(ep, "SP001"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotPath, "/sap/opu/odata/sap/ZAPI_PRODUCT_SRV/") {
		t.Fatalf("path must follow endpoint_path: %s", gotPath)
	}
}
