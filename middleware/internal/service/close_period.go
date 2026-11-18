package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// maxClosedMonthsAhead membatasi pencarian bulan terbuka (mis. seluruh tahun dicentang tutup).
const maxClosedMonthsAhead = 36

// ResolvePostingDate menerapkan FSD Close Period Service:
//   - bulan actualdate tutup  -> tanggal 1 bulan terbuka berikutnya (melewati bulan lain yang juga tutup)
//   - bulan actualdate terbuka -> actualdate apa adanya
func ResolvePostingDate(actual time.Time, isClosed func(year, month int) bool) time.Time {
	if isClosed == nil || !isClosed(actual.Year(), int(actual.Month())) {
		return actual
	}
	first := time.Date(actual.Year(), actual.Month(), 1, 0, 0, 0, 0, actual.Location())
	for i := 1; i <= maxClosedMonthsAhead; i++ {
		next := first.AddDate(0, i, 0)
		if !isClosed(next.Year(), int(next.Month())) {
			return next
		}
	}
	return first.AddDate(0, maxClosedMonthsAhead+1, 0)
}

// parseActualDate membaca ACTUALDATE Maximo/AX; kosong atau tidak valid = hari ini.
func parseActualDate(dateStr string) time.Time {
	trimmed := strings.TrimSpace(dateStr)
	for _, f := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02", time.RFC3339, "2006/01/02"} {
		if t, err := time.Parse(f, trimmed); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		}
	}
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func odataV2Date(t time.Time) string {
	return fmt.Sprintf("/Date(%d)/", t.UnixMilli())
}

// grnDates mengembalikan DocumentDate (= actualdate) dan PostingDate (aturan Close Period) format OData v2.
// Bila tabel close_periods tidak bisa dibaca, PostingDate = actualdate (SAP tetap menolak periode tertutup).
func (s *SyncService) grnDates(ctx context.Context, actualDate string) (string, string) {
	actual := parseActualDate(actualDate)
	var closed map[[2]int]bool
	if s.PGRepo != nil && s.PGRepo.Pool != nil {
		var err error
		if closed, err = s.PGRepo.ClosedPeriods(ctx); err != nil {
			log.Printf("[WARN] Close Period tidak bisa dibaca, PostingDate = actualdate: %v", err)
		}
	}
	posting := ResolvePostingDate(actual, func(y, m int) bool { return closed[[2]int{y, m}] })
	return odataV2Date(actual), odataV2Date(posting)
}
