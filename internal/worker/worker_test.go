package worker_test

import (
	"context"
	"example.com/stockflow/internal/testutil"
	"example.com/stockflow/internal/worker"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestWorkersDrainAndStop(t *testing.T) {
	db := testutil.DB(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Worker{Store: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Count: 2, Interval: 10 * time.Millisecond}.Run(ctx)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM delivery_receipts`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 3 {
			cancel()
			select {
			case <-done:
				return
			case <-time.After(2 * time.Second):
				t.Fatal("workers did not stop")
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("not drained")
}
