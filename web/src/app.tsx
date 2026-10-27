import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { RequestEpoch, Submission } from "./request-state.mjs";
type Stock = {
  sku: string;
  name: string;
  category: string;
  on_hand: number;
  reserved: number;
  available: number;
  threshold: number;
};
type Item = { sku: string; quantity: number };
type Reservation = {
  id: string;
  reference: string;
  status: string;
  expires_at: string;
  created_at: string;
  version: number;
  items: Item[];
};
type Event = {
  id: number;
  reservation_id: string;
  type: string;
  status: string;
  attempts: number;
  last_error: string;
  created_at: string;
  next_attempt: string;
};
type Summary = {
  products: number;
  available: number;
  active: number;
  confirmed: number;
  pending: number;
  failed: number;
  delivered: number;
};
type Actor = { tenant: string; name: string; role: string };
type Audit = { id: number; action: string; actor: string; created_at: string };
const profiles = [
  { key: "demo-manager-a", label: "管理者 · 北関東ファクトリー" },
  { key: "demo-operator-a", label: "出荷担当 · 北関東ファクトリー" },
  { key: "demo-viewer-a", label: "参照担当 · 北関東ファクトリー" },
  { key: "demo-manager-b", label: "管理者 · 別組織デモ" },
];
const statusNames: Record<string, string> = {
  active: "引当中",
  confirmed: "出荷確定",
  cancelled: "取消済",
  expired: "期限切れ",
  pending: "処理待ち",
  delivered: "処理済",
  failed: "要確認",
};
const number = (value: number) => value.toLocaleString("ja-JP");
const date = (s: string) =>
  new Intl.DateTimeFormat("ja-JP", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(s));
class APIError extends Error {
  constructor(
    public status: number,
    message: string,
    public requestId = "",
  ) {
    super(message);
  }
}
async function request(
  path: string,
  key: string,
  options: RequestInit = {},
  signal?: AbortSignal,
) {
  const response = await fetch("/api/v1" + path, {
    ...options,
    signal: signal
      ? AbortSignal.any([signal, AbortSignal.timeout(12000)])
      : AbortSignal.timeout(12000),
    headers: {
      Authorization: "Bearer " + key,
      ...(options.body ? { "Content-Type": "application/json" } : {}),
      ...options.headers,
    },
  });
  const body = await response.json();
  if (!response.ok)
    throw new APIError(
      response.status,
      body.error?.message || "処理を完了できませんでした。",
      body.error?.request_id || "",
    );
  return body;
}
function Badge({ status }: { status: string }) {
  return (
    <span className={"badge " + status}>
      <span aria-hidden="true">●</span> {statusNames[status] || status}
    </span>
  );
}
function Icon({ name }: { name: string }) {
  const paths: Record<string, React.ReactNode> = {
    home: (
      <>
        <rect x="3" y="3" width="7" height="7" rx="1" />
        <rect x="14" y="3" width="7" height="7" rx="1" />
        <rect x="3" y="14" width="7" height="7" rx="1" />
        <rect x="14" y="14" width="7" height="7" rx="1" />
      </>
    ),
    box: (
      <>
        <path d="m12 3 9 5v9l-9 5-9-5V8l9-5Z M3 8l9 5 9-5 M12 13v9 M7 5.8l9 5" />
      </>
    ),
    list: (
      <>
        <rect x="4" y="3" width="16" height="18" rx="2" />
        <path d="M8 8h8M8 12h8M8 16h5" />
      </>
    ),
    pulse: (
      <>
        <path d="M2 12h5l3-8 4 16 3-8h5" />
      </>
    ),
    code: (
      <>
        <path d="m8 5-6 7 6 7m8-14 6 7-6 7m-3-16-2 20" />
      </>
    ),
    arrow: (
      <>
        <path d="M5 12h14m-6-6 6 6-6 6" />
      </>
    ),
  };
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      aria-hidden="true"
    >
      {paths[name] || paths.box}
    </svg>
  );
}
function App() {
  const [key, setKey] = useState("");
  const [page, setPage] = useState("home");
  const [actor, setActor] = useState<Actor | null>(null);
  const [stock, setStock] = useState<Stock[]>([]);
  const [reservations, setReservations] = useState<Reservation[]>([]);
  const [events, setEvents] = useState<Event[]>([]);
  const [summary, setSummary] = useState<Summary | null>(null);
  const [loading, setLoading] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [modal, setModal] = useState(false);
  const [detail, setDetail] = useState<Reservation | null>(null);
  const [audit, setAudit] = useState<Audit[]>([]);
  const [actionBusy, setActionBusy] = useState(false);
  const [filter, setFilter] = useState("all");
  const [offset, setOffset] = useState(0);
  const [total, setTotal] = useState(0);
  const [synced, setSynced] = useState<Date | null>(null);
  const epoch = useRef(new RequestEpoch());
  const auditEpoch = useRef(new RequestEpoch());
  const activeKey = useRef("");
  const loadController = useRef<AbortController | null>(null);
  const actionController = useRef<AbortController | null>(null);
  const pageRef = useRef(0);
  async function load(currentKey: string, currentOffset = pageRef.current) {
    loadController.current?.abort();
    const controller = new AbortController();
    loadController.current = controller;
    const ticket = epoch.current.next();
    setLoading(true);
    try {
      const [me, s, products, orders, queue] = await Promise.all([
        request("/session", currentKey, {}, controller.signal),
        request("/summary", currentKey, {}, controller.signal),
        request("/stock", currentKey, {}, controller.signal),
        request(
          "/reservations?limit=20&offset=" + currentOffset,
          currentKey,
          {},
          controller.signal,
        ),
        request("/events", currentKey, {}, controller.signal),
      ]);
      if (!epoch.current.current(ticket) || activeKey.current !== currentKey)
        return;
      setActor(me);
      setSummary(s);
      setStock(products.data);
      setReservations(orders.data);
      setTotal(orders.total);
      setEvents(queue.data);
      setSynced(new Date());
      setError("");
    } catch (e) {
      if (controller.signal.aborted || !epoch.current.current(ticket)) return;
      setError(e instanceof Error ? e.message : "読み込みに失敗しました。");
    } finally {
      if (epoch.current.current(ticket)) setLoading(false);
    }
  }
  function changeProfile(next: string) {
    auditEpoch.current.next();
    loadController.current?.abort();
    actionController.current?.abort();
    epoch.current.next();
    activeKey.current = next;
    setKey(next);
    setActor(null);
    setSummary(null);
    setStock([]);
    setReservations([]);
    setEvents([]);
    setModal(false);
    setDetail(null);
    setAudit([]);
    setNotice("");
    setError("");
    setActionBusy(false);
    setOffset(0);
    pageRef.current = 0;
  }
  useEffect(() => {
    if (!key) return;
    void load(key, 0);
    const timer = setInterval(() => void load(key), 5000);
    return () => {
      clearInterval(timer);
      loadController.current?.abort();
    };
  }, [key]);
  async function transition(r: Reservation, target: string) {
    if (actionBusy) return;
    const scope = key;
    const controller = new AbortController();
    actionController.current = controller;
    setActionBusy(true);
    setNotice("");
    setError("");
    try {
      const updated = await request(
        "/reservations/" + r.id + "/" + target,
        scope,
        { method: "POST", body: JSON.stringify({ version: r.version }) },
        controller.signal,
      );
      if (activeKey.current !== scope) return;
      setNotice(
        target === "confirm"
          ? "出荷を確定しました。在庫と履歴を更新しました。"
          : "引当を取り消し、在庫を解放しました。",
      );
      setDetail(updated);
      await load(scope);
      if (detail) await showDetail(updated);
    } catch (e) {
      if (activeKey.current !== scope) return;
      setError(
        e instanceof APIError
          ? e.message + (e.requestId ? "（ID: " + e.requestId + "）" : "")
          : "結果を確認できませんでした。一覧を更新して状態をご確認ください。",
      );
      await load(scope);
    } finally {
      if (activeKey.current === scope) setActionBusy(false);
    }
  }
  async function showDetail(r: Reservation) {
    const ticket = auditEpoch.current.next();
    const scope = key;
    setDetail(r);
    setAudit([]);
    try {
      const data = await request("/reservations/" + r.id + "/audit", scope);
      if (activeKey.current === scope && auditEpoch.current.current(ticket))
        setAudit(data.data);
    } catch {
      if (activeKey.current === scope)
        setError("操作履歴を読み込めませんでした。");
    }
  }
  async function retryEvent(event: Event) {
    const scope = key;
    try {
      await request("/events/" + event.id + "/retry", scope, {
        method: "POST",
      });
      if (activeKey.current === scope) {
        setNotice("再処理キューへ戻しました。");
        await load(scope);
      }
    } catch (e) {
      if (activeKey.current === scope)
        setError(e instanceof Error ? e.message : "再処理に失敗しました。");
    }
  }
  function paginate(next: number) {
    setOffset(next);
    pageRef.current = next;
    void load(key, next);
  }
  const write = actor?.role === "manager" || actor?.role === "operator";
  const shown = reservations.filter(
    (r) => filter === "all" || r.status === filter,
  );
  const inventoryLow = stock.filter((s) => s.available <= s.threshold).length;
  if (!key)
    return (
      <div className="entry">
        <div className="entry-card">
          <div className="brand entry-brand">
            <div className="brandmark">sf</div>
            <span>
              StockFlow<small>INVENTORY OPERATIONS</small>
            </span>
          </div>
          <span className="eyebrow">GO / POSTGRESQL PORTFOLIO</span>
          <h1>
            在庫から出荷まで、
            <br />
            確かな一つの流れに。
          </h1>
          <p className="muted">
            在庫引当・出荷確定・非同期イベントをつなぐ
            <br />
            日本語の業務ワークスペース。
          </p>
          <div className="profile-list">
            {profiles.map((p, i) => (
              <button
                key={p.key}
                className={i === 0 ? "profile primary-profile" : "profile"}
                onClick={() => changeProfile(p.key)}
              >
                <span>
                  <strong>{p.label.split(" · ")[0]}</strong>
                  <small>{p.label.split(" · ")[1]}</small>
                </span>
                <Icon name="arrow" />
              </button>
            ))}
          </div>
          <p className="local-note">
            架空データを使うローカルデモです。
            <br />
            実際の出荷・外部システムへの送信は行いません。
          </p>
        </div>
        <div className="entry-visual">
          <span className="wire one" />
          <span className="wire two" />
          <div className="visual-box">
            <Icon name="box" />
            <span>RESERVE → CONFIRM → DELIVER</span>
          </div>
          <p>一度の操作を、確かな記録に。</p>
          <small>Go · Gin · PostgreSQL · React · Docker</small>
        </div>
      </div>
    );
  const titles: Record<string, string> = {
    home: "オペレーション概要",
    reservations: "引当・出荷管理",
    inventory: "在庫一覧",
    events: "イベント処理",
    api: "API・設計ガイド",
  };
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brandmark">sf</div>
          <span>
            StockFlow<small>在庫・出荷ワークスペース</small>
          </span>
        </div>
        <div className="workspace-label">WORKSPACE</div>
        <nav aria-label="メインメニュー">
          {[
            ["home", "home", "ダッシュボード"],
            ["reservations", "list", "引当・出荷"],
            ["inventory", "box", "在庫一覧"],
            ["events", "pulse", "イベント処理"],
            ["api", "code", "API・設計"],
          ].map(([id, icon, label]) => (
            <button
              key={id}
              className={page === id ? "nav active" : "nav"}
              onClick={() => {
                setPage(id);
                setNotice("");
              }}
            >
              <Icon name={icon} />
              <span>{label}</span>
              {id === "events" && summary && summary.failed > 0 && (
                <span className="nav-count">{summary.failed}</span>
              )}
            </button>
          ))}
        </nav>
        <div className="sidebar-footer">
          <span className="live-dot" /> ローカルデモ環境
          <p>
            Go × PostgreSQL
            <br />
            StockFlow v1.0.0
          </p>
        </div>
      </aside>
      <main>
        <header className="topbar">
          <div>
            <span className="workspace-dot" />{" "}
            {actor?.tenant === "factory-b"
              ? "別組織デモ"
              : "北関東ファクトリー"}{" "}
            <span className="slash">/</span> オペレーション
          </div>
          <select
            aria-label="デモ利用者を切り替え"
            value={key}
            onChange={(e) => changeProfile(e.target.value)}
          >
            {profiles.map((p) => (
              <option key={p.key} value={p.key}>
                {p.label}
              </option>
            ))}
          </select>
        </header>
        <div className="content">
          <div className="page-heading">
            <div>
              <span className="eyebrow">
                {page === "home"
                  ? "INVENTORY / OVERVIEW"
                  : "WORKSPACE / " + page.toUpperCase()}
              </span>
              <h1>{titles[page]}</h1>
              <p className="muted">
                {page === "home"
                  ? "在庫と出荷のいまを把握し、次の作業へ。"
                  : page === "events"
                    ? "成功した業務操作を、確実に次の処理へつなぎます。"
                    : "業務の状態と記録を、ひとつの画面で確認できます。"}
              </p>
            </div>
            <div className="heading-actions">
              <button
                className="button ghost"
                onClick={() => void load(key)}
                disabled={loading}
              >
                {loading ? "更新中…" : "↻ 更新"}
              </button>
              {write && page !== "api" && (
                <button
                  className="button primary"
                  onClick={() => setModal(true)}
                >
                  ＋ 在庫を引き当てる
                </button>
              )}
            </div>
          </div>
          {error && (
            <div role="alert" className="alert error">
              {error}
            </div>
          )}
          {notice && (
            <div role="status" className="alert success">
              {notice}
            </div>
          )}
          {!actor && (
            <div className="panel empty">データを読み込んでいます…</div>
          )}
          {actor && page === "home" && (
            <>
              <div className="kpis">
                <KPI
                  label="引当可能な在庫"
                  value={summary?.available || 0}
                  unit="点"
                  sub={`${summary?.products || 0} SKU / 引当済みを除く`}
                  icon="box"
                />
                <KPI
                  label="引当中の受注"
                  value={summary?.active || 0}
                  unit="件"
                  sub="出荷確定または取消を待機中"
                  icon="list"
                />
                <KPI
                  label="出荷確定"
                  value={summary?.confirmed || 0}
                  unit="件"
                  sub="確定済み受注の累計"
                  icon="arrow"
                />
                <KPI
                  label="在庫アラート"
                  value={inventoryLow}
                  unit="SKU"
                  sub="引当可能数が基準以下"
                  icon="pulse"
                  caution={inventoryLow > 0}
                />
              </div>
              <div className="dashboard-grid">
                <section className="panel stock-panel">
                  <div className="panel-head">
                    <div>
                      <h2>在庫の状況</h2>
                      <p className="muted">引当可能数 / 手持在庫</p>
                    </div>
                    <button
                      className="text-button"
                      onClick={() => setPage("inventory")}
                    >
                      在庫一覧へ →
                    </button>
                  </div>
                  {stock.length === 0 ? (
                    <Empty text="この組織にはまだ在庫データがありません。" />
                  ) : (
                    stock.map((s) => (
                      <div className="stock-line" key={s.sku}>
                        <div className="stock-icon">
                          <Icon name="box" />
                        </div>
                        <div className="stock-info">
                          <strong>{s.name}</strong>
                          <small>
                            {s.sku} · {s.category}
                          </small>
                          <progress
                            max={Math.max(s.on_hand, 1)}
                            value={s.available}
                            aria-label={s.name + "の引当可能率"}
                          />
                        </div>
                        <div className="stock-quantity">
                          <strong>
                            {number(s.available)}
                            <small> / {number(s.on_hand)}</small>
                          </strong>
                          <span
                            className={
                              s.available <= s.threshold
                                ? "warning-text"
                                : "muted"
                            }
                          >
                            {s.available <= s.threshold ? "在庫少" : "引当可能"}
                          </span>
                        </div>
                      </div>
                    ))
                  )}
                </section>
                <section className="panel event-summary">
                  <span className="eyebrow">BACKGROUND OPERATIONS</span>
                  <h2>処理の流れも、見える。</h2>
                  <p className="muted">
                    業務更新とイベントを同時に記録。
                    <br />
                    失敗時は待機・再試行へ進みます。
                  </p>
                  <div className="queue-stat">
                    <span>
                      <i className="dot pending" /> 処理待ち
                    </span>
                    <strong>{summary?.pending || 0}</strong>
                  </div>
                  <div className="queue-stat">
                    <span>
                      <i className="dot delivered" /> 処理済み
                    </span>
                    <strong>{summary?.delivered || 0}</strong>
                  </div>
                  <div className="queue-stat">
                    <span>
                      <i className="dot failed" /> 要確認
                    </span>
                    <strong>{summary?.failed || 0}</strong>
                  </div>
                  <button
                    className="text-button"
                    onClick={() => setPage("events")}
                  >
                    イベントを確認する →
                  </button>
                  <small className="auto-update">
                    <span className="live-dot" /> 約5秒ごとに更新
                  </small>
                </section>
              </div>
              <ReservationTable
                rows={reservations.slice(0, 5)}
                onDetail={(r) => void showDetail(r)}
                title="最近の引当・出荷"
                extra={
                  <button
                    className="text-button"
                    onClick={() => setPage("reservations")}
                  >
                    すべて見る →
                  </button>
                }
              />
              <div className="steps">
                {[
                  ["01", "在庫を引き当てる", "SKUと数量を選び、在庫を確保。"],
                  ["02", "出荷を確定する", "引当を消化し、手持在庫を更新。"],
                  ["03", "処理履歴を確認する", "イベントと操作履歴を追跡。"],
                ].map(([n, t, d]) => (
                  <div key={n}>
                    <span>{n}</span>
                    <strong>{t}</strong>
                    <small>{d}</small>
                  </div>
                ))}
              </div>
            </>
          )}
          {actor && page === "inventory" && (
            <section className="panel">
              <div className="panel-head">
                <div>
                  <h2>在庫マスター</h2>
                  <p className="muted">
                    引当可能 = 手持在庫 −
                    引当済み。在庫は組織ごとに分離しています。
                  </p>
                </div>
                <span className="subtle-count">{stock.length} SKU</span>
              </div>
              <div className="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>SKU / 商品名</th>
                      <th>カテゴリ</th>
                      <th className="numeric">手持在庫</th>
                      <th className="numeric">引当済み</th>
                      <th className="numeric">引当可能</th>
                      <th>状態</th>
                    </tr>
                  </thead>
                  <tbody>
                    {stock.map((s) => (
                      <tr key={s.sku}>
                        <td>
                          <strong className="sku">{s.sku}</strong>
                          <small>{s.name}</small>
                        </td>
                        <td>{s.category}</td>
                        <td className="numeric">{number(s.on_hand)}</td>
                        <td className="numeric">{number(s.reserved)}</td>
                        <td className="numeric strong">
                          {number(s.available)}
                        </td>
                        <td>
                          <span
                            className={
                              "badge " +
                              (s.available <= s.threshold ? "low" : "delivered")
                            }
                          >
                            {s.available <= s.threshold ? "在庫少" : "正常"}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {stock.length === 0 && (
                  <Empty text="この組織には在庫データがありません。" />
                )}
              </div>
            </section>
          )}
          {actor && page === "reservations" && (
            <>
              <div className="filter-bar">
                <label>
                  状態{" "}
                  <select
                    value={filter}
                    onChange={(e) => setFilter(e.target.value)}
                  >
                    {[
                      ["all", "すべて"],
                      ["active", "引当中"],
                      ["confirmed", "出荷確定"],
                      ["cancelled", "取消済"],
                      ["expired", "期限切れ"],
                    ].map(([v, t]) => (
                      <option key={v} value={v}>
                        {t}
                      </option>
                    ))}
                  </select>
                </label>
                <span className="muted">
                  現在のページ内を絞り込み / 全{total}件
                </span>
              </div>
              <ReservationTable
                rows={shown}
                onDetail={(r) => void showDetail(r)}
                title="引当・出荷一覧"
              />
              <div className="pagination">
                <button
                  className="button ghost"
                  disabled={offset === 0 || loading}
                  onClick={() => paginate(Math.max(0, offset - 20))}
                >
                  ← 前へ
                </button>
                <span>
                  {total === 0 ? 0 : offset + 1}–{Math.min(offset + 20, total)}{" "}
                  / {total}
                </span>
                <button
                  className="button ghost"
                  disabled={offset + 20 >= total || loading}
                  onClick={() => paginate(offset + 20)}
                >
                  次へ →
                </button>
              </div>
            </>
          )}
          {actor && page === "events" && (
            <section className="panel">
              <div className="panel-head">
                <div>
                  <h2>非同期イベント</h2>
                  <p className="muted">
                    直近100件。処理済みはローカル配送台帳への記録完了を示します。
                  </p>
                </div>
                <span className="subtle-count">{events.length} EVENTS</span>
              </div>
              <div className="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>イベント / 発生日時</th>
                      <th>受注ID</th>
                      <th>状態</th>
                      <th>試行</th>
                      <th>補足・操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {events.map((e) => (
                      <tr key={e.id}>
                        <td>
                          <strong>
                            #{e.id} · {e.type}
                          </strong>
                          <small>{date(e.created_at)}</small>
                        </td>
                        <td>
                          <code>{e.reservation_id.slice(0, 12)}</code>
                        </td>
                        <td>
                          <Badge status={e.status} />
                        </td>
                        <td>{e.attempts}回</td>
                        <td>
                          {e.last_error && (
                            <small className="warning-text">
                              {e.last_error}
                            </small>
                          )}
                          {e.status === "failed" && actor.role === "manager" ? (
                            <button
                              className="text-button"
                              onClick={() => void retryEvent(e)}
                            >
                              再処理する →
                            </button>
                          ) : (
                            <span className="muted">
                              {e.status === "pending"
                                ? "次回 " + date(e.next_attempt)
                                : e.status === "delivered"
                                  ? "記録完了"
                                  : "管理者へ確認"}
                            </span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {events.length === 0 && (
                  <Empty text="この組織にはイベントがありません。" />
                )}
              </div>
            </section>
          )}
          {page === "api" && (
            <section className="panel guide">
              <span className="eyebrow">API FIRST / QUALITY BUILT IN</span>
              <h2>画面の裏側まで、説明できる設計。</h2>
              <p>
                Go + GinのREST
                API、PostgreSQLの行ロック、トランザクショナルOutboxを使い、在庫と記録の一貫性を保ちます。
              </p>
              <a
                className="button primary"
                href="/openapi.json"
                target="_blank"
                rel="noreferrer"
              >
                OpenAPI仕様を開く ↗
              </a>
              <div className="guide-grid">
                {[
                  [
                    "安全な在庫引当",
                    "SKU順で行をロック。数量の不足時は全明細をロールバックします。",
                  ],
                  [
                    "二重送信に対応",
                    "Idempotency-Keyと正規化した内容のハッシュを照合。同じ要求は同じ受注へ戻ります。",
                  ],
                  [
                    "状態遷移と期限",
                    "引当中から確定・取消・期限切れへ。versionで古い画面からの更新を防ぎます。",
                  ],
                  [
                    "失敗を見える状態へ",
                    "非同期処理は最大5回。バックオフと要確認キューを用い、管理者が再処理できます。",
                  ],
                ].map(([t, d]) => (
                  <article key={t}>
                    <h3>{t}</h3>
                    <p>{d}</p>
                  </article>
                ))}
              </div>
              <p className="local-note">
                デモ用の固定APIキーで利用者を切り替えています。本番の利用者管理、外部配送・決済連携、クラウド稼働実績は含みません。
              </p>
            </section>
          )}
          <footer className="page-footer">
            <span>
              架空データを使う自主制作ポートフォリオ · 実際の出荷は行いません
            </span>
            <span>
              {synced
                ? "最終取得 " + synced.toLocaleTimeString("ja-JP")
                : "接続確認中"}
            </span>
          </footer>
        </div>
      </main>
      {modal && (
        <CreateModal
          stock={stock}
          authKey={key}
          onClose={() => setModal(false)}
          onSuccess={() => {
            setModal(false);
            setNotice("在庫を引き当てました。受注一覧で出荷確定へ進めます。");
            void load(key);
          }}
        />
      )}
      {detail && (
        <div className="modal-backdrop">
          <section
            className="modal detail-modal"
            role="dialog"
            aria-modal="true"
            aria-label="引当詳細"
          >
            <div className="modal-head">
              <div>
                <span className="eyebrow">RESERVATION DETAIL</span>
                <h2>{detail.reference}</h2>
              </div>
              <button
                className="close"
                aria-label="詳細を閉じる"
                disabled={actionBusy}
                onClick={() => {
                  auditEpoch.current.next();
                  setDetail(null);
                }}
              >
                ×
              </button>
            </div>
            <div className="detail-meta">
              <Badge status={detail.status} />
              <span>version {detail.version}</span>
              <span>期限 {date(detail.expires_at)}</span>
            </div>
            <code className="detail-id">{detail.id}</code>
            <h3>引当明細</h3>
            <div className="item-list">
              {detail.items.map((i) => (
                <div key={i.sku}>
                  <span>
                    {stock.find((s) => s.sku === i.sku)?.name || i.sku}
                    <small>{i.sku}</small>
                  </span>
                  <strong>{number(i.quantity)} 点</strong>
                </div>
              ))}
            </div>
            {write && detail.status === "active" && (
              <div className="detail-actions">
                <button
                  className="button ghost"
                  disabled={actionBusy}
                  onClick={() => void transition(detail, "cancel")}
                >
                  引当を取り消す
                </button>
                <button
                  className="button primary"
                  disabled={actionBusy}
                  onClick={() => void transition(detail, "confirm")}
                >
                  {actionBusy ? "処理中…" : "出荷を確定する →"}
                </button>
              </div>
            )}
            <h3>操作履歴</h3>
            <div className="audit-list">
              {audit.map((a) => (
                <div key={a.id}>
                  <span className="live-dot" />
                  <span>
                    <strong>{a.action}</strong>
                    <small>
                      {a.actor} · {date(a.created_at)}
                    </small>
                  </span>
                </div>
              ))}
              {audit.length === 0 && (
                <p className="muted">履歴を読み込んでいます…</p>
              )}
            </div>
            <p className="local-note">
              この画面の内容は開いた時点の状態です。期限切れや他の操作と競合した場合、サーバーが再検証します。
            </p>
          </section>
        </div>
      )}
    </div>
  );
}
function KPI({
  label,
  value,
  unit,
  sub,
  icon,
  caution = false,
}: {
  label: string;
  value: number;
  unit: string;
  sub: string;
  icon: string;
  caution?: boolean;
}) {
  return (
    <section className={"kpi " + (caution ? "caution" : "")}>
      <div className="kpi-label">
        {label}
        <Icon name={icon} />
      </div>
      <strong>
        {number(value)}
        <span>{unit}</span>
      </strong>
      <small>{sub}</small>
    </section>
  );
}
function Empty({ text }: { text: string }) {
  return (
    <div className="empty">
      <Icon name="box" />
      <p>{text}</p>
    </div>
  );
}
function ReservationTable({
  rows,
  onDetail,
  title,
  extra,
}: {
  rows: Reservation[];
  onDetail: (r: Reservation) => void;
  title: string;
  extra?: React.ReactNode;
}) {
  return (
    <section className="panel">
      <div className="panel-head">
        <div>
          <h2>{title}</h2>
          <p className="muted">状態を確認し、受注の詳細へ進みます。</p>
        </div>
        {extra}
      </div>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>受注参照 / ID</th>
              <th>明細</th>
              <th>引当期限</th>
              <th>状態</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id}>
                <td>
                  <button
                    className="text-button order-link"
                    onClick={() => onDetail(r)}
                  >
                    {r.reference}
                  </button>
                  <small>{r.id.slice(0, 12)}</small>
                </td>
                <td>
                  {r.items.length} SKU ·{" "}
                  {number(r.items.reduce((n, i) => n + i.quantity, 0))}点
                </td>
                <td>{date(r.expires_at)}</td>
                <td>
                  <Badge status={r.status} />
                </td>
                <td>
                  <button className="text-button" onClick={() => onDetail(r)}>
                    詳細 →
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {rows.length === 0 && (
          <Empty text="該当する引当・出荷データがありません。" />
        )}
      </div>
    </section>
  );
}
function CreateModal({
  stock,
  authKey,
  onClose,
  onSuccess,
}: {
  stock: Stock[];
  authKey: string;
  onClose: () => void;
  onSuccess: () => void;
}) {
  const [reference, setReference] = useState("");
  const [ttl, setTTL] = useState(3600);
  const [items, setItems] = useState<Item[]>([
    { sku: stock[0]?.sku || "", quantity: 1 },
  ]);
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [error, setError] = useState("");
  const submission = useRef(new Submission());
  const controller = useRef<AbortController | null>(null);
  const mounted = useRef(true);
  useEffect(
    () => () => {
      mounted.current = false;
      controller.current?.abort();
    },
    [],
  );
  const changeItem = (index: number, patch: Partial<Item>) =>
    setItems(items.map((v, i) => (i === index ? { ...v, ...patch } : v)));
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    const attempt = submission.current.prepare(
      { reference, ttl_seconds: ttl, items },
      crypto.randomUUID(),
    );
    const abort = new AbortController();
    controller.current = abort;
    const timeout = setTimeout(() => abort.abort(), 10000);
    try {
      await request(
        "/reservations",
        authKey,
        {
          method: "POST",
          headers: { "Idempotency-Key": attempt.key },
          body: JSON.stringify(attempt.payload),
        },
        abort.signal,
      );
      if (mounted.current) {
        submission.current.clear();
        onSuccess();
      }
    } catch (e) {
      if (!mounted.current) return;
      if (e instanceof APIError && e.status < 500) {
        setUncertain(false);
        submission.current.clear();
        setError(
          e.message + (e.requestId ? "（ID: " + e.requestId + "）" : ""),
        );
      } else {
        setUncertain(true);
        setError(
          "結果を確認できませんでした。内容とキーを保持しています。「同じ内容で再送」で二重登録を防いで再確認できます。",
        );
      }
    } finally {
      clearTimeout(timeout);
      if (mounted.current) setBusy(false);
    }
  }
  return (
    <div className="modal-backdrop">
      <section
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label="在庫の引当を作成"
      >
        <div className="modal-head">
          <div>
            <span className="eyebrow">NEW RESERVATION</span>
            <h2>在庫を引き当てる</h2>
          </div>
          <button
            className="close"
            aria-label="作成画面を閉じる"
            onClick={onClose}
            disabled={busy}
          >
            ×
          </button>
        </div>
        <p className="muted">
          受注参照と明細を入力し、出荷までの在庫を確保します。
        </p>
        {error && (
          <div role="alert" className="alert error">
            {error}
          </div>
        )}
        <form onSubmit={submit}>
          <fieldset disabled={busy || uncertain}>
            <label className="field">
              受注参照{" "}
              <input
                autoFocus
                required
                maxLength={64}
                value={reference}
                onChange={(e) => setReference(e.target.value)}
                placeholder="例：受注 SF-1027-004"
              />
            </label>
            <label className="field">
              引当期限{" "}
              <select
                value={ttl}
                onChange={(e) => setTTL(Number(e.target.value))}
              >
                <option value={30}>30秒（期限切れ確認用）</option>
                <option value={3600}>1時間</option>
                <option value={86400}>24時間</option>
              </select>
            </label>
            <h3>明細</h3>
            {items.map((item, index) => (
              <div className="form-item" key={index}>
                <label>
                  商品
                  <select
                    required
                    aria-label={"商品 " + (index + 1)}
                    value={item.sku}
                    onChange={(e) => changeItem(index, { sku: e.target.value })}
                  >
                    {stock.map((s) => (
                      <option key={s.sku} value={s.sku}>
                        {s.name}（可能数 {s.available}）
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  数量
                  <input
                    type="number"
                    aria-label={"数量 " + (index + 1)}
                    min={1}
                    max={10000}
                    required
                    value={item.quantity}
                    onChange={(e) =>
                      changeItem(index, { quantity: Number(e.target.value) })
                    }
                  />
                </label>
                {items.length > 1 && (
                  <button
                    type="button"
                    className="remove-item"
                    aria-label={"明細 " + (index + 1) + " を削除"}
                    onClick={() =>
                      setItems(items.filter((_, i) => i !== index))
                    }
                  >
                    ×
                  </button>
                )}
              </div>
            ))}
            <button
              type="button"
              className="text-button add-item"
              disabled={items.length >= 10}
              onClick={() =>
                setItems([
                  ...items,
                  {
                    sku:
                      stock.find((s) => !items.some((i) => i.sku === s.sku))
                        ?.sku ||
                      stock[0]?.sku ||
                      "",
                    quantity: 1,
                  },
                ])
              }
            >
              ＋ 明細を追加
            </button>
          </fieldset>
          <div className="form-info">
            同じ商品の明細は1行にまとめてください。在庫不足の明細があれば、すべての明細を登録せずに戻します。
          </div>
          <div className="modal-actions">
            <button
              type="button"
              className="button ghost"
              disabled={busy}
              onClick={onClose}
            >
              閉じる
            </button>
            <button
              type="submit"
              className="button primary"
              disabled={busy || stock.length === 0}
            >
              {busy
                ? "処理中…"
                : uncertain
                  ? "同じ内容で再送 →"
                  : "引当を作成する →"}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}
createRoot(document.getElementById("root")!).render(<App />);
