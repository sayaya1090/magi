package daemon

import "testing"

// A refused piece's receipt is taken back: nothing crossed, so nothing stays askable-about.
func TestADroppedReceiptIsGone(t *testing.T) {
	r := NewReceipts()
	id, err := r.Give("s_x")
	if err != nil {
		t.Fatal(err)
	}
	r.Drop(id)
	if _, _, _, ok := r.Where(id); ok {
		t.Fatal("a dropped receipt still answers")
	}
}
