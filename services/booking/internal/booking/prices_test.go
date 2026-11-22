package booking

import (
	"errors"
	"slices"
	"testing"
)

func TestEncodePrices(t *testing.T) {
	perSeat := []int{8500, 8500, 12000, 12000, 8500, 25000}

	prices, seatPrice, err := EncodePrices(perSeat)
	if err != nil {
		t.Fatal(err)
	}
	// Each distinct price once, in order of first appearance.
	if want := []int{8500, 12000, 25000}; !slices.Equal(prices, want) {
		t.Errorf("prices = %v, want %v", prices, want)
	}
	if want := []byte{0, 0, 1, 1, 0, 2}; !slices.Equal(seatPrice, want) {
		t.Errorf("seatPrice = %v, want %v", seatPrice, want)
	}
	// Decoding gives every seat its price back.
	for seat, want := range perSeat {
		if got := prices[seatPrice[seat]]; got != want {
			t.Errorf("seat %d decodes to %d, want %d", seat, got, want)
		}
	}
}

func TestEncodePricesLimit(t *testing.T) {
	perSeat := make([]int, MaxPrices+1)
	for i := range perSeat {
		perSeat[i] = 1000 + i // every seat a different price
	}
	if _, _, err := EncodePrices(perSeat[:MaxPrices]); err != nil {
		t.Errorf("%d prices: %v, want ok", MaxPrices, err)
	}
	if _, _, err := EncodePrices(perSeat); !errors.Is(err, ErrTooManyPrices) {
		t.Errorf("%d prices: err = %v, want ErrTooManyPrices", MaxPrices+1, err)
	}
}
