// Package pricing suggests room prices from how full the hotel is and what competitors charge. The rules are plain and
// the reasons are shown. Nothing changes a price by itself: a person approves each change, and the limits the hotel
// set always hold.
package pricing

import (
	"fmt"
	"slices"
)

// Input is what the suggestion for one rate plan and night is made from.
type Input struct {
	Current    int64   // the price now, in minor units
	Occupancy  float64 // share of the room type that is sold or blocked, 0 to 1
	DaysAhead  int     // days from today to the night
	Competitor *int64  // the median price competitors charge, if staff entered any
	Floor      *int64
	Ceiling    *int64
	Currency   string
}

// Suggestion is a proposed price and why.
type Suggestion struct {
	Price   int64
	Reasons []string
}

var noCents = map[string]bool{"VND": true, "JPY": true, "KRW": true, "CLP": true, "IDR": true}

// step is the unit a suggested price is rounded to, so it looks like a price a person would set.
func step(currency string) int64 {
	switch currency {
	case "VND", "IDR":
		return 1000
	}
	return 100
}

func money(amount int64, currency string) string {
	if noCents[currency] {
		return fmt.Sprintf("%d %s", amount, currency)
	}
	return fmt.Sprintf("%d.%02d %s", amount/100, amount%100, currency)
}

// Suggest proposes a price, or says there is nothing worth changing. A change under 2 percent is noise.
func Suggest(in Input) (Suggestion, bool) {
	if in.Current <= 0 {
		return Suggestion{}, false
	}
	var reasons []string
	p := in.Current
	pct := 0
	switch {
	case in.Occupancy >= 0.85:
		pct = 10
	case in.Occupancy >= 0.70:
		pct = 5
	case in.Occupancy <= 0.15 && in.DaysAhead <= 21:
		pct = -10
	case in.Occupancy <= 0.30 && in.DaysAhead <= 21:
		pct = -5
	}
	if pct != 0 {
		p = p * int64(100+pct) / 100
		dir := "up"
		if pct < 0 {
			dir = "down"
		}
		reasons = append(reasons, fmt.Sprintf("Occupancy is %d percent, so the price goes %s %d percent.", int(in.Occupancy*100+0.5), dir, max(pct, -pct)))
	}
	if in.Competitor != nil && *in.Competitor > 0 {
		p += (*in.Competitor - p) * 3 / 10
		reasons = append(reasons, fmt.Sprintf("The competitors you entered charge about %s, so the price moves 30 percent of the way there.", money(*in.Competitor, in.Currency)))
	}
	s := step(in.Currency)
	p = (p + s/2) / s * s
	if in.Ceiling != nil && p > *in.Ceiling {
		p = *in.Ceiling
		reasons = append(reasons, fmt.Sprintf("The ceiling you set is %s, so the price stops there.", money(*in.Ceiling, in.Currency)))
	}
	if in.Floor != nil && p < *in.Floor {
		p = *in.Floor
		reasons = append(reasons, fmt.Sprintf("The floor you set is %s, so the price stops there.", money(*in.Floor, in.Currency)))
	}
	diff := p - in.Current
	if diff < 0 {
		diff = -diff
	}
	if diff*100 < in.Current*2 {
		return Suggestion{}, false
	}
	return Suggestion{Price: p, Reasons: reasons}, true
}

// Median is the middle of the prices, so one odd price does not move it. With two middle prices it is their average.
func Median(prices []int64) (int64, bool) {
	if len(prices) == 0 {
		return 0, false
	}
	s := slices.Clone(prices)
	slices.Sort(s)
	if len(s)%2 == 1 {
		return s[len(s)/2], true
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2, true
}
