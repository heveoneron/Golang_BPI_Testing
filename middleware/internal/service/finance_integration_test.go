//go:build integration

// Uji integrasi RICEF 1-9 terhadap PostgreSQL sungguhan (SAP dalam mode MOCK).
// Jalankan dengan:
//   FIN_TEST_PG="postgres://postgres:pw@localhost:5432/db?sslmode=disable" go test -tags integration ./internal/service/ -run Integration -v
// Database harus sudah berisi database/schema.sql dan boleh dikosongkan oleh test ini.

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"golang-bpi-testing/internal/client"
	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"

	"github.com/jackc/pgx/v5/pgxpool"
)

func setupIntegration(t *testing.T) (context.Context, *repository.PostgresRepository, *FinanceService, *SyncService) {
	t.Helper()
	dsn := os.Getenv("FIN_TEST_PG")
	if dsn == "" {
		t.Skip("FIN_TEST_PG tidak diatur")
	}
	t.Setenv("SAP_BASE_URL", "MOCK")
	t.Setenv("AUTO_PULL_ENABLED", "false")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	repo := repository.NewPostgresRepository(pool)
	for i := 0; i < 2; i++ { // migrasi harus idempotent
		if err := repo.MigrateFinance(ctx); err != nil {
			t.Fatalf("migrate #%d: %v", i+1, err)
		}
	}
	_, err = pool.Exec(ctx, `
		TRUNCATE documents, budget_masters, budget_transaction_locks, budget_adjustments, sap_gl_line_items,
		         sap_payment_proposals, sap_fixed_assets, asset_acquisition_prints, asset_acquisitions, excel_upload_batches, exchange_rates, integration_logs,
		         employee_vendor_mappings;
		UPDATE sap_endpoints SET base_url = 'MOCK';
		UPDATE sap_endpoints SET is_active = true WHERE service_code = 'BUDGET';`)
	if err != nil {
		t.Fatal(err)
	}
	sap := client.NewSAPClient("MOCK", "", "")
	return ctx, repo, NewFinanceService(repo, sap), NewSyncService(nil, repo, sap)
}

func docStatus(t *testing.T, ctx context.Context, repo *repository.PostgresRepository, docType, docNo string) string {
	t.Helper()
	s, found, err := repo.GetDocumentStatus(ctx, docType, docNo)
	if err != nil || !found {
		t.Fatalf("document %s %s not found: %v", docType, docNo, err)
	}
	return s
}

func lockStatuses(t *testing.T, ctx context.Context, repo *repository.PostgresRepository, refType string) map[string]string {
	t.Helper()
	locks, err := repo.ListBudgetLocks(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, l := range locks {
		if l.ReferenceDocType == refType {
			out[l.ReferenceDocNo] = l.LockStatus
		}
	}
	return out
}

func TestIntegration_BudgetSupplementTransfer(t *testing.T) {
	ctx, repo, fin, sync := setupIntegration(t)
	if err := repo.UpsertBudgetMaster(ctx, model.BudgetMaster{BudgetCombination: "CC1-B1", BudgetYear: 2026, BudgetCode: "B1", Type: "OPX", ApprovedBudget: 1000}); err != nil {
		t.Fatal(err)
	}

	// Transfer melebihi sisa pagu ditolak.
	_, err := fin.TransferBudget(ctx, model.BudgetTransferRequest{FromMainAccount: "CC1", FromBudgetCode: "B1", ToMainAccount: "CC2", ToBudgetCode: "B2", Year: 2026, Amount: 5000})
	var vErr *ValidationError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected validation error for insufficient budget, got %v", err)
	}

	sup, err := fin.SupplementBudget(ctx, model.BudgetSupplementRequest{MainAccount: "CC1", BudgetCode: "B1", Year: 2026, Amount: 500})
	if err != nil {
		t.Fatal(err)
	}
	trf, err := fin.TransferBudget(ctx, model.BudgetTransferRequest{FromMainAccount: "CC1", FromBudgetCode: "B1", ToMainAccount: "CC2", ToBudgetCode: "B2", Year: 2026, Amount: 300})
	if err != nil {
		t.Fatal(err)
	}
	// Sebelum SAP sukses: pagu asal ditahan 300.
	masters, _ := repo.ListBudgetMasters(ctx, 2026)
	if len(masters) != 1 || masters[0].ApprovedBudget != 1000 || masters[0].RemainingBudget != 700 {
		t.Fatalf("unexpected masters before push: %+v", masters)
	}

	if n := sync.PushDocumentsToSAP(ctx); n != 2 {
		t.Fatalf("expected 2 budget docs pushed, got %d", n)
	}
	if docStatus(t, ctx, repo, model.DocTypeBudget, sup.DocumentNo) != "success" {
		t.Fatal("supplement not success")
	}
	masters, _ = repo.ListBudgetMasters(ctx, 2026)
	got := map[string][2]float64{}
	for _, m := range masters {
		got[m.BudgetCombination] = [2]float64{m.ApprovedBudget, m.RemainingBudget}
		if m.BudgetCombination == "CC2-B2" && m.BudgetCode != "B2" {
			t.Errorf("new combination must derive budget_code, got %q", m.BudgetCode)
		}
		if m.BudgetCombination == "CC1-B1" && (m.Type != "OPX" || m.Status != "APPR") {
			t.Errorf("type/status not kept: %+v", m)
		}
	}
	// CC1: 1000 + 500 - 300 = 1200 (lock transfer dilepas), CC2: 300.
	if got["CC1-B1"] != [2]float64{1200, 1200} || got["CC2-B2"] != [2]float64{300, 300} {
		t.Fatalf("unexpected masters after push: %v", got)
	}
	if lockStatuses(t, ctx, repo, "BUDGET_TRANSFER")[trf.DocumentNo] != "RELEASED" {
		t.Error("transfer lock must be released after apply")
	}
	adjs, _ := repo.ListBudgetAdjustments(ctx, 10)
	for _, a := range adjs {
		if a.Status != "APPLIED" {
			t.Errorf("adjustment %s status %s", a.DocumentNo, a.Status)
		}
	}
}

func TestIntegration_JournalEntryLocksAndCommits(t *testing.T) {
	ctx, repo, fin, sync := setupIntegration(t)
	_ = repo.UpsertBudgetMaster(ctx, model.BudgetMaster{BudgetCombination: "513043-10081", BudgetYear: 2026, ApprovedBudget: 2000000})

	req := validJournal()
	ref, err := fin.CreateJournalEntry(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	locks := lockStatuses(t, ctx, repo, model.DocTypeJournalGI)
	if locks[ref+"|513043-10081"] != "RESERVED" {
		t.Fatalf("expected RESERVED lock, got %v", locks)
	}

	// Jurnal kedua melebihi sisa pagu (2.000.000 - 1.500.000) ditolak dan tidak meninggalkan lock.
	req2 := validJournal()
	req2.Reference = "JE-TEST-002"
	if _, err := fin.CreateJournalEntry(ctx, req2); err == nil {
		t.Fatal("expected budget deficit")
	}
	if _, ok := lockStatuses(t, ctx, repo, model.DocTypeJournalGI)["JE-TEST-002|513043-10081"]; ok {
		t.Error("rejected journal must not keep a lock row")
	}

	if n := sync.PushDocumentsToSAP(ctx); n != 1 {
		t.Fatalf("expected 1 journal pushed, got %d", n)
	}
	if docStatus(t, ctx, repo, model.DocTypeJournalGI, ref) != "success" {
		t.Fatal("journal not success")
	}
	if lockStatuses(t, ctx, repo, model.DocTypeJournalGI)[ref+"|513043-10081"] != "COMMITTED" {
		t.Error("lock must be COMMITTED after SAP success")
	}
	if _, err := fin.CreateJournalEntry(ctx, validJournal()); !errors.Is(err, repository.ErrAlreadyPosted) {
		t.Errorf("re-submitting posted journal must return ErrAlreadyPosted, got %v", err)
	}
}

func TestIntegration_JournalRejectedBySAPReleasesLock(t *testing.T) {
	ctx, repo, fin, sync := setupIntegration(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<Envelope><Body><Conf><AccountingDocument>0000000000</AccountingDocument><Log><MaximumLogItemSeverityCode>3</MaximumLogItemSeverityCode><Item><Note>GL 513043 is blocked</Note></Item></Log></Conf></Body></Envelope>`))
	}))
	defer srv.Close()
	_, _ = repo.Pool.Exec(ctx, `UPDATE sap_endpoints SET base_url = $1 WHERE service_code = 'JOURNAL_GI'`, srv.URL)
	_ = repo.UpsertBudgetMaster(ctx, model.BudgetMaster{BudgetCombination: "513043-10081", BudgetYear: 2026, ApprovedBudget: 2000000})

	ref, err := fin.CreateJournalEntry(ctx, validJournal())
	if err != nil {
		t.Fatal(err)
	}
	if n := sync.PushDocumentsToSAP(ctx); n != 0 {
		t.Fatalf("rejected journal must not count as pushed, got %d", n)
	}
	if s := docStatus(t, ctx, repo, model.DocTypeJournalGI, ref); s != "error_data" {
		t.Fatalf("expected error_data, got %s", s)
	}
	if lockStatuses(t, ctx, repo, model.DocTypeJournalGI)[ref+"|513043-10081"] != "RELEASED" {
		t.Error("lock must be RELEASED after SAP rejection")
	}
	docs, _ := repo.ListDocumentsByType(ctx, model.DocTypeJournalGI, 1)
	if len(docs) != 1 || docs[0].ErrorMessage == nil || !strings.Contains(*docs[0].ErrorMessage, "GL 513043 is blocked") {
		t.Errorf("SAP log note must be stored as error message: %+v", docs)
	}
}

func TestIntegration_ExchangeRateQueue(t *testing.T) {
	ctx, repo, fin, sync := setupIntegration(t)
	payload := &model.MaximoExchangeRatePayload{
		Event: "KURS", Source: "BPI",
		Period: model.ExchangeRatePeriod{KMKNumber: "KMK-TEST", ValidFrom: "2026-11-05", ValidTo: "2026-11-11"},
		Rates:  []model.ExchangeRateItem{{CurrencyCode: "USD", Rate: 16500, Unit: 1}, {CurrencyCode: "JPY", Rate: 11000, Unit: 100}},
	}
	if _, err := repo.SaveExchangeRates(ctx, payload, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	res, err := fin.QueueExchangeRates(ctx, "")
	if err != nil || len(res.Queued) != 2 || res.KMKNumber != "KMK-TEST" {
		t.Fatalf("unexpected queue result %+v %v", res, err)
	}
	if n := sync.PushDocumentsToSAP(ctx); n != 2 {
		t.Fatalf("expected 2 pushed, got %d", n)
	}
	res, _ = fin.QueueExchangeRates(ctx, "KMK-TEST")
	if len(res.Queued) != 0 || len(res.Skipped) != 2 {
		t.Errorf("already posted rates must be skipped: %+v", res)
	}
}

func TestIntegration_SupplierInvoiceUpload(t *testing.T) {
	ctx, repo, fin, sync := setupIntegration(t)
	res, err := fin.UploadSupplierInvoices(ctx, "bt.csv", "finance01", []byte(invoiceCSV))
	if err != nil || res.Batch.Status != "QUEUED" || len(res.Invoices) != 2 {
		t.Fatalf("unexpected upload %+v %v", res, err)
	}
	if n := sync.PushDocumentsToSAP(ctx); n != 2 {
		t.Fatalf("expected 2 invoices pushed, got %d", n)
	}
	res, err = fin.UploadSupplierInvoices(ctx, "bt.csv", "finance01", []byte(invoiceCSV))
	if err != nil || res.Batch.Status != "REJECTED" || len(res.Batch.Errors) != 2 {
		t.Fatalf("re-upload of posted invoices must be rejected: %+v %v", res, err)
	}
	list, _ := fin.ListSupplierInvoices(ctx, 10)
	if len(list) != 2 || list[0].Status != "success" || list[0].InvoicingParty == "" {
		t.Errorf("unexpected monitoring list %+v", list)
	}
	batches, _ := repo.ListUploadBatches(ctx, 10)
	if len(batches) != 2 || batches[0].Status != "REJECTED" || len(batches[0].Errors) != 2 {
		t.Errorf("unexpected batches %+v", batches)
	}
}

func TestIntegration_AssetParkApprovePost(t *testing.T) {
	ctx, repo, fin, sync := setupIntegration(t)
	req := model.AssetAcquisitionRequest{Reference: "AQ-1", MasterFixedAsset: "50000000", DocumentDate: "2026-11-05", PostingDate: "2026-11-05",
		Amount: 250000000, OffsettingAccount: "199999", ParkedBy: "maker"}

	if _, err := fin.ParkAssetAcquisition(ctx, req); err == nil {
		t.Fatal("parking must fail when asset is not synced yet")
	}
	if _, err := repo.UpsertFixedAssets(ctx, []model.FixedAsset{{CompanyCode: "5110", MasterFixedAsset: "50000000", FixedAsset: "0", Description: "Pompa", Raw: []byte(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	a, err := fin.ParkAssetAcquisition(ctx, req)
	if err != nil || a.Status != "PARKED" {
		t.Fatalf("park failed: %+v %v", a, err)
	}
	if _, err := fin.ParkAssetAcquisition(ctx, req); err == nil {
		t.Error("duplicate reference must be rejected")
	}
	if _, err := fin.ApproveAssetAcquisition(ctx, a.ID, "checker"); err == nil {
		t.Fatal("approve must require Print Form first")
	}
	if p, err := fin.PrintAssetAcquisition(ctx, a.ID, "checker"); err != nil || p.PrintedAt == nil {
		t.Fatalf("print failed: %+v %v", p, err)
	}
	if _, err := fin.ApproveAssetAcquisition(ctx, a.ID, "MAKER"); err == nil {
		t.Fatal("maker must not approve own parking")
	}
	a, err = fin.ApproveAssetAcquisition(ctx, a.ID, "checker")
	if err != nil || a.Status != "APPROVED" || a.ApprovedBy != "checker" {
		t.Fatalf("approve failed: %+v %v", a, err)
	}
	if _, err := fin.RejectAssetAcquisition(ctx, a.ID, "checker", "x"); err == nil {
		t.Error("approved parking must not be rejectable")
	}
	if n := sync.PushDocumentsToSAP(ctx); n != 1 {
		t.Fatalf("expected 1 asset doc pushed, got %d", n)
	}
	a, _ = repo.GetAssetAcquisition(ctx, a.ID)
	if a.Status != "POSTED" || a.SAPDocumentNo == "" {
		t.Errorf("asset must be POSTED with SAP doc no: %+v", a)
	}
}

func TestIntegration_GLUpsertAndFilter(t *testing.T) {
	ctx, repo, _, _ := setupIntegration(t)
	d1, d2 := "2026-11-01", "2026-11-05"
	items := []model.GLLineItem{
		{Key: "K1", CompanyCode: "5110", FiscalYear: "2026", AccountingDocument: "100", LedgerGLLineItem: "1", AccountingDocumentType: "SA", PostingDate: &d1, GLAccount: "513043", AmountInCompanyCodeCurrency: 10, Raw: []byte(`{}`)},
		{Key: "K2", CompanyCode: "5110", FiscalYear: "2026", AccountingDocument: "200", LedgerGLLineItem: "1", AccountingDocumentType: "KR", PostingDate: &d2, GLAccount: "211001", AmountInCompanyCodeCurrency: 20, Raw: []byte(`{}`)},
	}
	if n, err := repo.UpsertGLLineItems(ctx, items); err != nil || n != 2 {
		t.Fatalf("upsert: %d %v", n, err)
	}
	items[0].AmountInCompanyCodeCurrency = 15
	_, _ = repo.UpsertGLLineItems(ctx, items[:1])

	all, err := repo.ListGLLineItems(ctx, repository.GLLineItemFilter{DocTypes: []string{}})
	if err != nil || len(all) != 2 {
		t.Fatalf("expected 2 rows with empty doc types, got %d %v", len(all), err)
	}
	inv, _ := repo.ListGLLineItems(ctx, repository.GLLineItemFilter{DocTypes: []string{"KR"}})
	if len(inv) != 1 || inv[0].Key != "K2" {
		t.Errorf("doc type filter failed: %+v", inv)
	}
	ranged, _ := repo.ListGLLineItems(ctx, repository.GLLineItemFilter{FromDate: "2026-11-02", ToDate: "2026-11-30", Search: "200"})
	if len(ranged) != 1 {
		t.Errorf("date/search filter failed: %+v", ranged)
	}
	k1, _ := repo.ListGLLineItems(ctx, repository.GLLineItemFilter{Search: "513043"})
	if len(k1) != 1 || k1[0].AmountInCompanyCodeCurrency != 15 {
		t.Errorf("upsert must update amount: %+v", k1)
	}
}

func TestIntegration_SkippedAndStaleRecovery(t *testing.T) {
	ctx, repo, fin, sync := setupIntegration(t)
	_, _ = repo.Pool.Exec(ctx, `UPDATE sap_endpoints SET is_active = false WHERE service_code = 'BUDGET'`)
	sup, err := fin.SupplementBudget(ctx, model.BudgetSupplementRequest{MainAccount: "CC9", BudgetCode: "B9", Amount: 10})
	if err != nil {
		t.Fatal(err)
	}
	sync.PushDocumentsToSAP(ctx)
	if s := docStatus(t, ctx, repo, model.DocTypeBudget, sup.DocumentNo); s != "skipped" {
		t.Fatalf("inactive endpoint must mark skipped, got %s", s)
	}
	// Belum 10 menit: tidak diambil ulang.
	docs, _ := repo.FetchAndLockPendingDocuments(ctx, 10)
	if len(docs) != 0 {
		t.Fatalf("skipped doc must wait before retry, got %d", len(docs))
	}
	_, _ = repo.Pool.Exec(ctx, `UPDATE documents SET last_attempt_at = NOW() - INTERVAL '11 minutes'`)
	_, _ = repo.Pool.Exec(ctx, `UPDATE sap_endpoints SET is_active = true WHERE service_code = 'BUDGET'`)
	if n := sync.PushDocumentsToSAP(ctx); n != 1 {
		t.Fatalf("skipped doc must be retried after endpoint re-activated, got %d", n)
	}

	// Dokumen macet di 'processing' (worker crash) dipulihkan setelah 10 menit.
	sup2, _ := fin.SupplementBudget(ctx, model.BudgetSupplementRequest{MainAccount: "CC8", BudgetCode: "B8", Amount: 10})
	_, _ = repo.Pool.Exec(ctx, `UPDATE documents SET status = 'processing', last_attempt_at = $2 WHERE document_no = $1`, sup2.DocumentNo, time.Now().Add(-15*time.Minute))
	if n := sync.PushDocumentsToSAP(ctx); n != 1 {
		t.Fatalf("stale processing doc must be recovered, got %d", n)
	}
}

func TestIntegration_POBudgetLockPerMainAccount(t *testing.T) {
	ctx, repo, _, sync := setupIntegration(t)
	_ = repo.UpsertBudgetMaster(ctx, model.BudgetMaster{BudgetCombination: "513043-28011", BudgetYear: 2027, ApprovedBudget: 1000})
	_ = repo.UpsertBudgetMaster(ctx, model.BudgetMaster{BudgetCombination: "513044-28011", BudgetYear: 2027, ApprovedBudget: 100})

	po := func(qty2 float64) []byte {
		return []byte(fmt.Sprintf(`{"PONUM":"4500099001","VENDOR":"V1","ORDERDATE":"2026-11-05","LINES":[
			{"POLINENUM":1,"ITEMNUM":"A","QTY":2,"UNITCOST":200,"BUDGETCODE":"28011","MAINACCOUNT":"513043","DEPARTMENT":"51101101","BUDGETYEAR":"2027"},
			{"POLINENUM":2,"ITEMNUM":"B","QTY":1,"UNITCOST":100,"BUDGETCODE":"28011","MAINACCOUNT":"513043","DEPARTMENT":"51101102","BUDGETYEAR":"2027"},
			{"POLINENUM":3,"ITEMNUM":"C","QTY":%g,"UNITCOST":50,"BUDGETCODE":"28011","MAINACCOUNT":"513044","BUDGETYEAR":"2027"}]}`, qty2))
	}
	if _, err := sync.TransformPO(ctx, po(1)); err != nil {
		t.Fatal(err)
	}
	locks, _ := repo.ListBudgetLocks(ctx, "RESERVED", 10)
	amounts := map[string]float64{}
	for _, l := range locks {
		if l.BudgetYear != 2027 {
			t.Errorf("BUDGETYEAR must be used, got %d", l.BudgetYear)
		}
		amounts[l.ReferenceDocNo] = l.LockAmount
	}
	// Dua baris 513043 (cost center berbeda) digabung menjadi satu kombinasi MainAccount-BudgetCode.
	if amounts["4500099001|513043-28011"] != 500 || amounts["4500099001|513044-28011"] != 50 || len(amounts) != 2 {
		t.Fatalf("unexpected PO locks %v", amounts)
	}
	// Revisi PO melebihi pagu 513044 (3 x 50 = 150 > 100): ditolak dan seluruh lock PO ini dilepas.
	if _, err := sync.TransformPO(ctx, po(3)); err == nil {
		t.Fatal("expected budget deficit on revised PO")
	}
	if active, _ := repo.ListBudgetLocks(ctx, "RESERVED", 10); len(active) != 0 {
		t.Errorf("all PO locks must be released after rejected revision, got %+v", active)
	}
}

type fakeMaximoWriter struct {
	failKey string
	calls   [][]model.MxInvoiceRow
	next    int64
}

func (w *fakeMaximoWriter) WriteMxInvoiceMessage(_ context.Context, rows []model.MxInvoiceRow) ([]int64, error) {
	if w.failKey != "" && rows[0].InvoiceNum == w.failKey {
		return nil, fmt.Errorf("simulasi: koneksi SQL Server putus")
	}
	w.calls = append(w.calls, rows)
	ids := make([]int64, len(rows))
	for i := range rows {
		w.next++
		ids[i] = w.next
	}
	return ids, nil
}

func TestIntegration_ExportToMaximo(t *testing.T) {
	ctx, repo, fin, _ := setupIntegration(t)
	_, _ = repo.Pool.Exec(ctx, `TRUNCATE sap_supplier_invoices, maximo_invoice_exports`)
	// Dokumen PO & GRN yang pernah dikirim middleware (sumber itemnum, budget code, nomor GRN Maximo).
	if err := repo.QueueDocument(ctx, model.Document{DocType: "PO", DocumentNo: "4500011147",
		RawPayload: []byte(`{"PONUM":"4500011147","LINES":[{"POLINENUM":1,"ITEMNUM":"68-00045","BUDGETCODE":"43071","MAINACCOUNT":"114001"}]}`),
		SAPPayload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	_ = repo.QueueDocument(ctx, model.Document{DocType: "GRN", DocumentNo: "038/RI/VI/24", RawPayload: []byte(`{}`), SAPPayload: []byte(`{}`)})
	_, _ = repo.Pool.Exec(ctx, `UPDATE documents SET status = 'success', sap_document_no = '5000000096' WHERE doc_type = 'GRN'`)

	recs, _ := ParseSAPSupplierInvoices([]byte(sapInvoiceJSON))
	if _, err := repo.UpsertSAPSupplierInvoices(ctx, recs); err != nil {
		t.Fatal(err)
	}
	d := "2026-11-05"
	_, _ = repo.UpsertGLLineItems(ctx, []model.GLLineItem{
		{Key: "J1", CompanyCode: "5110", FiscalYear: "2026", AccountingDocument: "100000123", LedgerGLLineItem: "000001", AccountingDocumentType: "SA", DebitCreditCode: "S", GLAccount: "513043", CostCenter: "10081", AmountInTransactionCurrency: 1000, TransactionCurrency: "IDR", PostingDate: &d, Raw: []byte(`{}`)},
		{Key: "J2", CompanyCode: "5110", FiscalYear: "2026", AccountingDocument: "100000123", LedgerGLLineItem: "000002", AccountingDocumentType: "SA", DebitCreditCode: "H", GLAccount: "141000", AmountInTransactionCurrency: -1000, TransactionCurrency: "IDR", PostingDate: &d, Raw: []byte(`{}`)},
		{Key: "K1", CompanyCode: "5110", FiscalYear: "2026", AccountingDocument: "190000001", LedgerGLLineItem: "000001", AccountingDocumentType: "KR", DebitCreditCode: "S", GLAccount: "516044", AmountInTransactionCurrency: 1, PostingDate: &d, Raw: []byte(`{}`)},
	})

	// Tanpa writer hanya dry run yang boleh.
	if _, err := fin.ExportToMaximo(ctx, MaximoExportOptions{}); err == nil {
		t.Fatal("export without writer must fail")
	}
	dry, err := fin.ExportToMaximo(ctx, MaximoExportOptions{DryRun: true})
	if err != nil || len(dry.Messages) != 2 {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	inv := dry.Messages[0]
	if inv.SourceType != "INVOICE" || inv.Rows[0].ItemNum != "68-00045" || inv.Rows[0].BPIGRNNumber != "038/RI/VI/24" || inv.Rows[0].GLDebitAcct != "114001-43071" {
		t.Errorf("lookups not applied in dry run: %+v", inv.Rows[0])
	}
	if dry.Messages[1].SourceType != "JOURNAL" || dry.Messages[1].SourceKey != "5110/2026/100000123" {
		t.Errorf("only journal doc types (SA) must be exported as JOURNAL: %+v", dry.Messages[1])
	}

	w := &fakeMaximoWriter{failKey: "100000123", next: 4768700}
	fin.MaximoWriter = w
	res, err := fin.ExportToMaximo(ctx, MaximoExportOptions{})
	if err != nil || res.Exported != 1 || res.Failed != 1 {
		t.Fatalf("first export: %+v %v", res, err)
	}
	// Jurnal yang gagal dicoba lagi; invoice yang sudah EXPORTED tidak ditulis dua kali.
	w.failKey = ""
	res, err = fin.ExportToMaximo(ctx, MaximoExportOptions{})
	if err != nil || res.Exported != 1 || res.Failed != 0 || res.Messages[0].SourceType != "JOURNAL" {
		t.Fatalf("retry export: %+v %v", res, err)
	}
	res, _ = fin.ExportToMaximo(ctx, MaximoExportOptions{})
	if res.Exported != 0 {
		t.Errorf("nothing left to export, got %d", res.Exported)
	}
	exports, _ := repo.ListMaximoExports(ctx, 10)
	got := map[string]model.MaximoExport{}
	for _, e := range exports {
		got[e.SourceType] = e
	}
	if got["INVOICE"].Status != "EXPORTED" || got["INVOICE"].TransIDs != "4768701,4768702,4768703" || got["JOURNAL"].Status != "EXPORTED" {
		t.Errorf("unexpected export history %+v", exports)
	}
}

func TestIntegration_ReimbursementPolicyMapping(t *testing.T) {
	ctx, repo, fin, _ := setupIntegration(t)
	_, _ = repo.Pool.Exec(ctx, `TRUNCATE employee_vendor_mappings`)
	maps, err := repo.ListReimbursementMappings(ctx)
	if err != nil || len(maps) != 22 {
		t.Fatalf("expected 22 seeded mappings, got %d %v", len(maps), err)
	}

	n, rowErrs, err := fin.ImportEmployeeVendors(ctx, "vendor.csv", []byte("Vendor Code,Comment (CIP/Non CIP)\nEMP-CIP,CIP\nEMP-X,Non CIP\n"))
	if err != nil || len(rowErrs) > 0 || n != 2 {
		t.Fatalf("vendor import failed: %d %v %v", n, rowErrs, err)
	}

	// Baris 3 & 4 policyCSV tidak bisa dipetakan -> seluruh file ditolak.
	res, err := fin.UploadSupplierInvoices(ctx, "p.csv", "finance01", []byte(policyCSV))
	if err != nil || res.Batch.Status != "REJECTED" || len(res.Batch.Errors) != 2 {
		t.Fatalf("expected rejection with 2 errors, got %+v %v", res, err)
	}

	lines := strings.Split(strings.TrimSpace(policyCSV), "\n")
	okCSV := strings.Join(lines[:3], "\n") + "\n"
	res, err = fin.UploadSupplierInvoices(ctx, "p.csv", "finance01", []byte(okCSV))
	if err != nil || res.Batch.Status != "QUEUED" || len(res.Invoices) != 2 {
		t.Fatalf("expected 2 invoices queued, got %+v %v", res, err)
	}
	docs, _ := repo.ListDocumentsByType(ctx, model.DocTypeSupplierInvoice, 10)
	gl := map[string]string{}
	for _, d := range docs {
		var p model.SAPSupplierInvoicePayload
		_ = json.Unmarshal(d.SAPPayload, &p)
		gl[d.DocumentNo] = p.ToSupplierInvoiceItemGLAcct[0].GLAccount
		if !strings.Contains(string(d.RawPayload), `"BudgetCode"`) {
			t.Errorf("raw payload must keep resolved budget code: %s", d.RawPayload)
		}
	}
	// MED-001: vendor CIP + Medical Claim – Outpatient -> 511006; BT-002: kolom Non CIP + Domestic -> 522234.
	if gl["MED-001"] != "511006" || gl["BT-002"] != "522234" {
		t.Errorf("unexpected GL resolution %v", gl)
	}
}
