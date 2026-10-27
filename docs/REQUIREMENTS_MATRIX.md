# 求人要件と実装・証拠の対応

求人原文と取得範囲は[調査報告](RESEARCH.md)を参照。以下は技術面の対応であり、実務年数や全案件への応募資格を満たす認定ではない。

| 要件 | 実装・成果物 | 主な確認方法 | 判定範囲 |
|---|---|---|---|
| Go API開発 | cmd/stockflow、internal/httpapi、Gin | HTTP正常/異常/権限試験 | 実装・ローカル検証済み |
| RDB設計・SQL | migrations/001_init.sql、internal/store | 実PostgreSQL、制約・取引・複数接続競合 | 実装・検証済み |
| API契約 | web/openapi.json | JSON仕様と実応答、入力境界試験 | 実装・検証済み |
| テスト/品質 | *_test.go、scripts/check.sh | race、fuzz、gofmt、vet、依存監査 | 実施結果はVALIDATION参照 |
| Docker | Dockerfile、compose.yaml | clean build、起動、非root、DB永続化 | 実装・検証済み |
| React/TypeScript | web/src、package-lock.json | 型検査、応答世代/再送の試験、実画面 | 実装・検証済み |
| 非同期・運用 | worker、Outbox、ログ、/metrics | 再試行/失敗キュー/競合・終了の試験 | ローカル台帳まで |
| Git/CI | .github/workflows/ci.yml | ローカルで同等の検査 | GitHub上の実行は未実施 |
| 設計・レビュー | DESIGN、PRIMARY_REVIEW、COUNTER_REVIEW | 要件・反例・修正・再試験の追跡 | Codexの自己レビュー |
| AWS・IaC | deploy/aws、CLOUD.md | Terraform fmt/init/validate | 構成検査のみ、plan/apply/運用は未実施 |
| Go実務年数 | 該当なし | 既存の業務経歴でのみ判断 | この作品では充足しない |
| チーム開発・リード実績 | 該当なし | 実際の経歴・チーム活動で判断 | 作品から新しい実績を作らない |
| gRPC/Redis/Kafka/Kubernetes/Lambda | 今回の実装対象外 | 案件ごとに追加確認 | 要件がある案件では未充足 |

特に「Go経験3年以上」「AWS本番運用」「React実務経験」は、実装を同梱するだけでは経験要件を満たさない。応募時は実務と自主制作のURL・記述を分ける。
