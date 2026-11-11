//go:build integration

package service

import (
	"context"
	"testing"
	"time"

	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
)

func TestIntegration_AssetEditRequiresReprint(t *testing.T) {
	ctx, repo, fin, _ := setupIntegration(t)
	if _, err := repo.UpsertFixedAssets(ctx, []model.FixedAsset{{CompanyCode: "1000", MasterFixedAsset: "50000001", FixedAsset: "0", Description: "Pompa", Raw: []byte(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	base := model.AssetAcquisitionRequest{Reference: "AQ-EDIT-1", CompanyCode: "1000", MasterFixedAsset: "50000001", FixedAsset: "0",
		DocumentDate: "2026-11-05", PostingDate: "2026-11-05", Amount: 1000, OffsettingAccount: "199999", ParkedBy: "maker"}
	a, err := fin.ParkAssetAcquisition(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fin.PrintAssetAcquisition(ctx, a.ID, "maker"); err != nil {
		t.Fatal(err)
	}
	edit := base
	edit.Reference, edit.Amount = "IGNORED", 2500
	a, err = fin.EditAssetAcquisition(ctx, a.ID, edit, "editor")
	if err != nil || a.Reference != "AQ-EDIT-1" || a.Amount != 2500 || a.ParkedBy != "editor" || a.Status != "PARKED" || a.PrintedAt != nil {
		t.Fatalf("edit result wrong: %+v %v", a, err)
	}
	if _, err := fin.ApproveAssetAcquisition(ctx, a.ID, "checker"); err == nil {
		t.Fatal("approve after edit must require reprint")
	}
	if _, err := fin.PrintAssetAcquisition(ctx, a.ID, "checker"); err != nil {
		t.Fatal(err)
	}
	if _, err := fin.ApproveAssetAcquisition(ctx, a.ID, "editor"); err == nil {
		t.Fatal("editor is the new maker and must not approve")
	}
	if a, err = fin.ApproveAssetAcquisition(ctx, a.ID, "checker"); err != nil || a.Status != "APPROVED" {
		t.Fatalf("approve failed: %+v %v", a, err)
	}
	if _, err := fin.EditAssetAcquisition(ctx, a.ID, edit, "editor"); err == nil {
		t.Fatal("APPROVED parking must not be editable")
	}
}

func TestIntegration_SAPEnvironmentSwitch(t *testing.T) {
	ctx, repo, fin, _ := setupIntegration(t)
	if _, err := fin.SetSAPBaseURL(ctx, "https://evil.example.com", true); err == nil {
		t.Fatal("host outside allowlist must be rejected")
	}
	if _, err := fin.SetSAPBaseURL(ctx, "https://my446174-api.s4hana.cloud.sap", false); err != nil {
		t.Fatal(err)
	}
	env, _ := fin.GetSAPEnvironment(ctx)
	if len(env.BaseURLs) != 1 || env.BaseURLs[0].BaseURL != "MOCK" {
		t.Fatalf("MOCK endpoints must stay MOCK unless include_mock: %+v", env.BaseURLs)
	}
	n, err := fin.SetSAPBaseURL(ctx, "https://my446174-api.s4hana.cloud.sap/", true)
	if err != nil || n == 0 {
		t.Fatalf("switch failed: %d %v", n, err)
	}
	ep, _ := repo.GetSAPEndpointByCode(ctx, "PRODUCT")
	if ep == nil || ep.BaseURL != "https://my446174-api.s4hana.cloud.sap" {
		t.Fatalf("endpoint not switched (or reset by seed): %+v", ep)
	}
	if _, err := fin.SetSAPBaseURL(ctx, "MOCK", true); err != nil {
		t.Fatal(err)
	}
}

type fakeInventoryReader struct {
	rows []repository.InventoryStagingRow
}

func (f fakeInventoryReader) ExtractInventoryStaging(context.Context, time.Time, int) ([]repository.InventoryStagingRow, error) {
	return f.rows, nil
}

func TestIntegration_InventoryJournalSync(t *testing.T) {
	ctx, repo, fin, _ := setupIntegration(t)
	if tag, err := repo.Pool.Exec(ctx, `UPDATE sap_endpoints SET base_url = 'MOCK', is_active = true WHERE service_code = 'PRODUCT'`); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("PRODUCT endpoint harus ada di schema.sql: %v", err)
	}
	td := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	reader := fakeInventoryReader{rows: []repository.InventoryStagingRow{
		{Source: "axinvissue", SourceID: 1001, ItemNum: "SP001", Quantity: 2, TransDate: td, TransType: "ISSUE", SiteID: "BTNG"},
		{Source: "axinvtrans", SourceID: 2001, ItemNum: "SP002", Quantity: -1, TransDate: td, TransType: "ADJUST"},
		{Source: "axinvissue", SourceID: 1002, ItemNum: "SP003", Quantity: 1, TransDate: td, TransType: "RETURN"},
	}}
	res := fin.SyncInventoryJournals(ctx, reader, td.AddDate(0, 0, -1), 500)
	if res.Queued != 3 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("first sync (RETURN ikut diposting): %+v", res)
	}
	if s := docStatus(t, ctx, repo, model.DocTypeJournalGI, "GI-1001"); s != "pending" {
		t.Fatalf("GI-1001 status %q", s)
	}
	res = fin.SyncInventoryJournals(ctx, reader, td.AddDate(0, 0, -1), 500)
	if res.Queued != 0 || res.Skipped != 3 {
		t.Fatalf("second sync must skip existing: %+v", res)
	}
}
