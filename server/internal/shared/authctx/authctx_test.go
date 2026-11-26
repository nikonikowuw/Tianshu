package authctx_test

import (
	"testing"

	"github.com/google/uuid"

	"tianshu/internal/shared/authctx"
)

func TestIdentityContextRoundTrip(t *testing.T) {
	if _, ok := authctx.IdentityFromContext(t.Context()); ok {
		t.Fatal("expected missing identity on empty context")
	}

	want := authctx.Identity{
		UserID:    uuid.Must(uuid.NewV7()),
		SessionID: uuid.Must(uuid.NewV7()),
		Username:  "admin",
	}
	ctx := authctx.WithIdentity(t.Context(), want)

	got, ok := authctx.IdentityFromContext(ctx)
	if !ok {
		t.Fatal("expected identity from context")
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
