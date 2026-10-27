package store

import (
	"context"
	"example.com/stockflow/internal/domain"
)

func (s *Store) Stocks(ctx context.Context, tenant string) ([]domain.Stock, error) {
	result := []domain.Stock{}
	rows, err := s.Pool.Query(ctx, `SELECT sku,name,category,on_hand,reserved,on_hand-reserved,threshold FROM stock WHERE tenant_id=$1 ORDER BY sku`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v domain.Stock
		if err = rows.Scan(&v.SKU, &v.Name, &v.Category, &v.OnHand, &v.Reserved, &v.Available, &v.Threshold); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (s *Store) Reservations(ctx context.Context, tenant string, limit, offset int) ([]domain.Reservation, int, error) {
	result := []domain.Reservation{}
	var count int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM reservations WHERE tenant_id=$1`, tenant).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id,reference,status,expires_at,created_at,version FROM reservations WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var v domain.Reservation
		v.Items = []domain.Item{}
		if err = rows.Scan(&v.ID, &v.Reference, &v.Status, &v.ExpiresAt, &v.CreatedAt, &v.Version); err != nil {
			rows.Close()
			return nil, 0, err
		}
		result = append(result, v)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(result) > 0 {
		ids := make([]string, len(result))
		for i, r := range result {
			ids[i] = r.ID
		}
		ir, e := s.Pool.Query(ctx, `SELECT reservation_id,sku,quantity FROM reservation_items WHERE tenant_id=$1 AND reservation_id=ANY($2) ORDER BY sku`, tenant, ids)
		if e != nil {
			return nil, 0, e
		}
		defer ir.Close()
		byID := map[string]int{}
		for i, r := range result {
			byID[r.ID] = i
		}
		for ir.Next() {
			var id string
			var item domain.Item
			if e = ir.Scan(&id, &item.SKU, &item.Quantity); e != nil {
				return nil, 0, e
			}
			i := byID[id]
			result[i].Items = append(result[i].Items, item)
		}
		if e = ir.Err(); e != nil {
			return nil, 0, e
		}
	}
	return result, count, nil
}
func (s *Store) Events(ctx context.Context, tenant string) ([]domain.Event, error) {
	result := []domain.Event{}
	rows, err := s.Pool.Query(ctx, `SELECT id,reservation_id,type,status,attempts,last_error,created_at,next_attempt FROM outbox WHERE tenant_id=$1 ORDER BY id DESC LIMIT 100`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v domain.Event
		if err = rows.Scan(&v.ID, &v.ReservationID, &v.Type, &v.Status, &v.Attempts, &v.LastError, &v.CreatedAt, &v.NextAttempt); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (s *Store) Audits(ctx context.Context, tenant, id string) ([]domain.Audit, error) {
	if _, err := s.Get(ctx, tenant, id); err != nil {
		return nil, err
	}
	result := []domain.Audit{}
	rows, err := s.Pool.Query(ctx, `SELECT id,reservation_id,action,actor,created_at FROM audit WHERE tenant_id=$1 AND reservation_id=$2 ORDER BY id DESC`, tenant, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v domain.Audit
		if err = rows.Scan(&v.ID, &v.ReservationID, &v.Action, &v.Actor, &v.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (s *Store) Summary(ctx context.Context, tenant string) (domain.Summary, error) {
	var v domain.Summary
	err := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM stock WHERE tenant_id=$1),(SELECT COALESCE(sum(on_hand-reserved),0) FROM stock WHERE tenant_id=$1),(SELECT count(*) FROM reservations WHERE tenant_id=$1 AND status='active'),(SELECT count(*) FROM reservations WHERE tenant_id=$1 AND status='confirmed'),(SELECT count(*) FROM outbox WHERE tenant_id=$1 AND status='pending'),(SELECT count(*) FROM outbox WHERE tenant_id=$1 AND status='failed'),(SELECT count(*) FROM outbox WHERE tenant_id=$1 AND status='delivered')`, tenant).Scan(&v.Products, &v.Available, &v.Active, &v.Confirmed, &v.Pending, &v.Failed, &v.Delivered)
	return v, err
}
