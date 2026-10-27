package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"example.com/stockflow/internal/domain"
	"example.com/stockflow/internal/store"
	"example.com/stockflow/web"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Credential struct {
	Hash  [32]byte
	Actor domain.Actor
}

func DemoCredentials() []Credential {
	keys := []struct{ key, name, tenant, role string }{{"demo-manager-a", "製造管理者", "factory-a", "manager"}, {"demo-operator-a", "出荷担当者", "factory-a", "operator"}, {"demo-viewer-a", "参照担当者", "factory-a", "viewer"}, {"demo-manager-b", "別組織の管理者", "factory-b", "manager"}}
	out := []Credential{}
	for _, v := range keys {
		out = append(out, Credential{sha256.Sum256([]byte(v.key)), domain.Actor{Tenant: v.tenant, Name: v.name, Role: v.role}})
	}
	return out
}

type API struct {
	Store       *store.Store
	Logger      *slog.Logger
	Credentials []Credential
	Requests    atomic.Uint64
	Failures    atomic.Uint64
}

func (a *API) Handler() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(func(c *gin.Context) {
		start := time.Now()
		id := domain.ID()
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		a.Requests.Add(1)
		if c.Writer.Status() >= 500 {
			a.Failures.Add(1)
		}
		a.Logger.Info("http_request", "request_id", id, "method", c.Request.Method, "route", c.FullPath(), "status", c.Writer.Status(), "duration_ms", time.Since(start).Milliseconds())
	})
	r.Use(gin.CustomRecovery(func(c *gin.Context, _ any) { a.fail(c, errors.New("panic recovered")) }))
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		if err := a.Store.Pool.Ping(c.Request.Context()); err != nil {
			c.JSON(503, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})
	r.GET("/openapi.json", func(c *gin.Context) {
		body, err := web.Files.ReadFile("dist/openapi.json")
		if err != nil {
			c.Status(404)
			return
		}
		c.Data(200, "application/json", body)
	})
	api := r.Group("/api/v1", a.auth)
	api.GET("/session", func(c *gin.Context) { c.JSON(200, actor(c)) })
	api.GET("/summary", func(c *gin.Context) {
		data, err := a.Store.Summary(c.Request.Context(), actor(c).Tenant)
		a.respond(c, data, err)
	})
	api.GET("/stock", func(c *gin.Context) {
		data, err := a.Store.Stocks(c.Request.Context(), actor(c).Tenant)
		a.respond(c, gin.H{"data": data}, err)
	})
	api.GET("/reservations", func(c *gin.Context) {
		limit, offset, err := pagination(c)
		if err != nil {
			a.fail(c, err)
			return
		}
		data, total, err := a.Store.Reservations(c.Request.Context(), actor(c).Tenant, limit, offset)
		a.respond(c, gin.H{"data": data, "total": total, "limit": limit, "offset": offset}, err)
	})
	api.GET("/reservations/:id", func(c *gin.Context) {
		data, err := a.Store.Get(c.Request.Context(), actor(c).Tenant, c.Param("id"))
		a.respond(c, data, err)
	})
	api.GET("/reservations/:id/audit", func(c *gin.Context) {
		data, err := a.Store.Audits(c.Request.Context(), actor(c).Tenant, c.Param("id"))
		a.respond(c, gin.H{"data": data}, err)
	})
	api.POST("/reservations", func(c *gin.Context) {
		if !actor(c).CanWrite() {
			a.forbidden(c)
			return
		}
		var in domain.CreateRequest
		if err := decode(c, &in); err != nil {
			a.fail(c, err)
			return
		}
		data, replay, err := a.Store.Create(c.Request.Context(), actor(c), c.GetHeader("Idempotency-Key"), in)
		if err != nil {
			a.fail(c, err)
			return
		}
		code := 201
		if replay {
			code = 200
			c.Header("Idempotency-Replayed", "true")
		}
		c.JSON(code, data)
	})
	for _, transition := range []struct{ path, target string }{{"confirm", "confirmed"}, {"cancel", "cancelled"}} {
		target := transition.target
		api.POST("/reservations/:id/"+transition.path, func(c *gin.Context) {
			if !actor(c).CanWrite() {
				a.forbidden(c)
				return
			}
			var in struct {
				Version int `json:"version"`
			}
			if err := decode(c, &in); err != nil {
				a.fail(c, err)
				return
			}
			data, err := a.Store.Transition(c.Request.Context(), actor(c), c.Param("id"), target, in.Version)
			a.respond(c, data, err)
		})
	}
	api.GET("/events", func(c *gin.Context) {
		data, err := a.Store.Events(c.Request.Context(), actor(c).Tenant)
		a.respond(c, gin.H{"data": data}, err)
	})
	api.POST("/events/:id/retry", func(c *gin.Context) {
		if actor(c).Role != "manager" {
			a.forbidden(c)
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id < 1 {
			a.fail(c, domain.Invalid("イベントIDが不正です。"))
			return
		}
		if err = a.Store.Retry(c.Request.Context(), actor(c), id); err != nil {
			a.fail(c, err)
			return
		}
		c.JSON(200, gin.H{"status": "pending"})
	})
	api.GET("/metrics", func(c *gin.Context) {
		if actor(c).Role != "manager" {
			a.forbidden(c)
			return
		}
		summary, err := a.Store.Summary(c.Request.Context(), actor(c).Tenant)
		if err != nil {
			a.fail(c, err)
			return
		}
		c.Data(200, "text/plain; version=0.0.4", []byte(fmt.Sprintf("# TYPE stockflow_http_requests_total counter\nstockflow_http_requests_total %d\n# TYPE stockflow_http_failures_total counter\nstockflow_http_failures_total %d\n# TYPE stockflow_outbox_pending gauge\nstockflow_outbox_pending %d\n# TYPE stockflow_outbox_failed gauge\nstockflow_outbox_failed %d\n", a.Requests.Load(), a.Failures.Load(), summary.Pending, summary.Failed)))
	})
	assets, _ := fs.Sub(web.Files, "dist")
	r.GET("/", func(c *gin.Context) {
		body, err := web.Files.ReadFile("dist/index.html")
		if err != nil {
			a.fail(c, err)
			return
		}
		c.Data(200, "text/html; charset=utf-8", body)
	})
	r.GET("/assets/:file", func(c *gin.Context) {
		if strings.ContainsAny(c.Param("file"), "/\\") {
			c.Status(404)
			return
		}
		c.FileFromFS("assets/"+c.Param("file"), http.FS(assets))
	})
	r.NoRoute(func(c *gin.Context) {
		c.JSON(404, gin.H{"error": gin.H{"code": "not_found", "message": "URLが見つかりません。", "request_id": c.GetString("request_id")}})
	})
	return r
}
func actor(c *gin.Context) domain.Actor { return c.MustGet("actor").(domain.Actor) }
func (a *API) auth(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	value := c.GetHeader("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		a.fail(c, &domain.Fault{Status: 401, Code: "unauthorized", Message: "APIキーが必要です。"})
		c.Abort()
		return
	}
	token := strings.TrimPrefix(value, "Bearer ")
	digest := sha256.Sum256([]byte(token))
	for _, credential := range a.Credentials {
		if subtle.ConstantTimeCompare(digest[:], credential.Hash[:]) == 1 {
			c.Set("actor", credential.Actor)
			c.Next()
			return
		}
	}
	a.fail(c, &domain.Fault{Status: 401, Code: "unauthorized", Message: "APIキーを確認してください。"})
	c.Abort()
}
func (a *API) forbidden(c *gin.Context) {
	a.fail(c, &domain.Fault{Status: 403, Code: "forbidden", Message: "この役割には操作権限がありません。"})
}
func (a *API) respond(c *gin.Context, data any, err error) {
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(200, data)
}
func (a *API) fail(c *gin.Context, err error) {
	f := domain.AsFault(err)
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && (pgerr.Code == "57014" || pgerr.Code == "55P03" || pgerr.Code == "40P01" || pgerr.Code == "40001") {
		f = &domain.Fault{Status: 503, Code: "temporarily_unavailable", Message: "処理が混み合っています。しばらく待って再試行してください。"}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		f = &domain.Fault{Status: 503, Code: "temporarily_unavailable", Message: "処理を完了できませんでした。同じキーで再試行してください。"}
	}
	if f.Status >= 500 {
		a.Logger.Error("request_failed", "request_id", c.GetString("request_id"), "error", err)
	}
	c.JSON(f.Status, gin.H{"error": gin.H{"code": f.Code, "message": f.Message, "request_id": c.GetString("request_id")}})
}
func decode(c *gin.Context, dest any) error {
	mediaType, _, mediaErr := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		return &domain.Fault{Status: 415, Code: "unsupported_media_type", Message: "Content-Type: application/jsonを指定してください。"}
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return domain.Invalid("JSONの形式・型・項目名を確認してください。")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return domain.Invalid("JSONは1つのオブジェクトで送信してください。")
	}
	return nil
}
func pagination(c *gin.Context) (int, int, error) {
	limit, offset := 20, 0
	var err error
	if v, ok := c.GetQuery("limit"); ok {
		limit, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, domain.Invalid("limitが不正です。")
		}
	}
	if v, ok := c.GetQuery("offset"); ok {
		offset, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, domain.Invalid("offsetが不正です。")
		}
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return 0, 0, domain.Invalid("limitは1～100、offsetは0～100000で指定してください。")
	}
	return limit, offset, nil
}
