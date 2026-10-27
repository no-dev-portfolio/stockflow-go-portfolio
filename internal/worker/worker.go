package worker

import (
	"context"
	"example.com/stockflow/internal/store"
	"log/slog"
	"sync"
	"time"
)

type Worker struct {
	Store            *store.Store
	Logger           *slog.Logger
	Count            int
	SimulateFailures int
	Interval         time.Duration
}

func (w Worker) Run(ctx context.Context) {
	var group sync.WaitGroup
	for i := 0; i < w.Count; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			ticker := time.NewTicker(w.Interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					for task := 0; task < 50; task++ {
						taskctx, cancel := context.WithTimeout(ctx, 5*time.Second)
						expired, e1 := w.Store.ExpireOne(taskctx)
						delivered, e2 := w.Store.DeliverOne(taskctx, w.SimulateFailures)
						cancel()
						if e1 != nil || e2 != nil {
							if ctx.Err() == nil {
								w.Logger.Error("worker_task_failed", "expiry_error", e1, "delivery_error", e2)
							}
							break
						}
						if !expired && !delivered {
							break
						}
					}
				}
			}
		}()
	}
	group.Wait()
}
