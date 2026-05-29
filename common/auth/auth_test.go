package auth

import (
	"context"
	"testing"
)

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "empty", header: "", want: ""},
		{name: "valid bearer", header: "Bearer abc.def", want: "abc.def"},
		{name: "lowercase scheme", header: "bearer token", want: "token"},
		{name: "wrong scheme", header: "Basic token", want: ""},
		{name: "missing token", header: "Bearer", want: ""},
		{name: "too many parts", header: "Bearer token extra", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BearerToken(tt.header); got != tt.want {
				t.Fatalf("BearerToken(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestClaimsHasRole(t *testing.T) {
	claims := Claims{Roles: []string{"User", "Admin"}}
	if !claims.HasRole("admin") {
		t.Fatal("expected role comparison to be case-insensitive")
	}
	if claims.HasRole("manager") {
		t.Fatal("unexpected role match")
	}
}

func TestClaimsContext(t *testing.T) {
	want := Claims{Username: "Test Max", Email: "test.max@example.com"}
	ctx := ContextWithClaims(context.Background(), want)
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("claims not found in context")
	}
	if got.Username != want.Username || got.Email != want.Email {
		t.Fatalf("claims = %+v, want %+v", got, want)
	}
}
