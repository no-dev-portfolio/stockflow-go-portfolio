package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name    string
		request CreateRequest
		valid   bool
	}{{"valid", CreateRequest{"  受注A  ", 30, []Item{{"SKU-1", 1}}}, true}, {"empty reference", CreateRequest{" ", 30, []Item{{"SKU-1", 1}}}, false}, {"long reference", CreateRequest{strings.Repeat("あ", 65), 30, []Item{{"SKU-1", 1}}}, false}, {"C1 character", CreateRequest{"A\u0085B", 30, []Item{{"SKU-1", 1}}}, false}, {"newline", CreateRequest{"A\nB", 30, []Item{{"SKU-1", 1}}}, false}, {"no items", CreateRequest{"A", 30, nil}, false}, {"negative", CreateRequest{"A", 30, []Item{{"SKU-1", -1}}}, false}, {"quantity limit", CreateRequest{"A", 30, []Item{{"SKU-1", 10001}}}, false}, {"SKU injection", CreateRequest{"A", 30, []Item{{"';DROP", 1}}}, false}, {"duplicates", CreateRequest{"A", 30, []Item{{"SKU-1", 1}, {"SKU-1", 2}}}, false}, {"short ttl", CreateRequest{"A", 29, []Item{{"SKU-1", 1}}}, false}, {"long ttl", CreateRequest{"A", 86401, []Item{{"SKU-1", 1}}}, false}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.request.Normalize()
			if (err == nil) != c.valid {
				t.Fatalf("valid %v error %v", c.valid, err)
			}
		})
	}
}
func TestCanonicalSortWithoutMutation(t *testing.T) {
	r := CreateRequest{" A ", 300, []Item{{"Z-1", 1}, {"A-1", 2}}}
	original := append([]Item(nil), r.Items...)
	got, err := r.Normalize()
	if err != nil || got.Reference != "A" || got.Items[0].SKU != "A-1" || !reflect.DeepEqual(r.Items, original) {
		t.Fatalf("sort or mutation failed: %+v", got)
	}
}
func TestIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := ID()
		if len(id) != 32 || seen[id] {
			t.Fatal("invalid/random duplicate ID")
		}
		seen[id] = true
	}
}
func TestPermissions(t *testing.T) {
	for _, role := range []string{"manager", "operator", "viewer", "", "admin"} {
		got := (Actor{Role: role}).CanWrite()
		want := role == "manager" || role == "operator"
		if got != want {
			t.Fatal(role)
		}
	}
}
func TestFaults(t *testing.T) {
	if AsFault(ErrStock).Status != 409 || AsFault(Invalid("bad")).Status != 400 || CheckVersion(0) == nil {
		t.Fatal("error classification")
	}
}
func FuzzNormalize(f *testing.F) {
	f.Add("REF", "SKU-1", 1, 300)
	f.Add("a\x00", "';--", -1, 0)
	f.Fuzz(func(t *testing.T, reference, sku string, qty, ttl int) {
		r := CreateRequest{reference, ttl, []Item{{sku, qty}}}
		got, err := r.Normalize()
		if err == nil {
			if len(got.Items) != 1 || got.Items[0].Quantity < 1 || got.Items[0].Quantity > 10000 || got.TTLSeconds < 30 || got.TTLSeconds > 86400 {
				t.Fatal("invalid accepted")
			}
		}
	})
}

func BenchmarkNormalize(b *testing.B) {
	r := CreateRequest{Reference: "B-ORDER", TTLSeconds: 300, Items: []Item{{SKU: "CTRL-101", Quantity: 1}, {SKU: "SENSOR-220", Quantity: 2}}}
	for i := 0; i < b.N; i++ {
		if _, err := r.Normalize(); err != nil {
			b.Fatal(err)
		}
	}
}
