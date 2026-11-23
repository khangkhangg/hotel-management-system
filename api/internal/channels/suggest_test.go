package channels

import "testing"

func TestSuggestionsMatchByNameOnceEachAndLeaveGuessesOut(t *testing.T) {
	local := []LocalRate{
		{PlanID: "p1", RoomName: "Deluxe", PlanName: "Flexible"},
		{PlanID: "p2", RoomName: "Deluxe", PlanName: "Non-refundable"},
		{PlanID: "p3", RoomName: "Garden Suite", PlanName: "Flexible"},
		{PlanID: "p4", RoomName: "Attic", PlanName: "Mystery"},
	}
	remote := []RemoteRate{
		{RoomID: "R1", RoomName: "Deluxe Double Room", RateID: "A", RateName: "Flexible rate"},
		{RoomID: "R1", RoomName: "Deluxe Double Room", RateID: "B", RateName: "Non Refundable"},
		{RoomID: "R2", RoomName: "Garden suite with balcony", RateID: "C", RateName: "Flexible"},
	}
	got := Suggest(local, remote)
	want := map[string][2]string{"p1": {"R1", "A"}, "p2": {"R1", "B"}, "p3": {"R2", "C"}}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for _, s := range got {
		if w, ok := want[s.PlanID]; !ok || w[0] != s.RoomID || w[1] != s.RateID {
			t.Errorf("%+v, want %v", s, w)
		}
	}
	if len(Suggest(local[:1], append(remote[:0:0], remote[0], remote[0]))) != 1 {
		t.Error("one remote rate was suggested for two plans, or one plan twice")
	}
}
