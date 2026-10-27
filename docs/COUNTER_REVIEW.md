# Claude Code / Desktop等での反証レビュー手順

v1.0.0再審査版について、ユーザー提供のClaude反証レビュー報告書を受領済み。Claude側でソース精読・フロントエンド検証を実施したが、Goおよび実PostgreSQLでの反証試験は環境制約により未実施。以下は追加の実行レビューを依頼するための手順。

1. 配布ZIPを専用フォルダーへ展開し、README、DESIGN、REQUIREMENTS_MATRIX、PRIMARY_REVIEWを読む。
2. `docker compose --profile test run --build --rm tests`で実DB試験を実行。TEST_DATABASE_URL不足によるskipを成功扱いしない。
3. ロック待機で期限を越える確定、同一キーの異内容再送、別組織ID、viewer/operatorの権限、確定と取消の競合、複数worker、途中失敗のrollbackを反証する。
4. 画面の素早い利用者切替、結果不明時の再送、別受注の履歴応答の入れ替わり、モバイル表示を確認。
5. 指摘は「再現手順・期待・実際・影響・優先度・対象ファイル行」をまとめる。過去の報告の存在だけで修正済みと判断しない。
6. 生ログには実秘密や接続情報を含めない。テストは使い捨ての架空データDBで行う。Terraform apply、GitHubへの書込、外部システム送信は別の承認なしに行わない。

依頼文例：

「StockFlow Go Portfolio v1.0.0を反証レビューしてください。ソースの主張を信用せず、実PostgreSQLとHTTPで業務不変条件を破れるか確認してください。特に期限判定のタイミング、ロック順、idempotencyの組織・内容束縛、Outboxと台帳の原子性、認可、通信失敗時の画面を検証してください。実行していない試験は明示し、再現できた指摘と推測を区別して報告してください。まずコードの修正はせず結果をreports/へまとめてください。」
