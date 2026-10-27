package testutil

import (
	"context"
	"example.com/stockflow/internal/domain"
	"example.com/stockflow/internal/store"
	"net/url"
	"os"
	"testing"
)

func DB(t testing.TB) *store.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PostgreSQL integration requires TEST_DATABASE_URL")
	}
	ctx := context.Background()
	admin, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + domain.ID()
	if _, err = admin.Pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Pool.Close()
		_, _ = admin.Pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Pool.Close()
	})
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}
func Manager() domain.Actor {
	return domain.Actor{Tenant: "factory-a", Name: "test-manager", Role: "manager"}
}
func Request(sku string, qty int) domain.CreateRequest {
	return domain.CreateRequest{Reference: "TEST-ORDER", TTLSeconds: 300, Items: []domain.Item{{SKU: sku, Quantity: qty}}}
}
