//go:build integration

package service

import (
	"context"
	"testing"
	"time"

	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
)

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
	if res.Queued != 2 || res.Skipped != 1 || res.Failed != 0 {
		t.Fatalf("first sync: %+v", res)
	}
	if s := docStatus(t, ctx, repo, model.DocTypeJournalGI, "GI-1001"); s != "pending" {
		t.Fatalf("GI-1001 status %q", s)
	}
	res = fin.SyncInventoryJournals(ctx, reader, td.AddDate(0, 0, -1), 500)
	if res.Queued != 0 || res.Skipped != 3 {
		t.Fatalf("second sync must skip existing: %+v", res)
	}
}
