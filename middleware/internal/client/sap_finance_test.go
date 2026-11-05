package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang-bpi-testing/internal/model"
)

func TestExecuteSOAP_SendsXMLWithBasicAuth(t *testing.T) {
	var gotCT, gotUser, gotBody, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotUser, _, _ = r.BasicAuth()
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`<Envelope><Body><Conf><AccountingDocument>1900000123</AccountingDocument><FiscalYear>2026</FiscalYear><Log><MaximumLogItemSeverityCode>1</MaximumLogItemSeverityCode></Log></Conf></Body></Envelope>`))
	}))
	defer srv.Close()

	c := NewSAPClient(srv.URL, "default", "pw")
	ep := &model.SAPEndpoint{ServiceCode: "JOURNAL_GI", BaseURL: srv.URL, EndpointPath: "/sap/bc/srt/scs_ext/sap/journalentrycreaterequestconfi", Username: "Y_BPI_CU_SAP_COM_0002"}
	code, body, err := c.ExecuteSOAP(ep, "<x/>")
	if err != nil || code != 200 {
		t.Fatalf("unexpected: %v %d", err, code)
	}
	if gotCT != "text/xml; charset=utf-8" || gotUser != "Y_BPI_CU_SAP_COM_0002" || gotBody != "<x/>" || !strings.HasSuffix(gotPath, "journalentrycreaterequestconfi") {
		t.Errorf("unexpected request: ct=%q user=%q body=%q path=%q", gotCT, gotUser, gotBody, gotPath)
	}
	conf, err := ParseJournalEntryConfirmation(body)
	if err != nil || !conf.Posted() || conf.AccountingDocument != "1900000123" || conf.FiscalYear != "2026" {
		t.Errorf("unexpected confirmation %+v err=%v", conf, err)
	}
}

func TestParseJournalEntryConfirmation_Error(t *testing.T) {
	body := `<Envelope><Body><Conf><AccountingDocument>0000000000</AccountingDocument><Log><MaximumLogItemSeverityCode>3</MaximumLogItemSeverityCode><Item><Note>Account 513043 does not exist</Note></Item></Log></Conf></Body></Envelope>`
	conf, err := ParseJournalEntryConfirmation(body)
	if err != nil {
		t.Fatal(err)
	}
	if conf.Posted() {
		t.Error("zero document with severity 3 must not be treated as posted")
	}
	if len(conf.Notes) != 1 || !strings.Contains(conf.Notes[0], "does not exist") {
		t.Errorf("unexpected notes %v", conf.Notes)
	}
	if _, err := ParseJournalEntryConfirmation("<broken"); err == nil {
		t.Error("broken XML must error")
	}
}

func TestGetOData_MockAndHTTPError(t *testing.T) {
	c := NewSAPClient("MOCK", "", "")
	_, body, err := c.GetOData(&model.SAPEndpoint{BaseURL: "MOCK", ODataVersion: "v4"}, "/x")
	if err != nil || string(body) != `{"value":[]}` {
		t.Errorf("unexpected mock v4 response %s %v", body, err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") != "500" {
			t.Errorf("query not forwarded: %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("no auth"))
	}))
	defer srv.Close()
	code, _, err := c.GetOData(&model.SAPEndpoint{ServiceCode: "FIXED_ASSET", BaseURL: srv.URL}, "/FixedAsset?$top=500")
	if err == nil || code != http.StatusForbidden {
		t.Errorf("expected 403 error, got %d %v", code, err)
	}
}

func TestHTTPFor_SeparateCookieJarPerUser(t *testing.T) {
	c := NewSAPClient("MOCK", "", "")
	a1, a2, b := c.httpFor("USER_A"), c.httpFor("USER_A"), c.httpFor("USER_B")
	if a1 != a2 {
		t.Error("same user must reuse client")
	}
	if a1 == b || a1.Jar == b.Jar {
		t.Error("different users must not share cookie jar")
	}
}
