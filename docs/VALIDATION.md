# StockFlow v1.0.1 最新検証記録

検査日: 2026-10-27。Go1.27.3 / PostgreSQL17 / Docker arm64。

| 項目 | 結果 |
| --- | --- |
| Go | トップレベル42、サブテスト込み66成功、失敗0・テストskip 0 |
| 競合・静的検査 | -race、gofmt、go vet、go mod verify成功 |
| 対象回帰 | health URL、health HTTP、期限切れ再送、旧PR-01、旧PR-02を各5回成功 |
| 文カバレッジ | 今回71.7%。以前の数値との因果関係は断定しない |
| 画面ビルド | TypeScript・本番ビルド・3件のテスト成功 |
| 依存監査 | npm audit 0件、govulncheck到達可能0件・モジュール注意情報1件（未使用openpgp） |
| 実行 | Dockerビルド、9090指定のhealth、readyz、OpenAPI、正常終了を確認 |

[今回の一次審査](PRIMARY_REVIEW.md)、[今回のログ](evidence/v1_0_1/)、[旧版検証履歴](history/v1_0_0_VALIDATION.md)を参照。

再実行:

```sh
docker compose --profile test run --build --rm tests
```

上記は全体検査を行うコマンドです。今回の対象5回反復コマンドとJSON保存コマンドはgo.logに記録しています。DBなしでの統合試験skipを成功扱いしないでください。
