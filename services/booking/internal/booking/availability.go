package booking

import "slices"

// Availability is an event's seat state at one moment: Sold is a bitmap in
// Redis bit order (seat 0 is the most significant bit of byte 0), Held lists
// seats with a live hold, and Prices and SeatPrice are the dictionary-encoded
// seat prices (see EncodePrices).
type Availability struct {
	Seats     int
	Sold      []byte
	Held      []int
	Prices    []int
	SeatPrice []byte
}

// Unavailable returns a bitmap with a seat's bit set if it's sold or held, in
// the same bit order as Sold, one bit per seat. Buyers see both the same way
// (ADR 12).
func (a Availability) Unavailable() []byte {
	bits := make([]byte, (a.Seats+7)/8)
	// Start from the sold bits. Redis drops trailing zero bytes, so Sold may be
	// shorter than bits; the rest stay zero.
	copy(bits, a.Sold)
	// Then switch on each held seat's bit: its byte, then its bit in that byte.
	for _, seat := range a.Held {
		if seat >= 0 && seat < a.Seats {
			bits[seat/8] |= 0x80 >> (seat % 8)
		}
	}
	return bits
}

// Available reports whether a seat can be reserved: it exists, isn't sold,
// and isn't held.
func (a Availability) Available(seat int) bool {
	exists := seat >= 0 && seat < a.Seats
	return exists && !a.isSold(seat) && !a.isHeld(seat)
}

// isSold reports whether the seat's bit is 1 in the sold bitmap. The bitmap
// packs 8 seats per byte, numbered from the left, the same way Redis's SETBIT
// and GETBIT count. Example with seats 0 and 9 sold:
//
//	            byte 0                  byte 1
//	bit:    [ 1 0 0 0 0 0 0 0 ]   [ 0 1 0 0 0 0 0 0 ]
//	seat:     0 1 2 3 4 5 6 7       8 9 . . . . . .
//
// The comments below follow isSold(9).
func (a Availability) isSold(seat int) bool {
	// Which byte? Integer division drops the remainder: 9 / 8 = 1 → byte 1.
	byteIndex := seat / 8
	// Which bit inside that byte? The remainder: 9 % 8 = 1 → 2nd bit from the left.
	bitInByte := seat % 8
	// Redis drops trailing empty bytes (and the whole key if nothing is sold),
	// so a byte past the end means no seat in it was ever sold. Checking first
	// also avoids indexing past the end of the slice.
	if byteIndex >= len(a.Sold) {
		return false
	}
	// A mask with only this seat's bit on: 0x80 is 1000_0000, and shifting it
	// right by 1 gives 0100_0000.
	mask := byte(0x80) >> bitInByte
	// AND keeps a bit only where both sides have a 1, so this keeps just the
	// seat's bit: 0100_0000 & 0100_0000 = 0100_0000 ≠ 0 → sold. For seat 10 the
	// mask is 0010_0000, and 0100_0000 & 0010_0000 = 0 → not sold.
	return a.Sold[byteIndex]&mask != 0
}

// isHeld reports whether the seat has a live hold. Held is small (only active
// checkouts), so a linear scan is fine.
func (a Availability) isHeld(seat int) bool {
	return slices.Contains(a.Held, seat)
}
