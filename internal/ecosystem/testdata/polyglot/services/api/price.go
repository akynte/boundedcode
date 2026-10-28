package api

// Total sums line prices in cents.
func Total(cents []int) int {
	t := 0
	for _, c := range cents {
		t += c
	}
	return t
}

type Cart struct {
	Items []int
}

func (c Cart) Sum() int { return Total(c.Items) }
