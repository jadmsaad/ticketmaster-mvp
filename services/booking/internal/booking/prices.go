package booking

import "fmt"

// MaxPrices is how many distinct prices one event can have: each seat stores
// its price's index in one byte.
const MaxPrices = 256

// EncodePrices dictionary-encodes per-seat prices (ADR 12): prices lists each
// distinct price once, in order of first appearance, and seatPrice holds one
// byte per seat, the index of that seat's price in prices. A venue has few
// distinct prices and neighbouring seats share them, so seatPrice compresses
// to almost nothing.
//
//	perSeat:   [8500 8500 12000 12000 8500]
//	prices:    [8500 12000]
//	seatPrice: [0 0 1 1 0]
func EncodePrices(perSeat []int) (prices []int, seatPrice []byte, err error) {
	// price → its index in prices
	index := make(map[int]byte)
	seatPrice = make([]byte, len(perSeat))
	for seat, price := range perSeat {
		i, seen := index[price]
		if !seen {
			// A new price: add it to the dictionary, unless it's full.
			if len(prices) == MaxPrices {
				return nil, nil, fmt.Errorf("%w: more than %d", ErrTooManyPrices, MaxPrices)
			}
			i = byte(len(prices))
			index[price] = i
			prices = append(prices, price)
		}
		// The seat stores the index, not the price.
		seatPrice[seat] = i
	}
	return prices, seatPrice, nil
}
