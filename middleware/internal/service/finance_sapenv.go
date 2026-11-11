package service

import (
	"context"
	"net/url"
	"sort"
	"strings"

	"golang-bpi-testing/internal/repository"
)

// SAPEnvironment adalah konfigurasi tenant SAP yang aktif (host & company code).
type SAPEnvironment struct {
	BaseURLs     []repository.SAPBaseURLUsage `json:"base_urls"`
	AllowedHosts []string                     `json:"allowed_hosts"`
	CompanyCode  string                       `json:"company_code"`
}

func allowedSAPHosts() []string {
	var out []string
	for h := range envSet("FIN_SAP_ALLOWED_HOSTS", "my445160-api.s4hana.cloud.sap,my446174-api.s4hana.cloud.sap") {
		out = append(out, strings.ToLower(h))
	}
	sort.Strings(out)
	return out
}

// NormalizeSAPBaseURL memvalidasi base URL: "MOCK" atau https://<host di FIN_SAP_ALLOWED_HOSTS> tanpa path.
// Daftar host dibatasi karena kredensial SAP dikirim ke base URL ini.
func NormalizeSAPBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, "MOCK") {
		return "MOCK", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.User != nil {
		return "", newValidation("base_url harus https://<host> tanpa path, atau MOCK")
	}
	host := strings.ToLower(u.Host)
	for _, h := range allowedSAPHosts() {
		if host == h {
			return "https://" + host, nil
		}
	}
	return "", newValidation("host " + host + " tidak ada di FIN_SAP_ALLOWED_HOSTS")
}

// GetSAPEnvironment menampilkan base URL yang sedang dipakai endpoint SAP.
func (f *FinanceService) GetSAPEnvironment(ctx context.Context) (*SAPEnvironment, error) {
	usage, err := f.PGRepo.ListSAPBaseURLs(ctx)
	if err != nil {
		return nil, err
	}
	return &SAPEnvironment{BaseURLs: usage, AllowedHosts: allowedSAPHosts(), CompanyCode: DefaultCompanyCode()}, nil
}

// SetSAPBaseURL memindahkan seluruh endpoint SAP ke tenant lain (misal my445160 <-> my446174).
func (f *FinanceService) SetSAPBaseURL(ctx context.Context, baseURL string, includeMock bool) (int64, error) {
	clean, err := NormalizeSAPBaseURL(baseURL)
	if err != nil {
		return 0, err
	}
	return f.PGRepo.SetSAPBaseURL(ctx, clean, includeMock)
}
