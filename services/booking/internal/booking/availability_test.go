package booking

import "testing"

func TestAvailableBits(t *testing.T) {
	// 10 seats: seat 0 and seat 9 sold (bit 0 of byte 0, bit 1 of byte 1), seat 4 held.
	a := Availability{Seats: 10, Sold: []byte{0b1000_0000, 0b0100_0000}, Held: []int{4}}
	for seat, want := range map[int]bool{0: false, 1: true, 4: false, 8: true, 9: false, 10: false, -1: false} {
		if got := a.Available(seat); got != want {
			t.Errorf("Available(%d) = %v, want %v", seat, got, want)
		}
	}
}
