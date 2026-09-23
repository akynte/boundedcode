package eval

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// The order arms run in, and why it is not the order they are listed in.
//
// Running every task under arm A and then every task under arm B gives B
// warmer caches, a warmer page cache, a warmer model server and whatever
// thermal state the first pass left behind. None of that is the treatment,
// and all of it lands on one side of the comparison. On a laptop GPU the
// effect is not small.
//
// Counterbalancing costs nothing and removes the whole class of objection:
// the arm order is rotated per task and per repetition, so no arm is
// systematically first, and the order actually used is recorded on every run
// so a reader can check rather than trust.

// ArmOrder is the sequence for one (task, repetition) cell.
type ArmOrder struct {
	Task       string   `json:"task"`
	Repetition int      `json:"repetition"`
	Order      []string `json:"order"`
}

// OrderArms returns the arm sequence for one cell.
//
// Deterministic from the seed, the task and the repetition, so a run is
// reproducible and the rotation is not something anybody chose. Rotation
// rather than a shuffle: with two or three arms a shuffle leaves visible
// imbalance over a small set, while a rotation guarantees each arm leads
// about equally often.
func OrderArms(seed int64, task string, repetition int, arms []Arm) []Arm {
	if len(arms) < 2 {
		return arms
	}
	h := sha256.New()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(seed)) //nolint:gosec // a seed, not a size
	h.Write(buf[:])
	fmt.Fprintf(h, "%d:%s|%d", len(task), task, repetition)
	offset := int(binary.BigEndian.Uint32(h.Sum(nil)[:4])) % len(arms)

	out := make([]Arm, 0, len(arms))
	for i := range arms {
		out = append(out, arms[(i+offset)%len(arms)])
	}
	return out
}

// OrderBalance reports how often each arm ran first, so a reader can see the
// counterbalancing worked rather than being told it did.
type OrderBalance struct {
	FirstCount map[string]int `json:"first_count"`
	Cells      int            `json:"cells"`
	// Skew is the largest share any one arm held the first position for. At
	// 1/n it is perfectly balanced; near 1 the counterbalancing did not
	// happen and order is a candidate explanation for any difference.
	Skew float64 `json:"skew"`
}

// BalanceOf summarises the recorded orders.
func BalanceOf(orders []ArmOrder) OrderBalance {
	b := OrderBalance{FirstCount: map[string]int{}}
	for _, o := range orders {
		if len(o.Order) == 0 {
			continue
		}
		b.Cells++
		b.FirstCount[o.Order[0]]++
	}
	if b.Cells == 0 {
		return b
	}
	for _, n := range b.FirstCount {
		if share := float64(n) / float64(b.Cells); share > b.Skew {
			b.Skew = share
		}
	}
	return b
}
