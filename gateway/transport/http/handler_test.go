package transport

import "testing"

func TestFullAddress(t *testing.T) {
	got := fullAddress(hotelResponse{
		Country: "Россия",
		City:    "Москва",
		Address: "Неглинная ул., 4",
	})
	want := "Россия, Москва, Неглинная ул., 4"
	if got != want {
		t.Fatalf("fullAddress() = %q, want %q", got, want)
	}
}

func TestFullAddressSkipsEmptyParts(t *testing.T) {
	got := fullAddress(hotelResponse{
		Country: "Россия",
		City:    " ",
		Address: "Неглинная ул., 4",
	})
	want := "Россия, Неглинная ул., 4"
	if got != want {
		t.Fatalf("fullAddress() = %q, want %q", got, want)
	}
}

func TestBronzeLoyalty(t *testing.T) {
	got := bronzeLoyalty("Test Max")
	if got.Username != "Test Max" {
		t.Fatalf("username = %q", got.Username)
	}
	if got.Status != "BRONZE" || got.Discount != 5 || got.ReservationCount != 0 {
		t.Fatalf("bronze loyalty = %+v", got)
	}
}
