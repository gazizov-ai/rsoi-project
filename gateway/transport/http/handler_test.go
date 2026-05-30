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

func TestNewEventIDLooksLikeUUID(t *testing.T) {
	got := newEventID()
	if len(got) != 36 {
		t.Fatalf("event id length = %d, want 36: %q", len(got), got)
	}
	for _, pos := range []int{8, 13, 18, 23} {
		if got[pos] != '-' {
			t.Fatalf("event id %q is missing dash at %d", got, pos)
		}
	}
}
