package client

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
)

// GetOData menjalankan GET ke ep.BaseURL + pathAndQuery (path sudah termasuk query OData yang di-escape).
// Mode MOCK mengembalikan koleksi kosong sesuai versi OData endpoint.
func (s *SAPClient) GetOData(ep *model.SAPEndpoint, pathAndQuery string) (int, []byte, error) {
	if IsMock(ep) {
		if strings.EqualFold(ep.ODataVersion, "v4") {
			return http.StatusOK, []byte(`{"value":[]}`), nil
		}
		return http.StatusOK, []byte(`{"d":{"results":[]}}`), nil
	}

	username, password := s.credentials(ep)
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(ep.BaseURL, "/")+pathAndQuery, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("failed create GET request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpFor(username).Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("network error GET %s: %w", ep.ServiceCode, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("failed reading response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, body, fmt.Errorf("SAP GET %s HTTP %d: %s", ep.ServiceCode, resp.StatusCode, truncate(string(body), 2000))
	}
	return resp.StatusCode, body, nil
}

// ExecuteSOAP mengirim envelope SOAP XML (RICEF 2) dengan Basic Auth. Layanan SOAP SAP tidak
// memakai CSRF token.
func (s *SAPClient) ExecuteSOAP(ep *model.SAPEndpoint, envelope string) (int, string, error) {
	if IsMock(ep) {
		mock := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?><soap-env:Envelope xmlns:soap-env="http://schemas.xmlsoap.org/soap/envelope/"><soap-env:Body><n0:JournalEntryBulkCreateConfirmation xmlns:n0="http://sap.com/xi/SAPSCORE/SFIN"><JournalEntryCreateConfirmation><JournalEntryCreateConfirmation><AccountingDocument>%d</AccountingDocument><CompanyCode>5110</CompanyCode><FiscalYear>%d</FiscalYear></JournalEntryCreateConfirmation><Log><MaximumLogItemSeverityCode>1</MaximumLogItemSeverityCode><Item><Note>Document posted successfully (MOCK)</Note></Item></Log></JournalEntryCreateConfirmation></n0:JournalEntryBulkCreateConfirmation></soap-env:Body></soap-env:Envelope>`,
			100000000+time.Now().Unix()%899999999, time.Now().Year())
		return http.StatusOK, mock, nil
	}

	username, password := s.credentials(ep)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(ep.BaseURL, "/")+ep.EndpointPath, bytes.NewBufferString(envelope))
	if err != nil {
		return 0, "", fmt.Errorf("failed create SOAP request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("Accept", "text/xml")

	resp, err := s.httpFor(username).Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("network error executing SOAP to SAP Grow: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("failed reading SOAP response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return resp.StatusCode, string(body), fmt.Errorf("SAP SOAP error status %d: %s", resp.StatusCode, truncate(string(body), 2000))
	}
	return resp.StatusCode, string(body), nil
}

// JournalEntryConfirmation adalah hasil baca response JournalEntryBulkCreateConfirmation.
type JournalEntryConfirmation struct {
	AccountingDocument string
	FiscalYear         string
	MaxSeverity        string
	Notes              []string
}

// ParseJournalEntryConfirmation membaca response SOAP Journal Entry secara toleran terhadap
// namespace: mengambil AccountingDocument, FiscalYear, MaximumLogItemSeverityCode dan Note.
func ParseJournalEntryConfirmation(body string) (JournalEntryConfirmation, error) {
	var out JournalEntryConfirmation
	dec := xml.NewDecoder(strings.NewReader(body))
	var current string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, fmt.Errorf("response SOAP tidak valid: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			current = t.Name.Local
		case xml.EndElement:
			current = ""
		case xml.CharData:
			val := strings.TrimSpace(string(t))
			if val == "" {
				continue
			}
			switch current {
			case "AccountingDocument":
				if out.AccountingDocument == "" {
					out.AccountingDocument = val
				}
			case "FiscalYear":
				if out.FiscalYear == "" {
					out.FiscalYear = val
				}
			case "MaximumLogItemSeverityCode":
				if val > out.MaxSeverity {
					out.MaxSeverity = val
				}
			case "Note", "faultstring":
				out.Notes = append(out.Notes, val)
			}
		}
	}
	return out, nil
}

// Posted bernilai true bila SAP mengembalikan nomor dokumen yang valid tanpa log error.
// Severity SAP: 1 = info, 2 = warning, 3 = error, 4 = abort.
func (c JournalEntryConfirmation) Posted() bool {
	doc := strings.Trim(c.AccountingDocument, "0")
	return doc != "" && c.MaxSeverity < "3"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
