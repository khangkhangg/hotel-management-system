package reports

import "testing"

func TestAmountsKeepTheirSignAndCurrencyDecimals(t *testing.T) {
	for _, c := range []struct {
		minor int64
		cur   string
		want  string
	}{{20000, "USD", "200.00"}, {-5, "USD", "-0.05"}, {-12345, "USD", "-123.45"}, {5000, "VND", "5000"}, {-5000, "VND", "-5000"}} {
		if got := amount(c.minor, c.cur); got != c.want {
			t.Errorf("%d %s: %q, want %q", c.minor, c.cur, got, c.want)
		}
	}
}
