package api

import "testing"

func TestTotal(t *testing.T) {
	if Total([]int{1, 2, 3}) != 6 {
		t.Fatal("total")
	}
	if (Cart{Items: []int{4}}).Sum() != 4 {
		t.Fatal("sum")
	}
}
