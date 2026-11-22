package ids_test

import (
	"bytes"
	"testing"

	"hotelpms/api/internal/ids"
)

func TestNewIsVersion7(t *testing.T) {
	if v := ids.New().Version(); v != 7 {
		t.Fatalf("version %d, want 7", v)
	}
}

func TestNewIsTimeOrdered(t *testing.T) {
	prev := ids.New()
	for i := 0; i < 1000; i++ {
		next := ids.New()
		if bytes.Compare(prev[:], next[:]) >= 0 {
			t.Fatalf("id %d (%s) is not after the previous (%s)", i, next, prev)
		}
		prev = next
	}
}
