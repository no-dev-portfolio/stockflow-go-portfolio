package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Fault struct {
	Status  int
	Code    string
	Message string
}

func (f *Fault) Error() string { return f.Message }

var (
	ErrNotFound = &Fault{404, "not_found", "対象のデータが見つかりません。"}
	ErrStock    = &Fault{409, "insufficient_stock", "引当可能な在庫が不足しています。"}
	ErrState    = &Fault{409, "invalid_state", "現在の状態ではこの操作を実行できません。"}
	ErrExpired  = &Fault{409, "expired", "引当期限が切れたため、在庫を解放しました。"}
	ErrKey      = &Fault{409, "idempotency_conflict", "同じキーを異なる内容に再利用することはできません。"}
)

func Invalid(message string) error { return &Fault{400, "invalid_request", message} }
func AsFault(err error) *Fault {
	var f *Fault
	if errors.As(err, &f) {
		return f
	}
	return &Fault{500, "internal_error", "処理を完了できませんでした。リクエストIDとともに管理者へお知らせください。"}
}

var KeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,80}$`)
var SKUPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{1,31}$`)

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

type Actor struct {
	Tenant string `json:"tenant"`
	Name   string `json:"name"`
	Role   string `json:"role"`
}

func (a Actor) CanWrite() bool { return a.Role == "manager" || a.Role == "operator" }

type Item struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}
type CreateRequest struct {
	Reference  string `json:"reference"`
	TTLSeconds int    `json:"ttl_seconds"`
	Items      []Item `json:"items"`
}

func (r CreateRequest) Normalize() (CreateRequest, error) {
	r.Reference = strings.TrimSpace(r.Reference)
	if len([]rune(r.Reference)) < 1 || len([]rune(r.Reference)) > 64 {
		return r, Invalid("受注参照は1～64文字で指定してください。")
	}
	for _, c := range r.Reference {
		if unicode.IsControl(c) {
			return r, Invalid("受注参照に制御文字は使用できません。")
		}
	}
	if r.TTLSeconds < 30 || r.TTLSeconds > 86400 {
		return r, Invalid("引当期限は30～86400秒で指定してください。")
	}
	if len(r.Items) < 1 || len(r.Items) > 10 {
		return r, Invalid("明細は1～10行で指定してください。")
	}
	r.Items = append([]Item(nil), r.Items...)
	sort.Slice(r.Items, func(i, j int) bool { return r.Items[i].SKU < r.Items[j].SKU })
	for i, item := range r.Items {
		if !SKUPattern.MatchString(item.SKU) || item.Quantity < 1 || item.Quantity > 10000 {
			return r, Invalid("SKUまたは数量（1～10000）が不正です。")
		}
		if i > 0 && r.Items[i-1].SKU == item.SKU {
			return r, Invalid("同じSKUは1行にまとめてください。")
		}
	}
	return r, nil
}

type Stock struct {
	SKU       string `json:"sku"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	OnHand    int    `json:"on_hand"`
	Reserved  int    `json:"reserved"`
	Available int    `json:"available"`
	Threshold int    `json:"threshold"`
}
type Reservation struct {
	ID        string    `json:"id"`
	Reference string    `json:"reference"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	Version   int       `json:"version"`
	Items     []Item    `json:"items"`
}
type Event struct {
	ID            int64     `json:"id"`
	ReservationID string    `json:"reservation_id"`
	Type          string    `json:"type"`
	Status        string    `json:"status"`
	Attempts      int       `json:"attempts"`
	LastError     string    `json:"last_error"`
	CreatedAt     time.Time `json:"created_at"`
	NextAttempt   time.Time `json:"next_attempt"`
}
type Audit struct {
	ID            int64     `json:"id"`
	ReservationID string    `json:"reservation_id"`
	Action        string    `json:"action"`
	Actor         string    `json:"actor"`
	CreatedAt     time.Time `json:"created_at"`
}
type Summary struct {
	Products  int `json:"products"`
	Available int `json:"available"`
	Active    int `json:"active"`
	Confirmed int `json:"confirmed"`
	Pending   int `json:"pending"`
	Failed    int `json:"failed"`
	Delivered int `json:"delivered"`
}

func CheckVersion(v int) error {
	if v < 1 {
		return Invalid(fmt.Sprintf("versionは1以上で指定してください（指定値 %d）。", v))
	}
	return nil
}
