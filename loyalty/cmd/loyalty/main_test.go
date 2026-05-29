package main

import "testing"

func TestStatusFor(t *testing.T) {
	tests := []struct {
		count        int
		wantStatus   string
		wantDiscount int
	}{
		{count: 0, wantStatus: "BRONZE", wantDiscount: 5},
		{count: 9, wantStatus: "BRONZE", wantDiscount: 5},
		{count: 10, wantStatus: "SILVER", wantDiscount: 7},
		{count: 19, wantStatus: "SILVER", wantDiscount: 7},
		{count: 20, wantStatus: "GOLD", wantDiscount: 10},
	}

	for _, tt := range tests {
		t.Run(tt.wantStatus, func(t *testing.T) {
			gotStatus, gotDiscount := statusFor(tt.count)
			if gotStatus != tt.wantStatus || gotDiscount != tt.wantDiscount {
				t.Fatalf("statusFor(%d) = (%q, %d), want (%q, %d)", tt.count, gotStatus, gotDiscount, tt.wantStatus, tt.wantDiscount)
			}
		})
	}
}
