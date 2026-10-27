package store_test

import (
	"context"
	"example.com/stockflow/internal/domain"
	"example.com/stockflow/internal/testutil"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var ctx = context.Background()

func TestCreateReplayAndConflict(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	r, repeat, err := db.Create(ctx, a, "request-001", testutil.Request("CTRL-101", 5))
	if err != nil || repeat {
		t.Fatal(err)
	}
	r2, repeat, err := db.Create(ctx, a, "request-001", testutil.Request("CTRL-101", 5))
	if err != nil || !repeat || r.ID != r2.ID {
		t.Fatal("replay", err)
	}
	_, _, err = db.Create(ctx, a, "request-001", testutil.Request("CTRL-101", 6))
	if domain.AsFault(err).Code != "idempotency_conflict" {
		t.Fatal(err)
	}
	stock, _ := db.Stocks(ctx, a.Tenant)
	if stock[0].Reserved != 5 {
		t.Fatal(stock)
	}
}
func TestCanonicalReplay(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	in := testutil.Request("CTRL-101", 1)
	in.Items = append(in.Items, domain.Item{SKU: "SENSOR-220", Quantity: 1})
	r, _, err := db.Create(ctx, a, "canonical-key", in)
	if err != nil {
		t.Fatal(err)
	}
	in.Items[0], in.Items[1] = in.Items[1], in.Items[0]
	r2, replay, err := db.Create(ctx, a, "canonical-key", in)
	if err != nil || !replay || r.ID != r2.ID {
		t.Fatal(err)
	}
}
func TestAllOrNothing(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	in := testutil.Request("CTRL-101", 2)
	in.Items = append(in.Items, domain.Item{SKU: "MOTOR-315", Quantity: 99})
	if _, _, err := db.Create(ctx, a, "atomic-key", in); domain.AsFault(err).Code != "insufficient_stock" {
		t.Fatal(err)
	}
	stocks, _ := db.Stocks(ctx, a.Tenant)
	if stocks[0].Reserved != 0 {
		t.Fatal("partial reservation")
	}
	rs, total, _ := db.Reservations(ctx, a.Tenant, 20, 0)
	if len(rs) != 3 || total != 3 {
		t.Fatal("partial data")
	}
	var keys int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM idempotency`).Scan(&keys); err != nil || keys != 0 {
		t.Fatal("key committed on failure")
	}
}
func TestMissingSKUAndUnknownTenant(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	_, _, err := db.Create(ctx, a, "missing-sku", testutil.Request("MISSING-1", 1))
	if domain.AsFault(err).Status != 404 {
		t.Fatal(err)
	}
	a.Tenant = "factory-b"
	_, _, err = db.Create(ctx, a, "other-sku-1", testutil.Request("CTRL-101", 1))
	if domain.AsFault(err).Status != 404 {
		t.Fatal(err)
	}
}
func TestConfirmCancelAndReplay(t *testing.T) {
	for _, target := range []string{"confirmed", "cancelled"} {
		t.Run(target, func(t *testing.T) {
			db := testutil.DB(t)
			a := testutil.Manager()
			r, _, err := db.Create(ctx, a, "state-key-1", testutil.Request("CTRL-101", 5))
			if err != nil {
				t.Fatal(err)
			}
			updated, err := db.Transition(ctx, a, r.ID, target, 1)
			if err != nil || updated.Status != target || updated.Version != 2 {
				t.Fatal(err)
			}
			again, err := db.Transition(ctx, a, r.ID, target, 1)
			if err != nil || again.Version != 2 {
				t.Fatal("not idempotent", err)
			}
			opposite := "confirmed"
			if target == opposite {
				opposite = "cancelled"
			}
			if _, err = db.Transition(ctx, a, r.ID, opposite, 2); domain.AsFault(err).Status != 409 {
				t.Fatal(err)
			}
			stock, _ := db.Stocks(ctx, a.Tenant)
			want := 120
			if target == "confirmed" {
				want = 115
			}
			if stock[0].OnHand != want || stock[0].Reserved != 0 {
				t.Fatal(stock)
			}
			audit, _ := db.Audits(ctx, a.Tenant, r.ID)
			if len(audit) != 2 {
				t.Fatal("duplicate audit")
			}
		})
	}
}
func TestStaleVersion(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	r, _, _ := db.Create(ctx, a, "stale-version", testutil.Request("CTRL-101", 1))
	if _, err := db.Transition(ctx, a, r.ID, "confirmed", 99); domain.AsFault(err).Status != 409 {
		t.Fatal(err)
	}
	if _, err := db.Transition(ctx, a, r.ID, "confirmed", 0); domain.AsFault(err).Status != 400 {
		t.Fatal(err)
	}
}
func TestExpiryOnCommand(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	r, _, _ := db.Create(ctx, a, "expired-create", testutil.Request("CTRL-101", 5))
	_, err := db.Pool.Exec(ctx, `UPDATE reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Transition(ctx, a, r.ID, "confirmed", 1)
	if domain.AsFault(err).Code != "expired" {
		t.Fatal(err)
	}
	var beforeEvents, afterEvents int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE reservation_id=$1`, r.ID).Scan(&beforeEvents); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"confirmed", "cancelled"} {
		again, e := db.Transition(ctx, a, r.ID, target, 1)
		if e == nil || domain.AsFault(e).Code != "expired" || again.Status != "expired" || again.Version != 2 {
			t.Fatalf("expiry replay: %+v %v", again, e)
		}
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE reservation_id=$1`, r.ID).Scan(&afterEvents); err != nil {
		t.Fatal(err)
	}
	if beforeEvents != afterEvents {
		t.Fatal("expiry replay wrote events")
	}
	got, _ := db.Get(ctx, a.Tenant, r.ID)
	stock, _ := db.Stocks(ctx, a.Tenant)
	if got.Status != "expired" || stock[0].OnHand != 120 || stock[0].Reserved != 0 {
		t.Fatal("expiry invariant")
	}
}
func TestExpiryWorker(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	r, _, _ := db.Create(ctx, a, "expiry-worker", testutil.Request("CTRL-101", 5))
	_, _ = db.Pool.Exec(ctx, `UPDATE reservations SET expires_at=now()-interval '1 second' WHERE id=$1`, r.ID)
	worked, err := db.ExpireOne(ctx)
	if err != nil || !worked {
		t.Fatal(err)
	}
	worked, err = db.ExpireOne(ctx)
	if err != nil || worked {
		t.Fatal("expiry repeated", err)
	}
	stocks, _ := db.Stocks(ctx, a.Tenant)
	if stocks[0].Reserved != 0 {
		t.Fatal(stocks)
	}
}
func TestTenantAndRoleIsolation(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	r, _, _ := db.Create(ctx, a, "tenant-secret", testutil.Request("CTRL-101", 1))
	other := a
	other.Tenant = "factory-b"
	if _, err := db.Get(ctx, other.Tenant, r.ID); domain.AsFault(err).Status != 404 {
		t.Fatal(err)
	}
	if _, err := db.Transition(ctx, other, r.ID, "cancelled", 1); domain.AsFault(err).Status != 404 {
		t.Fatal(err)
	}
	viewer := a
	viewer.Role = "viewer"
	if _, _, err := db.Create(ctx, viewer, "viewer-write", testutil.Request("CTRL-101", 1)); domain.AsFault(err).Status != 403 {
		t.Fatal(err)
	}
	if _, err := db.Transition(ctx, viewer, r.ID, "confirmed", 1); domain.AsFault(err).Status != 403 {
		t.Fatal(err)
	}
	if _, err := db.Audits(ctx, other.Tenant, r.ID); domain.AsFault(err).Status != 404 {
		t.Fatal(err)
	}
}
func TestConcurrentNoOversell(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	var success atomic.Int32
	var unexpected atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := db.Create(ctx, a, domain.ID(), testutil.Request("MOTOR-315", 1))
			if err == nil {
				success.Add(1)
			} else if domain.AsFault(err).Code != "insufficient_stock" {
				unexpected.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if success.Load() != 8 || unexpected.Load() != 0 {
		t.Fatalf("success %d unexpected %d", success.Load(), unexpected.Load())
	}
	stocks, _ := db.Stocks(ctx, a.Tenant)
	if stocks[1].Reserved != 8 || stocks[1].Available != 0 {
		t.Fatal(stocks)
	}
}
func TestConcurrentSameKey(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	ids := make(chan string, 10)
	errs := make(chan error, 10)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, _, err := db.Create(ctx, a, "parallel-same-key", testutil.Request("CTRL-101", 5))
			ids <- r.ID
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first || id == "" {
			t.Fatal("different IDs")
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	stocks, _ := db.Stocks(ctx, a.Tenant)
	if stocks[0].Reserved != 5 {
		t.Fatal("duplicate hold")
	}
}
func TestParallelStateChanges(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	r, _, _ := db.Create(ctx, a, "parallel-state", testutil.Request("CTRL-101", 5))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, target := range []string{"confirmed", "cancelled"} {
		wg.Add(1)
		go func(target string) { defer wg.Done(); _, err := db.Transition(ctx, a, r.ID, target, 1); errs <- err }(target)
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		if err == nil {
			ok++
		} else if domain.AsFault(err).Status != 409 {
			t.Fatal(err)
		}
	}
	if ok != 1 {
		t.Fatal("both succeeded")
	}
	stocks, _ := db.Stocks(ctx, a.Tenant)
	if stocks[0].Reserved != 0 || (stocks[0].OnHand != 115 && stocks[0].OnHand != 120) {
		t.Fatal(stocks)
	}
}
func TestDeliveryRetryAndDeadLetter(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	_, _ = db.Pool.Exec(ctx, `DELETE FROM outbox`)
	r, _, err := db.Create(ctx, a, "delivery-failure", testutil.Request("CTRL-101", 1))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, _ = db.Pool.Exec(ctx, `UPDATE outbox SET next_attempt=now()-interval '1 second'`)
		worked, e := db.DeliverOne(ctx, 5)
		if e != nil || !worked {
			t.Fatal(e)
		}
	}
	events, _ := db.Events(ctx, a.Tenant)
	if len(events) != 1 || events[0].Status != "failed" || events[0].Attempts != 5 {
		t.Fatal(events)
	}
	operator := a
	operator.Role = "operator"
	if err = db.Retry(ctx, operator, events[0].ID); domain.AsFault(err).Status != 403 {
		t.Fatal(err)
	}
	if err = db.Retry(ctx, a, events[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DeliverOne(ctx, 0); err != nil {
		t.Fatal(err)
	}
	var receipts int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM delivery_receipts`).Scan(&receipts)
	if receipts != 1 {
		t.Fatal(receipts)
	}
	audit, _ := db.Audits(ctx, a.Tenant, r.ID)
	if len(audit) != 2 || audit[0].Action != "outbox.retried" {
		t.Fatal(audit)
	}
}
func TestRetryBackoff(t *testing.T) {
	db := testutil.DB(t)
	_, _ = db.Pool.Exec(ctx, `DELETE FROM outbox`)
	_, _, err := db.Create(ctx, testutil.Manager(), "backoff-key-1", testutil.Request("CTRL-101", 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DeliverOne(ctx, 2); err != nil {
		t.Fatal(err)
	}
	worked, err := db.DeliverOne(ctx, 2)
	if err != nil || worked {
		t.Fatal("ignored backoff")
	}
}
func TestParallelWorkersDeliverOnce(t *testing.T) {
	db := testutil.DB(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				worked, err := db.DeliverOne(ctx, 0)
				if err != nil {
					t.Error(err)
					return
				}
				if !worked {
					return
				}
			}
		}()
	}
	wg.Wait()
	var count, unique int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT event_id) FROM delivery_receipts`).Scan(&count, &unique)
	if count != 3 || unique != 3 {
		t.Fatalf("%d %d", count, unique)
	}
}
func TestCancelledContext(t *testing.T) {
	db := testutil.DB(t)
	c, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err := db.Create(c, testutil.Manager(), "cancel-request", testutil.Request("CTRL-101", 1))
	if err == nil {
		t.Fatal("accepted cancelled context")
	}
}
func TestLockWaitRespectsDeadline(t *testing.T) {
	db := testutil.DB(t)
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `SELECT 1 FROM stock WHERE tenant_id='factory-a' AND sku='CTRL-101' FOR UPDATE`)
	if err != nil {
		t.Fatal(err)
	}
	c, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err = db.Create(c, testutil.Manager(), "blocked-request", testutil.Request("CTRL-101", 1))
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatal("deadline not respected", err)
	}
}
func TestSeedAndMigrationsRepeat(t *testing.T) {
	db := testutil.DB(t)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	summary, err := db.Summary(ctx, "factory-a")
	if err != nil || summary.Products != 3 || summary.Active != 1 || summary.Confirmed != 1 || summary.Available != 170 {
		t.Fatal(summary, err)
	}
}
func TestDatabaseConstraints(t *testing.T) {
	db := testutil.DB(t)
	if _, err := db.Pool.Exec(ctx, `UPDATE stock SET reserved=on_hand+1`); err == nil {
		t.Fatal("missing over-reservation constraint")
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE stock SET on_hand=-1`); err == nil {
		t.Fatal("negative stock accepted")
	}
}

func BenchmarkCreateCancel(b *testing.B) {
	db := testutil.DB(b)
	a := testutil.Manager()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, _, err := db.Create(ctx, a, domain.ID(), testutil.Request("CTRL-101", 1))
		if err != nil {
			b.Fatal(err)
		}
		if _, err = db.Transition(ctx, a, r.ID, "cancelled", 1); err != nil {
			b.Fatal(err)
		}
	}
}

func TestExpiryIsRecheckedAfterStockLockWait(t *testing.T) {
	db := testutil.DB(t)
	a := testutil.Manager()
	r, _, err := db.Create(ctx, a, "lock-expiry-boundary", testutil.Request("CTRL-101", 5))
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, `SELECT 1 FROM stock WHERE tenant_id='factory-a' AND sku='CTRL-101' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE reservations SET expires_at=clock_timestamp()+interval '200 milliseconds' WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, e := db.Transition(ctx, a, r.ID, "confirmed", 1); result <- e }()
	time.Sleep(400 * time.Millisecond)
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("transition did not finish")
	}
	if err == nil || domain.AsFault(err).Code != "expired" {
		t.Fatalf("expired reservation confirmed after lock wait: %v", err)
	}
	got, err := db.Get(ctx, a.Tenant, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	stocks, err := db.Stocks(ctx, a.Tenant)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "expired" || stocks[0].OnHand != 120 || stocks[0].Reserved != 0 {
		t.Fatalf("invalid expiry state: %+v %+v", got, stocks[0])
	}
}
