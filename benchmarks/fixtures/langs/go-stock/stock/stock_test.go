package stock

import "testing"

func TestAddRemove(t *testing.T) {
	b := NewBook()
	b.Add("north", "A1", 5)
	if err := b.Remove("north", "A1", 3); err != nil {
		t.Fatal(err)
	}
	if err := b.Remove("north", "A1", 3); err != ErrInsufficient {
		t.Fatalf("got %v", err)
	}
	if l := b.Levels(); len(l) != 1 || l[0].Quantity != 2 {
		t.Fatalf("levels %+v", l)
	}
}
