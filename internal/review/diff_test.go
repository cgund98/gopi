package review

import "testing"

func TestRejectRestoresHunk(t *testing.T) {
	baseline := "alpha\nbeta\ngamma\n"
	current := "alpha\nBETA\ngamma\n"
	hunks := Diff(baseline, current)
	if len(hunks) != 1 {
		t.Fatalf("hunks = %d", len(hunks))
	}
	next, err := Reject(current, hunks[0])
	if err != nil {
		t.Fatal(err)
	}
	if next != baseline {
		t.Fatalf("restored = %q", next)
	}
}

func TestSecondHunkKeepsItsID(t *testing.T) {
	baseline := "a\nb\nc\n"
	current := "A\nb\nC\n"
	before := Diff(baseline, current)
	if len(before) != 2 {
		t.Fatalf("hunks = %d", len(before))
	}
	reverted, err := Reject(current, before[0])
	if err != nil {
		t.Fatal(err)
	}
	after := Diff(baseline, reverted)
	if len(after) != 1 || after[0].ID != before[1].ID {
		t.Fatalf("remaining = %+v want %s", after, before[1].ID)
	}
}
