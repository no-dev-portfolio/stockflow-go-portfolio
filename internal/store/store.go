package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"example.com/stockflow/internal/domain"
	"example.com/stockflow/migrations"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Store struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	c, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	c.MaxConns = 20
	c.MinConns = 1
	c.ConnConfig.RuntimeParams["timezone"] = "UTC"
	c.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	p, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{p}, nil
}
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(840271)`); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		var version int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
			return err
		}
		if version != 1 {
			return fmt.Errorf("unsupported migration version %d", version)
		}
		return tx.Commit(ctx)
	}
	body, err := migrations.Files.ReadFile("001_init.sql")
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, string(body)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) Seed(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(840272)`); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE id='factory-a')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `INSERT INTO tenants VALUES('factory-a','北関東ファクトリー'),('factory-b','別組織デモ');INSERT INTO stock(tenant_id,sku,name,category,on_hand,reserved,threshold)VALUES('factory-a','CTRL-101','制御モジュール','制御部品',120,0,20),('factory-a','SENSOR-220','精密センサー','計測部品',48,6,12),('factory-a','MOTOR-315','小型駆動ユニット','駆動部品',8,0,10);`)
	if err != nil {
		return err
	}
	for i, status := range []string{"active", "confirmed", "cancelled"} {
		id := fmt.Sprintf("demo%028d", i+1)
		sku := []string{"SENSOR-220", "MOTOR-315", "CTRL-101"}[i]
		qty := []int{6, 3, 4}[i]
		_, err = tx.Exec(ctx, `INSERT INTO reservations(id,tenant_id,reference,status,expires_at)VALUES($1,'factory-a',$2,$3,now()+interval '2 hours')`, id, fmt.Sprintf("受注 SF-1027-%03d", i+1), status)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO reservation_items VALUES('factory-a',$1,$2,$3)`, id, sku, qty)
		}
		if err != nil {
			return err
		}
		if err = writeEvent(ctx, tx, "factory-a", id, "reservation."+map[string]string{"active": "created", "confirmed": "confirmed", "cancelled": "cancelled"}[status], "demo-seed"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func writeEvent(ctx context.Context, tx pgx.Tx, tenant, id, action, actor string) error {
	_, err := tx.Exec(ctx, `INSERT INTO outbox(tenant_id,reservation_id,type)VALUES($1,$2,$3)`, tenant, id, action)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit(tenant_id,reservation_id,action,actor)VALUES($1,$2,$3,$4)`, tenant, id, action, actor)
	return err
}
func getReservation(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, tenant, id string) (domain.Reservation, error) {
	var r domain.Reservation
	r.Items = []domain.Item{}
	err := q.QueryRow(ctx, `SELECT id,reference,status,expires_at,created_at,version FROM reservations WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&r.ID, &r.Reference, &r.Status, &r.ExpiresAt, &r.CreatedAt, &r.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, domain.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	rows, err := q.Query(ctx, `SELECT sku,quantity FROM reservation_items WHERE tenant_id=$1 AND reservation_id=$2 ORDER BY sku`, tenant, id)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.Item
		if err = rows.Scan(&item.SKU, &item.Quantity); err != nil {
			return r, err
		}
		r.Items = append(r.Items, item)
	}
	return r, rows.Err()
}
func (s *Store) Get(ctx context.Context, tenant, id string) (domain.Reservation, error) {
	return getReservation(ctx, s.Pool, tenant, id)
}
func (s *Store) Create(ctx context.Context, a domain.Actor, key string, request domain.CreateRequest) (domain.Reservation, bool, error) {
	var zero domain.Reservation
	if !a.CanWrite() {
		return zero, false, &domain.Fault{Status: 403, Code: "forbidden", Message: "この役割には操作権限がありません。"}
	}
	if !domain.KeyPattern.MatchString(key) {
		return zero, false, domain.Invalid("Idempotency-Keyは8～80文字の英数字・ハイフン・アンダースコアで指定してください。")
	}
	normalized, err := request.Normalize()
	if err != nil {
		return zero, false, err
	}
	body, _ := json.Marshal(normalized)
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return zero, false, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, a.Tenant+":"+key); err != nil {
		return zero, false, err
	}
	var priorHash, id string
	err = tx.QueryRow(ctx, `SELECT request_hash,reservation_id FROM idempotency WHERE tenant_id=$1 AND key=$2`, a.Tenant, key).Scan(&priorHash, &id)
	if err == nil {
		if priorHash != hash {
			return zero, false, domain.ErrKey
		}
		r, e := getReservation(ctx, tx, a.Tenant, id)
		if e != nil {
			return zero, false, e
		}
		if e = tx.Commit(ctx); e != nil {
			return zero, false, e
		}
		return r, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, false, err
	}
	for _, item := range normalized.Items {
		var available int
		err = tx.QueryRow(ctx, `SELECT on_hand-reserved FROM stock WHERE tenant_id=$1 AND sku=$2 FOR UPDATE`, a.Tenant, item.SKU).Scan(&available)
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, false, domain.ErrNotFound
		}
		if err != nil {
			return zero, false, err
		}
		if available < item.Quantity {
			return zero, false, domain.ErrStock
		}
	}
	id = domain.ID()
	_, err = tx.Exec(ctx, `INSERT INTO reservations(id,tenant_id,reference,status,expires_at)VALUES($1,$2,$3,'active',clock_timestamp()+make_interval(secs=>$4))`, id, a.Tenant, normalized.Reference, normalized.TTLSeconds)
	if err != nil {
		return zero, false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO idempotency VALUES($1,$2,$3,$4,now())`, a.Tenant, key, hash, id)
	if err != nil {
		return zero, false, err
	}
	for _, item := range normalized.Items {
		_, err = tx.Exec(ctx, `UPDATE stock SET reserved=reserved+$3 WHERE tenant_id=$1 AND sku=$2`, a.Tenant, item.SKU, item.Quantity)
		if err != nil {
			return zero, false, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO reservation_items VALUES($1,$2,$3,$4)`, a.Tenant, id, item.SKU, item.Quantity)
		if err != nil {
			return zero, false, err
		}
	}
	if err = writeEvent(ctx, tx, a.Tenant, id, "reservation.created", a.Name); err != nil {
		return zero, false, err
	}
	r, err := getReservation(ctx, tx, a.Tenant, id)
	if err != nil {
		return zero, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, false, err
	}
	return r, false, nil
}
func (s *Store) Transition(ctx context.Context, a domain.Actor, id, target string, version int) (domain.Reservation, error) {
	var zero domain.Reservation
	if !a.CanWrite() {
		return zero, &domain.Fault{Status: 403, Code: "forbidden", Message: "この役割には操作権限がありません。"}
	}
	if target != "confirmed" && target != "cancelled" {
		return zero, domain.Invalid("不正な遷移です。")
	}
	if err := domain.CheckVersion(version); err != nil {
		return zero, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer rollback(tx)
	var status string
	var current int
	var expired bool
	err = tx.QueryRow(ctx, `SELECT status,version FROM reservations WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, a.Tenant, id).Scan(&status, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, domain.ErrNotFound
	}
	if err != nil {
		return zero, err
	}
	if status == "expired" {
		r, e := getReservation(ctx, tx, a.Tenant, id)
		if e != nil {
			return zero, e
		}
		if e = tx.Commit(ctx); e != nil {
			return zero, e
		}
		return r, domain.ErrExpired
	}
	if status == target {
		r, e := getReservation(ctx, tx, a.Tenant, id)
		if e != nil {
			return zero, e
		}
		return r, tx.Commit(ctx)
	}
	if status != "active" || current != version {
		return zero, domain.ErrState
	}
	// Acquire all stock rows before the deadline decision: waiting may cross expiry.
	locked, err := tx.Query(ctx, `SELECT s.sku FROM stock s JOIN reservation_items i ON i.tenant_id=s.tenant_id AND i.sku=s.sku WHERE i.tenant_id=$1 AND i.reservation_id=$2 ORDER BY s.sku FOR UPDATE OF s`, a.Tenant, id)
	if err != nil {
		return zero, err
	}
	for locked.Next() {
		var sku string
		if err = locked.Scan(&sku); err != nil {
			locked.Close()
			return zero, err
		}
	}
	locked.Close()
	if err = locked.Err(); err != nil {
		return zero, err
	}
	if err = tx.QueryRow(ctx, `SELECT expires_at<=clock_timestamp() FROM reservations WHERE tenant_id=$1 AND id=$2`, a.Tenant, id).Scan(&expired); err != nil {
		return zero, err
	}
	actual := target
	if expired {
		actual = "expired"
	}
	if err = releaseStock(ctx, tx, a.Tenant, id, actual == "confirmed"); err != nil {
		return zero, err
	}
	if _, err = tx.Exec(ctx, `UPDATE reservations SET status=$3,version=version+1 WHERE tenant_id=$1 AND id=$2`, a.Tenant, id, actual); err != nil {
		return zero, err
	}
	if err = writeEvent(ctx, tx, a.Tenant, id, "reservation."+actual, a.Name); err != nil {
		return zero, err
	}
	r, err := getReservation(ctx, tx, a.Tenant, id)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	if expired {
		return r, domain.ErrExpired
	}
	return r, nil
}
func releaseStock(ctx context.Context, tx pgx.Tx, tenant, id string, consume bool) error {
	rows, err := tx.Query(ctx, `SELECT sku,quantity FROM reservation_items WHERE tenant_id=$1 AND reservation_id=$2 ORDER BY sku`, tenant, id)
	if err != nil {
		return err
	}
	items := []domain.Item{}
	for rows.Next() {
		var i domain.Item
		if err = rows.Scan(&i.SKU, &i.Quantity); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, i := range items {
		consumeQty := 0
		if consume {
			consumeQty = i.Quantity
		}
		tag, e := tx.Exec(ctx, `UPDATE stock SET reserved=reserved-$3,on_hand=on_hand-$4 WHERE tenant_id=$1 AND sku=$2 AND reserved>=$3`, tenant, i.SKU, i.Quantity, consumeQty)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("stock invariant broken for %s", i.SKU)
		}
	}
	return nil
}
func (s *Store) ExpireOne(ctx context.Context) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	var id, tenant string
	err = tx.QueryRow(ctx, `SELECT id,tenant_id FROM reservations WHERE status='active' AND expires_at<=clock_timestamp() ORDER BY expires_at,id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = releaseStock(ctx, tx, tenant, id, false); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE reservations SET status='expired',version=version+1 WHERE id=$1`, id); err != nil {
		return false, err
	}
	if err = writeEvent(ctx, tx, tenant, id, "reservation.expired", "expiry-worker"); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// DeliverOne atomically records a local fulfillment receipt; it does not call external services.
func (s *Store) DeliverOne(ctx context.Context, simulateFailures int) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	var id int64
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id,attempts FROM outbox WHERE status='pending' AND next_attempt<=clock_timestamp() ORDER BY next_attempt,id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	attempts++
	if attempts <= simulateFailures {
		status := "pending"
		if attempts >= 5 {
			status = "failed"
		}
		_, err = tx.Exec(ctx, `UPDATE outbox SET attempts=$2,status=$3,last_error='デモ配送先の一時エラー',next_attempt=clock_timestamp()+make_interval(secs=>$4) WHERE id=$1`, id, attempts, status, 1<<min(attempts, 6))
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO delivery_receipts(event_id)VALUES($1) ON CONFLICT DO NOTHING`, id)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE outbox SET attempts=$2,status='delivered',last_error='' WHERE id=$1`, id, attempts)
		}
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
func (s *Store) Retry(ctx context.Context, a domain.Actor, id int64) error {
	if a.Role != "manager" {
		return &domain.Fault{Status: 403, Code: "forbidden", Message: "管理者権限が必要です。"}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var rid string
	err = tx.QueryRow(ctx, `UPDATE outbox SET status='pending',attempts=0,last_error='',next_attempt=clock_timestamp() WHERE tenant_id=$1 AND id=$2 AND status='failed' RETURNING reservation_id`, a.Tenant, id).Scan(&rid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit(tenant_id,reservation_id,action,actor)VALUES($1,$2,'outbox.retried',$3)`, a.Tenant, rid, a.Name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Transaction-scoped calls use a bounded request context. Background rollback is bounded by pgx connection cleanup.

func rollback(tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(cleanup)
}
