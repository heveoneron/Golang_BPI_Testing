package service

import (
	"testing"
	"time"
)

func TestResolvePostingDate_ClosePeriod(t *testing.T) {
	closed := map[[2]int]bool{{2026, 4}: true, {2026, 5}: true, {2026, 12}: true}
	isClosed := func(y, m int) bool { return closed[[2]int{y, m}] }
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		actual, want time.Time
	}{
		{d(2026, 4, 20), d(2026, 6, 1)},  // April & Mei tutup -> 1 Juni
		{d(2026, 6, 15), d(2026, 6, 15)}, // Juni terbuka -> actualdate
		{d(2026, 12, 31), d(2027, 1, 1)}, // lintas tahun
		{d(2026, 3, 2), d(2026, 3, 2)},
	}
	for _, c := range cases {
		if got := ResolvePostingDate(c.actual, isClosed); !got.Equal(c.want) {
			t.Errorf("%s -> %s, want %s", c.actual.Format("2006-01-02"), got.Format("2006-01-02"), c.want.Format("2006-01-02"))
		}
	}
	if got := ResolvePostingDate(d(2026, 4, 20), nil); !got.Equal(d(2026, 4, 20)) {
		t.Errorf("tanpa master close period PostingDate harus actualdate, got %s", got)
	}
}

func TestParseActualDate(t *testing.T) {
	if got := parseActualDate("2026-04-20 13:45:00").Format("2006-01-02"); got != "2026-04-20" {
		t.Fatalf("got %s", got)
	}
	if got := odataV2Date(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); got != "/Date(1780272000000)/" {
		t.Fatalf("odata date %s", got)
	}
}
