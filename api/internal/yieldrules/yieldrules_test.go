package yieldrules_test

import (
	"testing"

	"github.com/google/uuid"

	"hotelpms/api/internal/yieldrules"
)

var deluxe, suite = uuid.New(), uuid.New()

func night(date string, rt uuid.UUID, total, taken int, price int64) yieldrules.Night {
	return yieldrules.Night{Date: date, RoomTypeID: rt, Total: total, Taken: taken, Available: total - taken, Price: price}
}

func rule(kind string, value, percent int) yieldrules.Rule {
	return yieldrules.Rule{Kind: kind, Value: value, Percent: percent, Active: true}
}

func TestAnAvailabilityCapNeverShowsMoreRoomsThanTheCap(t *testing.T) {
	got := yieldrules.Apply(night("2026-12-10", deluxe, 10, 2, 10000), []yieldrules.Rule{rule("availability_cap", 3, 0)})
	if got.Available != 3 {
		t.Fatalf("%+v", got)
	}
	if got := yieldrules.Apply(night("2026-12-10", deluxe, 10, 8, 10000), []yieldrules.Rule{rule("availability_cap", 3, 0)}); got.Available != 2 {
		t.Fatalf("a cap raised what was left: %+v", got)
	}
}

func TestASellLimitStopsSellingOnceThatManyRoomsAreTaken(t *testing.T) {
	r := []yieldrules.Rule{rule("sell_limit", 6, 0)}
	for taken, want := range map[int]int{0: 6, 4: 2, 6: 0, 9: 0} {
		if got := yieldrules.Apply(night("2026-12-10", deluxe, 10, taken, 10000), r); got.Available != want {
			t.Errorf("taken %d: available %d, want %d", taken, got.Available, want)
		}
	}
}

func TestAutoCloseShutsTheRoomTypeWhenFewerThanTheSetNumberAreLeft(t *testing.T) {
	r := []yieldrules.Rule{rule("auto_close", 3, 0)}
	for taken, want := range map[int]int{5: 5, 7: 3, 8: 0, 10: 0} {
		if got := yieldrules.Apply(night("2026-12-10", deluxe, 10, taken, 10000), r); got.Available != want {
			t.Errorf("taken %d: available %d, want %d", taken, got.Available, want)
		}
	}
}

func TestARateAdjustmentMovesThePriceWhenFewRoomsAreLeft(t *testing.T) {
	up := []yieldrules.Rule{rule("rate_adjust", 2, 15)}
	if got := yieldrules.Apply(night("2026-12-10", deluxe, 10, 8, 10000), up); got.Price != 11500 || got.Available != 2 {
		t.Fatalf("%+v", got)
	}
	if got := yieldrules.Apply(night("2026-12-10", deluxe, 10, 5, 10000), up); got.Price != 10000 {
		t.Fatalf("the price moved with plenty of rooms left: %+v", got)
	}
	down := []yieldrules.Rule{rule("rate_adjust", 100, -20)}
	if got := yieldrules.Apply(night("2026-12-10", deluxe, 10, 0, 12345), down); got.Price != 9876 {
		t.Fatalf("%+v", got) // 12345 * 0.8 = 9876
	}
}

func TestARuleAppliesOnlyToItsRoomTypeAndDates(t *testing.T) {
	r := rule("availability_cap", 1, 0)
	r.RoomTypeID = &deluxe
	r.From, r.To = "2026-12-20", "2026-12-25"
	if got := yieldrules.Apply(night("2026-12-22", suite, 10, 0, 100), []yieldrules.Rule{r}); got.Available != 10 {
		t.Fatalf("another room type was capped: %+v", got)
	}
	if got := yieldrules.Apply(night("2026-12-19", deluxe, 10, 0, 100), []yieldrules.Rule{r}); got.Available != 10 {
		t.Fatalf("a date before the window was capped: %+v", got)
	}
	if got := yieldrules.Apply(night("2026-12-26", deluxe, 10, 0, 100), []yieldrules.Rule{r}); got.Available != 10 {
		t.Fatalf("a date after the window was capped: %+v", got)
	}
	for _, d := range []string{"2026-12-20", "2026-12-22", "2026-12-25"} {
		if got := yieldrules.Apply(night(d, deluxe, 10, 0, 100), []yieldrules.Rule{r}); got.Available != 1 {
			t.Errorf("%s was not capped: %+v", d, got)
		}
	}
}

func TestSwitchedOffRulesDoNothingAndRulesCombine(t *testing.T) {
	off := rule("auto_close", 99, 0)
	off.Active = false
	if got := yieldrules.Apply(night("2026-12-10", deluxe, 10, 0, 10000), []yieldrules.Rule{off}); got.Available != 10 {
		t.Fatalf("%+v", got)
	}
	both := []yieldrules.Rule{rule("rate_adjust", 3, 10), rule("availability_cap", 1, 0)}
	if got := yieldrules.Apply(night("2026-12-10", deluxe, 10, 8, 10000), both); got.Available != 1 || got.Price != 11000 {
		t.Fatalf("%+v", got)
	}
}

func TestARuleThatIsNotSensibleIsRefused(t *testing.T) {
	for _, bad := range []yieldrules.Rule{
		{Kind: "free_rooms", Value: 1},
		{Kind: "auto_close", Value: 0},
		{Kind: "availability_cap", Value: -1},
		{Kind: "rate_adjust", Value: 2, Percent: 0},
		{Kind: "rate_adjust", Value: 2, Percent: 150},
		{Kind: "rate_adjust", Value: 2, Percent: -60},
		{Kind: "sell_limit", Value: 3, From: "2026-12-25", To: "2026-12-20"},
		{Kind: "sell_limit", Value: 3, From: "tomorrow"},
	} {
		if bad.Valid() {
			t.Errorf("%+v was accepted", bad)
		}
	}
	if !(yieldrules.Rule{Kind: "rate_adjust", Value: 2, Percent: -50}).Valid() {
		t.Error("a 50 percent cut is allowed")
	}
}
