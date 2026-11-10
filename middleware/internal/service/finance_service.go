package service

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang-bpi-testing/internal/client"
	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ValidationError menandai kesalahan input (dipetakan ke HTTP 400 / 422 oleh controller).
type ValidationError struct {
	Messages []string
}

func (e *ValidationError) Error() string {
	return strings.Join(e.Messages, "; ")
}

func newValidation(msgs ...string) error {
	return &ValidationError{Messages: msgs}
}

// FinanceService menangani RICEF 1-9 (finance integration).
type FinanceService struct {
	PGRepo    *repository.PostgresRepository
	SAPClient *client.SAPClient
	// MaximoWriter menulis ke dbo.mxinvoice (nil bila SQL Server Maximo tidak dikonfigurasi).
	MaximoWriter MaximoInvoiceWriter
	// InventoryReader membaca staging Maximo [axinvissue]/[axinvtrans] untuk RICEF 2.
	InventoryReader InventoryStagingReader
}

func NewFinanceService(pgRepo *repository.PostgresRepository, sapClient *client.SAPClient) *FinanceService {
	return &FinanceService{PGRepo: pgRepo, SAPClient: sapClient}
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// DefaultCompanyCode adalah company code default (FIN_COMPANY_CODE, default 5110).
func DefaultCompanyCode() string {
	return envOr("FIN_COMPANY_CODE", "5110")
}

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// truncateRunes memotong string tanpa merusak karakter UTF-8.
func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func shortID() string {
	return strings.ToUpper(strings.ReplaceAll(uuid.New().String(), "-", "")[:10])
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ===================== RICEF 1: Budget =====================

// BudgetCombination membentuk kunci pagu [MainAccount]-[BudgetCode], contoh 513043-28011
// (dokumen BPI_MAPPING_API_MAXIMO_to_SAP FinanceModule, sheet Budget).
func BudgetCombination(mainAccount, budgetCode string) string {
	return fmt.Sprintf("%s-%s", strings.TrimSpace(mainAccount), strings.TrimSpace(budgetCode))
}

// SupplementBudget mencatat penambahan pagu dan mengantrekan pengiriman ke SAP.
func (f *FinanceService) SupplementBudget(ctx context.Context, req model.BudgetSupplementRequest) (*model.BudgetAdjustment, error) {
	var errs []string
	if strings.TrimSpace(req.MainAccount) == "" {
		errs = append(errs, "main_account wajib diisi")
	}
	if strings.TrimSpace(req.BudgetCode) == "" {
		errs = append(errs, "budget_code wajib diisi")
	}
	if req.Amount <= 0 {
		errs = append(errs, "amount harus lebih dari 0")
	}
	if len(errs) > 0 {
		return nil, newValidation(errs...)
	}
	if req.Year == 0 {
		req.Year = time.Now().Year()
	}
	adj := &model.BudgetAdjustment{
		DocumentNo:     "BDG-SUP-" + shortID(),
		AdjustmentType: "SUPPLEMENT",
		BudgetYear:     req.Year,
		ToCombination:  BudgetCombination(req.MainAccount, req.BudgetCode),
		Amount:         round2(req.Amount),
		Reason:         req.Reason,
		RequestedBy:    req.RequestedBy,
	}
	payload := model.SAPBudgetPayload{
		AdjustmentType:    "SUPPLEMENT",
		FiscalYear:        fmt.Sprintf("%d", req.Year),
		BudgetCombination: adj.ToCombination,
		MainAccount:       strings.TrimSpace(req.MainAccount),
		BudgetCode:        strings.TrimSpace(req.BudgetCode),
		CostCenter:        strings.TrimSpace(req.CostCenter),
		Amount:            adj.Amount,
		Reason:            req.Reason,
		ExternalReference: adj.DocumentNo,
	}
	return f.saveBudgetAdjustment(ctx, adj, req, payload)
}

// TransferBudget mencatat pemindahan pagu; pagu asal langsung ditahan sampai SAP mengonfirmasi.
func (f *FinanceService) TransferBudget(ctx context.Context, req model.BudgetTransferRequest) (*model.BudgetAdjustment, error) {
	var errs []string
	if strings.TrimSpace(req.FromMainAccount) == "" || strings.TrimSpace(req.FromBudgetCode) == "" {
		errs = append(errs, "from_main_account dan from_budget_code wajib diisi")
	}
	if strings.TrimSpace(req.ToMainAccount) == "" || strings.TrimSpace(req.ToBudgetCode) == "" {
		errs = append(errs, "to_main_account dan to_budget_code wajib diisi")
	}
	if req.Amount <= 0 {
		errs = append(errs, "amount harus lebih dari 0")
	}
	from := BudgetCombination(req.FromMainAccount, req.FromBudgetCode)
	to := BudgetCombination(req.ToMainAccount, req.ToBudgetCode)
	if from == to {
		errs = append(errs, "kombinasi asal dan tujuan tidak boleh sama")
	}
	if len(errs) > 0 {
		return nil, newValidation(errs...)
	}
	if req.Year == 0 {
		req.Year = time.Now().Year()
	}
	adj := &model.BudgetAdjustment{
		DocumentNo:      "BDG-TRF-" + shortID(),
		AdjustmentType:  "TRANSFER",
		BudgetYear:      req.Year,
		FromCombination: from,
		ToCombination:   to,
		Amount:          round2(req.Amount),
		Reason:          req.Reason,
		RequestedBy:     req.RequestedBy,
	}
	payload := model.SAPBudgetPayload{
		AdjustmentType:    "TRANSFER",
		FiscalYear:        fmt.Sprintf("%d", req.Year),
		BudgetCombination: to,
		MainAccount:       strings.TrimSpace(req.ToMainAccount),
		BudgetCode:        strings.TrimSpace(req.ToBudgetCode),
		SenderMainAccount: strings.TrimSpace(req.FromMainAccount),
		SenderBudgetCode:  strings.TrimSpace(req.FromBudgetCode),
		Amount:            adj.Amount,
		Reason:            req.Reason,
		ExternalReference: adj.DocumentNo,
	}
	return f.saveBudgetAdjustment(ctx, adj, req, payload)
}

func (f *FinanceService) saveBudgetAdjustment(ctx context.Context, adj *model.BudgetAdjustment, raw interface{}, payload model.SAPBudgetPayload) (*model.BudgetAdjustment, error) {
	if err := f.PGRepo.CreateBudgetAdjustment(ctx, adj); err != nil {
		if strings.Contains(err.Error(), "pagu asal") {
			return nil, newValidation(err.Error())
		}
		return nil, err
	}
	rawBytes, _ := json.Marshal(raw)
	sapBytes, _ := json.Marshal(payload)
	if err := f.PGRepo.QueueDocument(ctx, model.Document{
		DocType: model.DocTypeBudget, DocumentNo: adj.DocumentNo, RawPayload: rawBytes, SAPPayload: sapBytes,
	}); err != nil {
		_ = f.PGRepo.FailBudgetAdjustment(ctx, adj.DocumentNo, "gagal mengantre dokumen: "+err.Error())
		return nil, err
	}
	_ = f.PGRepo.InsertIntegrationLog(ctx, "GRP-BUDGET-"+adj.DocumentNo, "MIDDLEWARE", "MIDDLEWARE", model.DocTypeBudget, adj.DocumentNo, "RECEIVED", "",
		fmt.Sprintf("Budget %s %.2f diantre (%s -> %s)", adj.AdjustmentType, adj.Amount, adj.FromCombination, adj.ToCombination))
	return adj, nil
}

// ===================== RICEF 2: Journal Entry GI =====================

// ValidateJournalEntry memeriksa kelengkapan dan keseimbangan jurnal, serta mengisi default.
func ValidateJournalEntry(req *model.JournalEntryRequest) error {
	var errs []string
	req.Reference = strings.TrimSpace(req.Reference)
	if req.Reference == "" {
		errs = append(errs, "reference wajib diisi")
	} else if len(req.Reference) > 40 {
		errs = append(errs, "reference maksimal 40 karakter")
	} else if strings.Contains(req.Reference, "|") {
		errs = append(errs, "reference tidak boleh mengandung karakter '|'")
	}
	if req.CompanyCode == "" {
		req.CompanyCode = DefaultCompanyCode()
	}
	if req.DocumentType == "" {
		req.DocumentType = "SA"
	}
	if req.Currency == "" {
		req.Currency = "IDR"
	}
	req.Reference1 = strings.TrimSpace(req.Reference1)
	if req.Reference1 == "" {
		req.Reference1 = truncateRunes(req.Reference, 20)
	} else if len(req.Reference1) > 20 {
		errs = append(errs, "reference1 maksimal 20 karakter")
	}
	if len(strings.TrimSpace(req.Reference2)) > 10 {
		errs = append(errs, "reference2 maksimal 10 karakter")
	}
	if !validDate(req.DocumentDate) {
		errs = append(errs, "document_date wajib format YYYY-MM-DD")
	}
	if !validDate(req.PostingDate) {
		errs = append(errs, "posting_date wajib format YYYY-MM-DD")
	}
	if len(req.Items) < 2 {
		errs = append(errs, "jurnal minimal 2 baris (debit dan kredit)")
	}
	var debit, credit float64
	for i := range req.Items {
		it := &req.Items[i]
		n := i + 1
		it.DebitCredit = strings.ToUpper(strings.TrimSpace(it.DebitCredit))
		if strings.TrimSpace(it.GLAccount) == "" {
			errs = append(errs, fmt.Sprintf("baris %d: gl_account wajib diisi", n))
		}
		if it.Amount <= 0 {
			errs = append(errs, fmt.Sprintf("baris %d: amount harus lebih dari 0", n))
		}
		switch it.DebitCredit {
		case "S":
			debit += it.Amount
		case "H":
			credit += it.Amount
		default:
			errs = append(errs, fmt.Sprintf("baris %d: debit_credit harus S (debit) atau H (kredit)", n))
		}
	}
	if round2(debit) != round2(credit) {
		errs = append(errs, fmt.Sprintf("jurnal tidak seimbang: debit %.2f, kredit %.2f", debit, credit))
	}
	if len(errs) > 0 {
		return newValidation(errs...)
	}
	return nil
}

type soapAmount struct {
	Currency string `xml:"currencyCode,attr"`
	Value    string `xml:",chardata"`
}

type soapAccountAssignment struct {
	CostCenter string `xml:"CostCenter"`
}

type soapJEItem struct {
	ReferenceDocumentItem       int                    `xml:"ReferenceDocumentItem"`
	GLAccount                   string                 `xml:"GLAccount"`
	AmountInTransactionCurrency soapAmount             `xml:"AmountInTransactionCurrency"`
	DebitCreditCode             string                 `xml:"DebitCreditCode"`
	DocumentItemText            string                 `xml:"DocumentItemText,omitempty"`
	AccountAssignment           *soapAccountAssignment `xml:"AccountAssignment,omitempty"`
}

type soapMessageHeader struct {
	ID               string `xml:"ID"`
	CreationDateTime string `xml:"CreationDateTime"`
}

// soapJournalEntry mengikuti urutan elemen template "BPI - Payload API Journal Entry BPI.txt".
type soapJournalEntry struct {
	OriginalReferenceDocumentType          string       `xml:"OriginalReferenceDocumentType"`
	OriginalReferenceDocument              string       `xml:"OriginalReferenceDocument"`
	OriginalReferenceDocumentLogicalSystem string       `xml:"OriginalReferenceDocumentLogicalSystem"`
	BusinessTransactionType                string       `xml:"BusinessTransactionType"`
	AccountingDocumentType                 string       `xml:"AccountingDocumentType"`
	DocumentReferenceID                    string       `xml:"DocumentReferenceID"`
	DocumentHeaderText                     string       `xml:"DocumentHeaderText,omitempty"`
	CreatedByUser                          string       `xml:"CreatedByUser,omitempty"`
	CompanyCode                            string       `xml:"CompanyCode"`
	DocumentDate                           string       `xml:"DocumentDate"`
	PostingDate                            string       `xml:"PostingDate"`
	Reference1InDocumentHeader             string       `xml:"Reference1InDocumentHeader,omitempty"`
	Reference2InDocumentHeader             string       `xml:"Reference2InDocumentHeader,omitempty"`
	Item                                   []soapJEItem `xml:"Item"`
}

type soapJECreateRequest struct {
	MessageHeader soapMessageHeader `xml:"MessageHeader"`
	JournalEntry  soapJournalEntry  `xml:"JournalEntry"`
}

type soapJEBulk struct {
	MessageHeader             soapMessageHeader     `xml:"MessageHeader"`
	JournalEntryCreateRequest []soapJECreateRequest `xml:"JournalEntryCreateRequest"`
}

type soapEnvelope struct {
	XMLName xml.Name `xml:"soapenv:Envelope"`
	SoapNS  string   `xml:"xmlns:soapenv,attr"`
	SfinNS  string   `xml:"xmlns:sfin,attr"`
	Header  struct{} `xml:"soapenv:Header"`
	Body    struct {
		Request soapJEBulk `xml:"sfin:JournalEntryBulkCreateRequest"`
	} `xml:"soapenv:Body"`
}

// BuildJournalEntrySOAP membangun envelope JournalEntryBulkCreateRequest (SAP_COM_0002).
// Struktur harus dicocokkan dengan template "BPI - Payload API Journal Entry BPI.txt" sebelum UAT.
func BuildJournalEntrySOAP(req model.JournalEntryRequest, now time.Time) (string, error) {
	created := now.UTC().Format("2006-01-02T15:04:05Z")
	msgID := func() string { return "MW" + strings.ReplaceAll(uuid.New().String(), "-", "") }

	items := make([]soapJEItem, 0, len(req.Items))
	for i, it := range req.Items {
		amount := round2(it.Amount)
		if it.DebitCredit == "H" {
			amount = -amount
		}
		item := soapJEItem{
			ReferenceDocumentItem:       i + 1,
			GLAccount:                   strings.TrimSpace(it.GLAccount),
			AmountInTransactionCurrency: soapAmount{Currency: req.Currency, Value: fmt.Sprintf("%.2f", amount)},
			DebitCreditCode:             it.DebitCredit,
			DocumentItemText:            truncateRunes(it.ItemText, 50),
		}
		if cc := strings.TrimSpace(it.CostCenter); cc != "" {
			item.AccountAssignment = &soapAccountAssignment{CostCenter: cc}
		}
		items = append(items, item)
	}

	var env soapEnvelope
	env.SoapNS = "http://schemas.xmlsoap.org/soap/envelope/"
	env.SfinNS = "http://sap.com/xi/SAPSCORE/SFIN"
	env.Body.Request = soapJEBulk{
		MessageHeader: soapMessageHeader{ID: msgID(), CreationDateTime: created},
		JournalEntryCreateRequest: []soapJECreateRequest{{
			MessageHeader: soapMessageHeader{ID: msgID(), CreationDateTime: created},
			JournalEntry: soapJournalEntry{
				// Template BPI mengosongkan OriginalReferenceDocument & LogicalSystem (diisi SAP).
				OriginalReferenceDocumentType:          "BKPFF",
				OriginalReferenceDocument:              "",
				OriginalReferenceDocumentLogicalSystem: os.Getenv("FIN_LOGICAL_SYSTEM"),
				BusinessTransactionType:                "RFBU",
				AccountingDocumentType:                 req.DocumentType,
				DocumentReferenceID:                    truncateRunes(req.Reference, 16),
				DocumentHeaderText:                     truncateRunes(req.HeaderText, 25),
				CreatedByUser:                          os.Getenv("FIN_JE_CREATED_BY_USER"),
				CompanyCode:                            req.CompanyCode,
				DocumentDate:                           req.DocumentDate,
				PostingDate:                            req.PostingDate,
				Reference1InDocumentHeader:             truncateRunes(req.Reference1, 20),
				Reference2InDocumentHeader:             truncateRunes(req.Reference2, 10),
				Item:                                   items,
			},
		}},
	}
	out, err := xml.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", err
	}
	return xml.Header + string(out), nil
}

// journalBudgetLines menjumlahkan baris debit per kombinasi [GL account]-[budget code].
func journalBudgetLines(req model.JournalEntryRequest) map[string]float64 {
	out := map[string]float64{}
	for _, it := range req.Items {
		if it.DebitCredit != "S" || strings.TrimSpace(it.GLAccount) == "" || strings.TrimSpace(it.BudgetCode) == "" {
			continue
		}
		out[BudgetCombination(it.GLAccount, it.BudgetCode)] += it.Amount
	}
	return out
}

// CreateJournalEntry memvalidasi jurnal, menahan pagu, lalu mengantrekan pengiriman SOAP.
func (f *FinanceService) CreateJournalEntry(ctx context.Context, req model.JournalEntryRequest) (string, error) {
	if err := ValidateJournalEntry(&req); err != nil {
		return "", err
	}
	if status, found, err := f.PGRepo.GetDocumentStatus(ctx, model.DocTypeJournalGI, req.Reference); err != nil {
		return "", err
	} else if found && status == "success" {
		return "", repository.ErrAlreadyPosted
	} else if found && (status == "processing") {
		return "", newValidation("jurnal dengan reference yang sama sedang dikirim ke SAP")
	}

	year := time.Now().Year()
	if t, err := time.Parse("2006-01-02", req.PostingDate); err == nil {
		year = t.Year()
	}
	for combo, amount := range journalBudgetLines(req) {
		ok, err := f.PGRepo.CheckAndLockBudget(ctx, combo, year, model.DocTypeJournalGI, req.Reference+"|"+combo, round2(amount))
		if !ok || err != nil {
			_ = f.PGRepo.ReleaseBudgetLocks(ctx, model.DocTypeJournalGI, req.Reference)
			if err == nil {
				err = fmt.Errorf("budget lock ditolak")
			}
			return "", newValidation(fmt.Sprintf("pagu %s: %v", combo, err))
		}
	}

	envelope, err := BuildJournalEntrySOAP(req, time.Now())
	if err != nil {
		_ = f.PGRepo.ReleaseBudgetLocks(ctx, model.DocTypeJournalGI, req.Reference)
		return "", err
	}
	rawBytes, _ := json.Marshal(req)
	sapBytes, _ := json.Marshal(envelope)
	if err := f.PGRepo.QueueDocument(ctx, model.Document{
		DocType: model.DocTypeJournalGI, DocumentNo: req.Reference, RawPayload: rawBytes, SAPPayload: sapBytes,
	}); err != nil {
		_ = f.PGRepo.ReleaseBudgetLocks(ctx, model.DocTypeJournalGI, req.Reference)
		return "", err
	}
	_ = f.PGRepo.InsertIntegrationLog(ctx, "GRP-JOURNAL_GI-"+req.Reference, "SOURCE", "MIDDLEWARE", model.DocTypeJournalGI, req.Reference, "RECEIVED", "",
		fmt.Sprintf("Journal Entry GI %d baris diantre untuk dikirim via SOAP", len(req.Items)))
	return req.Reference, nil
}

// DetermineJournalEntryFromGIOrAdj membangun JournalEntryRequest untuk Goods Issue atau Stock Adjustment
// sesuai Enhancement Specification DJ-MM-ENH-ZME003:
// 1. Get Valuation Class dari input atau query OData SAP A_Product/to_Valuation
// 2. Lookup Akun GL & Cost Center dari tabel matriks valuation_class_gl_mappings
// 3. Tentukan Debit/Credit dan Cost Center:
//    - Goods Issue: DocType WL, Inventory H (tanpa CC), Goods Issue S (dengan CC)
//    - Stock Adjustment Positif: DocType WA, Inventory S (tanpa CC), Stock Adj H (dengan CC)
//    - Stock Adjustment Negatif: DocType WA, Inventory H (tanpa CC), Stock Adj S (dengan CC)
func (f *FinanceService) DetermineJournalEntryFromGIOrAdj(ctx context.Context, input model.GIOrAdjustmentInput) (model.JournalEntryRequest, error) {
	ref := strings.TrimSpace(input.Reference)
	if ref == "" {
		return model.JournalEntryRequest{}, fmt.Errorf("reference transaksi wajib diisi")
	}

	itemNum := strings.TrimSpace(input.ItemNum)
	if itemNum == "" {
		return model.JournalEntryRequest{}, fmt.Errorf("item_num wajib diisi")
	}

	// 1. Dapatkan Valuation Class
	valClass := strings.ToUpper(strings.TrimSpace(input.ValuationClass))
	unitCost := input.UnitCost
	needPrice := input.TotalAmount == 0 && unitCost <= 0
	if valClass == "" || needPrice {
		if f.SAPClient == nil || f.PGRepo == nil {
			return model.JournalEntryRequest{}, fmt.Errorf("valuation class/harga item %s tidak tersedia dan SAP client tidak dikonfigurasi", itemNum)
		}
		ep, err := f.PGRepo.GetSAPEndpointByCode(ctx, envOr("FIN_PRODUCT_ENDPOINT_CODE", "PRODUCT"))
		if err != nil || ep == nil {
			return model.JournalEntryRequest{}, fmt.Errorf("endpoint SAP PRODUCT tidak ditemukan untuk lookup valuation item %s", itemNum)
		}
		vc, price, err := f.SAPClient.GetProductValuation(ep, itemNum)
		if err != nil {
			return model.JournalEntryRequest{}, fmt.Errorf("lookup valuation item %s gagal: %w", itemNum, err)
		}
		if valClass == "" {
			valClass = strings.ToUpper(vc)
		}
		if needPrice {
			unitCost = price
		}
	}

	// 2. Lookup matriks pemetaan Valuation Class -> GL & Cost Center
	mapping, err := f.PGRepo.GetValuationClassGLMapping(ctx, valClass)
	if err != nil {
		return model.JournalEntryRequest{}, fmt.Errorf("gagal menemukan mapping GL untuk valuation class %s: %w", valClass, err)
	}

	// 3. Tentukan nominal transaksi
	amount := math.Abs(input.TotalAmount)
	if amount == 0 && unitCost > 0 {
		amount = math.Abs(input.Quantity) * unitCost
	}
	amount = round2(amount)
	if amount <= 0 {
		return model.JournalEntryRequest{}, fmt.Errorf("nominal transaksi %s nol: quantity atau harga valuasi item %s kosong", ref, itemNum)
	}

	// Tanggal transaksi
	transDate := strings.TrimSpace(input.TransDate)
	if transDate == "" {
		transDate = time.Now().Format("2006-01-02")
	}
	docDate := input.DocumentDate
	if docDate == "" {
		docDate = transDate
	}
	postingDate := input.PostingDate
	if postingDate == "" {
		postingDate = transDate
	}

	companyCode := strings.TrimSpace(input.CompanyCode)
	if companyCode == "" {
		companyCode = envOr("FIN_COMPANY_CODE", "5110")
	}

	ccy := strings.ToUpper(strings.TrimSpace(input.Currency))
	if ccy == "" {
		ccy = envOr("FIN_GI_CURRENCY", "USD")
	}

	// 4. Determinasi Akun, Debit/Credit, DocType, dan Header Text
	isGI := strings.EqualFold(input.TransactionType, "GOODS_ISSUE") || strings.EqualFold(input.TransactionType, "GI") || strings.EqualFold(input.TransactionType, "WL")

	var docType string
	var headerText string
	var items []model.JournalEntryItem

	if isGI {
		// Goods Issue: AccountingDocumentType = "WL"
		docType = "WL"
		headerText = "Goods Issue"

		// Baris 1: Inventory Posting, Kredit (H), TANPA Cost Center
		items = append(items, model.JournalEntryItem{
			GLAccount:   mapping.InventoryPostingGL,
			Amount:      amount,
			DebitCredit: "H",
			CostCenter:  "", // Larangan: Inventory Posting tidak ditempel Cost Center
			ItemText:    input.ItemText,
		})

		// Baris 2: Goods Issue Expense, Debit (S), DENGAN Cost Center
		items = append(items, model.JournalEntryItem{
			GLAccount:   mapping.GoodsIssueGL,
			Amount:      amount,
			DebitCredit: "S",
			CostCenter:  mapping.CostCenter,
			ItemText:    input.ItemText,
		})
	} else {
		// Stock Adjustment: AccountingDocumentType = "WA"
		docType = "WA"
		headerText = "Stock Adjustment"

		if input.Quantity >= 0 {
			// Positif (Surplus): Inventory Posting Debit (S) tanpa CC, Stock Adj Kredit (H) dengan CC
			items = append(items, model.JournalEntryItem{
				GLAccount:   mapping.InventoryPostingGL,
				Amount:      amount,
				DebitCredit: "S",
				CostCenter:  "", // Tanpa Cost Center
				ItemText:    input.ItemText,
			})
			items = append(items, model.JournalEntryItem{
				GLAccount:   mapping.StockAdjGL,
				Amount:      amount,
				DebitCredit: "H",
				CostCenter:  mapping.CostCenter,
				ItemText:    input.ItemText,
			})
		} else {
			// Negatif (Defisit): Inventory Posting Kredit (H) tanpa CC, Stock Adj Debit (S) dengan CC
			items = append(items, model.JournalEntryItem{
				GLAccount:   mapping.InventoryPostingGL,
				Amount:      amount,
				DebitCredit: "H",
				CostCenter:  "", // Tanpa Cost Center
				ItemText:    input.ItemText,
			})
			items = append(items, model.JournalEntryItem{
				GLAccount:   mapping.StockAdjGL,
				Amount:      amount,
				DebitCredit: "S",
				CostCenter:  mapping.CostCenter,
				ItemText:    input.ItemText,
			})
		}
	}

	req := model.JournalEntryRequest{
		Reference:    ref,
		CompanyCode:  companyCode,
		DocumentType: docType,
		DocumentDate: docDate,
		PostingDate:  postingDate,
		Currency:     ccy,
		HeaderText:   headerText,
		Reference1:   input.Reference1,
		Reference2:   input.Reference2,
		Items:        items,
	}

	return req, nil
}

// ProcessGoodsIssueOrAdjustment menerima transaksi GI / Stock Adj dari Maximo, menjalankan determinasi GL,
// memvalidasi, menahan pagu, dan mengantrekan dokumen SOAP ke SAP.
func (f *FinanceService) ProcessGoodsIssueOrAdjustment(ctx context.Context, input model.GIOrAdjustmentInput) (string, error) {
	req, err := f.DetermineJournalEntryFromGIOrAdj(ctx, input)
	if err != nil {
		return "", err
	}
	return f.CreateJournalEntry(ctx, req)
}

// ===================== RICEF 6: Exchange Rate =====================

// BuildExchangeRatePayload membentuk payload CurrencyExchangeRate untuk satu mata uang KMK.
func BuildExchangeRatePayload(rec model.ExchangeRateRecord) (string, model.SAPExchangeRatePayload, error) {
	ccy := strings.ToUpper(strings.TrimSpace(rec.CurrencyCode))
	if ccy == "" {
		return "", model.SAPExchangeRatePayload{}, fmt.Errorf("currency_code kosong")
	}
	validFrom := strings.TrimSpace(rec.ValidFrom)
	if validFrom == "" {
		validFrom = rec.CreatedAt.Format("2006-01-02")
	}
	unit := rec.Unit
	if unit <= 0 {
		unit = 1
	}
	if rec.Rate <= 0 {
		return "", model.SAPExchangeRatePayload{}, fmt.Errorf("kurs %s tidak valid: %.4f", ccy, rec.Rate)
	}
	docNo := fmt.Sprintf("EXR-%s-%s", validFrom, ccy)
	return docNo, model.SAPExchangeRatePayload{
		ExchangeRateType:            envOr("FIN_EXRATE_TYPE", "M"),
		SourceCurrency:              ccy,
		TargetCurrency:              envOr("FIN_EXRATE_TARGET_CURRENCY", "IDR"),
		ValidityStartDate:           validFrom,
		ExchangeRate:                rec.Rate,
		NumberOfSourceCurrencyUnits: unit,
		NumberOfTargetCurrencyUnits: 1,
	}, nil
}

// ExchangeRateQueueResult meringkas hasil antre kurs.
type ExchangeRateQueueResult struct {
	KMKNumber string   `json:"kmk_number"`
	Queued    []string `json:"queued"`
	Skipped   []string `json:"skipped"`
	Errors    []string `json:"errors,omitempty"`
}

// QueueExchangeRates mengantrekan kurs satu KMK (kosong = terbaru) untuk dikirim ke SAP.
// Kurs yang sudah sukses dikirim dilewati.
func (f *FinanceService) QueueExchangeRates(ctx context.Context, kmk string) (*ExchangeRateQueueResult, error) {
	recs, err := f.PGRepo.GetExchangeRatesForKMK(ctx, kmk)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, newValidation("data kurs untuk KMK tersebut tidak ditemukan")
		}
		return nil, err
	}
	res := &ExchangeRateQueueResult{KMKNumber: recs[0].KMKNumber}
	for _, rec := range recs {
		docNo, payload, err := BuildExchangeRatePayload(rec)
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		rawBytes, _ := json.Marshal(rec)
		sapBytes, _ := json.Marshal(payload)
		err = f.PGRepo.QueueDocument(ctx, model.Document{
			DocType: model.DocTypeExchangeRate, DocumentNo: docNo, RawPayload: rawBytes, SAPPayload: sapBytes,
		})
		switch {
		case errors.Is(err, repository.ErrAlreadyPosted):
			res.Skipped = append(res.Skipped, docNo)
		case err != nil:
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", docNo, err))
		default:
			res.Queued = append(res.Queued, docNo)
		}
	}
	return res, nil
}

// ===================== RICEF 8: Asset Acquisition =====================

// ValidateAssetAcquisition memeriksa input parking aset dan mengisi default.
func ValidateAssetAcquisition(req *model.AssetAcquisitionRequest) error {
	var errs []string
	req.Reference = strings.TrimSpace(req.Reference)
	req.MasterFixedAsset = strings.TrimSpace(req.MasterFixedAsset)
	req.FixedAsset = strings.TrimSpace(req.FixedAsset)
	req.ParkedBy = strings.TrimSpace(req.ParkedBy)
	if req.Reference == "" || len(req.Reference) > 50 {
		errs = append(errs, "reference wajib diisi (maksimal 50 karakter)")
	}
	if req.CompanyCode == "" {
		req.CompanyCode = DefaultCompanyCode()
	}
	if req.FixedAsset == "" {
		req.FixedAsset = "0"
	}
	if req.Currency == "" {
		req.Currency = "IDR"
	}
	if req.MasterFixedAsset == "" {
		errs = append(errs, "master_fixed_asset wajib diisi")
	}
	if req.AssetValueDate == "" {
		req.AssetValueDate = req.PostingDate
	}
	for name, v := range map[string]string{"document_date": req.DocumentDate, "posting_date": req.PostingDate, "asset_value_date": req.AssetValueDate} {
		if !validDate(v) {
			errs = append(errs, name+" wajib format YYYY-MM-DD")
		}
	}
	if req.Amount <= 0 {
		errs = append(errs, "amount harus lebih dari 0")
	}
	if strings.TrimSpace(req.OffsettingAccount) == "" {
		errs = append(errs, "offsetting_account wajib diisi")
	}
	if req.ParkedBy == "" {
		errs = append(errs, "parked_by wajib diisi")
	}
	if len(errs) > 0 {
		return newValidation(errs...)
	}
	return nil
}

// BuildAssetAcquisitionPayload membentuk parameter action SAP__self.Post.
func BuildAssetAcquisitionPayload(a model.AssetAcquisition) model.SAPAssetAcquisitionPayload {
	return model.SAPAssetAcquisitionPayload{
		CompanyCode:                  a.CompanyCode,
		MasterFixedAsset:             a.MasterFixedAsset,
		FixedAsset:                   a.FixedAsset,
		DocumentDate:                 a.DocumentDate,
		PostingDate:                  a.PostingDate,
		AssetValueDate:               a.AssetValueDate,
		AcqnAmtInTransactionCurrency: round2(a.Amount),
		TransactionCurrency:          a.Currency,
		OffsettingAccount:            a.OffsettingAccount,
		DocumentReferenceID:          truncateRunes(a.Reference, 16),
		DocumentItemText:             truncateRunes(a.ItemText, 50),
	}
}

// ParkAssetAcquisition menyimpan jurnal pengakuan aset berstatus PARKED.
func (f *FinanceService) ParkAssetAcquisition(ctx context.Context, req model.AssetAcquisitionRequest) (*model.AssetAcquisition, error) {
	if err := ValidateAssetAcquisition(&req); err != nil {
		return nil, err
	}
	exists, err := f.PGRepo.FixedAssetExists(ctx, req.CompanyCode, req.MasterFixedAsset, req.FixedAsset)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, newValidation(fmt.Sprintf("aset %s/%s/%s belum ada di data lokal; jalankan sinkron Fixed Asset (RICEF 9) terlebih dahulu",
			req.CompanyCode, req.MasterFixedAsset, req.FixedAsset))
	}
	a, err := f.PGRepo.CreateAssetAcquisition(ctx, req)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, newValidation("reference " + req.Reference + " sudah dipakai")
		}
		return nil, err
	}
	return a, nil
}

// ApproveAssetAcquisition menyetujui parking (maker-checker) dan mengantrekan posting ke SAP.
func (f *FinanceService) ApproveAssetAcquisition(ctx context.Context, id uuid.UUID, by string) (*model.AssetAcquisition, error) {
	by = strings.TrimSpace(by)
	if by == "" {
		return nil, newValidation("approver (by) wajib diisi")
	}
	return f.PGRepo.TransitionAssetAcquisition(ctx, id,
		func(a *model.AssetAcquisition) error {
			if a.Status != "PARKED" && a.Status != "FAILED" {
				return newValidation(fmt.Sprintf("status %s tidak dapat di-approve (hanya PARKED atau FAILED)", a.Status))
			}
			if strings.EqualFold(a.ParkedBy, by) {
				return newValidation("approver harus berbeda dari pembuat parking (maker-checker)")
			}
			if a.PrintedAt == nil {
				return newValidation("Print Form harus dicetak sebelum Approve")
			}
			return nil
		},
		func(ctx context.Context, tx pgx.Tx, a *model.AssetAcquisition) error {
			if _, err := tx.Exec(ctx, `
				UPDATE asset_acquisitions SET status = 'APPROVED', approved_by = $2, approved_at = NOW(),
					error_message = NULL, updated_at = NOW()
				WHERE id = $1`, a.ID, by); err != nil {
				return err
			}
			rawBytes, _ := json.Marshal(a)
			sapBytes, _ := json.Marshal(BuildAssetAcquisitionPayload(*a))
			return repository.QueueDocumentTx(ctx, tx, model.Document{
				DocType: model.DocTypeAssetAcq, DocumentNo: a.Reference, RawPayload: rawBytes, SAPPayload: sapBytes,
			})
		})
}

// PrintAssetAcquisition mencatat pencetakan Print Form; setelah itu tombol Approve/Reject tersedia.
func (f *FinanceService) PrintAssetAcquisition(ctx context.Context, id uuid.UUID, by string) (*model.AssetAcquisition, error) {
	if by = strings.TrimSpace(by); by == "" {
		return nil, newValidation("by wajib diisi")
	}
	return f.PGRepo.MarkAssetAcquisitionPrinted(ctx, id, by)
}

// RejectAssetAcquisition menolak parking berstatus PARKED.
func (f *FinanceService) RejectAssetAcquisition(ctx context.Context, id uuid.UUID, by, reason string) (*model.AssetAcquisition, error) {
	by, reason = strings.TrimSpace(by), strings.TrimSpace(reason)
	if by == "" || reason == "" {
		return nil, newValidation("by dan reason wajib diisi")
	}
	return f.PGRepo.TransitionAssetAcquisition(ctx, id,
		func(a *model.AssetAcquisition) error {
			if a.Status != "PARKED" {
				return newValidation(fmt.Sprintf("status %s tidak dapat ditolak (hanya PARKED)", a.Status))
			}
			if a.PrintedAt == nil {
				return newValidation("Print Form harus dicetak sebelum Reject")
			}
			return nil
		},
		func(ctx context.Context, tx pgx.Tx, a *model.AssetAcquisition) error {
			_, err := tx.Exec(ctx, `
				UPDATE asset_acquisitions SET status = 'REJECTED', rejected_by = $2, reject_reason = $3, updated_at = NOW()
				WHERE id = $1`, a.ID, by, reason)
			return err
		})
}
