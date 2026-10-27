# Go開発案件の要件調査

調査日：2026-10-27（日本時間）。対象は日本のフリーランス／業務委託バックエンド案件。Deep Research専用機能はこの環境では利用できないため、通常のWeb検索・求人原文の照合による調査を実施した。専用機能で生成したレポートではない。

## 結論と作品への反映

Go案件の技術要件は言語の文法だけではない。今回確認した求人では、API設計・実装、RDB/SQL、クラウド、コンテナ、自動テスト、Gitでの開発、設計・仕様調整が繰り返し現れた。フルスタック案件ではReact/TypeScript、特定案件ではGin/Echoも指定される。

このため、**Go + Gin + PostgreSQL + React/TypeScriptで、在庫引当・出荷確定を一連の業務として実装**した。単純なCRUDに留めず、同時更新、冪等性、期限、非同期処理、失敗後の再処理、認可とテストで設計上の判断を示す。

AWS/GCPの本番設計・運用経験、Goの実務年数、チームのレビュー経験は、作品の完成で置き換えられない。AWSはTerraformの構成例と検証記録を用意するが、クラウド上へ構築・運用した実績とは扱わない。

## 調査方法・読み方

目的に沿った事例抽出であり、無作為抽出や市場全体の統計調査ではない。独立求人の件数・掲載総数・単価平均・割合の推計は行わない。検索キャッシュしか取得できない情報は原文確認と区別し、環境欄の技術を勝手に必須スキルへ格上げしない。重複掲載の可能性がある案件を別々の市場需要として数えない。募集継続・契約条件は応募時に再確認が必要。

| ID | 掲載元・案件 | 確認できた必須要件の要約 | 尚可／環境との区別・確認範囲 |
|---|---|---|---|
| R1 | [FREE BRAIN：BtoB契約管理バックエンド](https://recruit-free-brain.com/recruits/14079752) | Go経験3年以上、GoでのAPI、JavaScript/TypeScript | SaaS初期開発・システム連携・PL/PMは尚可。本文を確認 |
| R2 | [Japan Technology：アカウント管理リプレイス](https://j-tech.co.jp/recruit/4927/) | Go2年以上、Java1年以上、要件定義 | AWSの知見は尚可。本文を確認。Java経験をこの作品で満たすとはしない |
| R3 | [PE-BANK：Go/AWSクラウドネイティブ開発](https://pe-bank.jp/project/sql/54319-G02/) | Web設計・開発、Go、クラウド開発知識、RDB/SQL | リーダー・サーバーレス・性能/セキュリティ等は歓迎。本文／掲載元検索結果を確認 |
| R4 | [TechSpace：公共系AWSサーバーレス開発リード](https://tech-space.jp/projects/56496/) | AWS上流・アーキテクト、リード参画、Lambda/Go等の指定経験 | 専門職の事例。全Go案件にLambdaを必須化しない。本文を確認 |
| R5 | [TechBiz：認証基盤、Go・PHP・AWS](https://techbiz.com/project/sp/detail/2287823) | 基本設計～テストまで一貫対応を求める案件 | Go/PostgreSQL/Docker/GitHub/AWS Lambdaが環境欄。取得本文では必須欄が省略されるため、必須の全条件は断定しない |
| R6 | [BizDev Tech：AIアバターのバックエンド](https://bizdev-tech.jp/job/backend-1791448368216x631361417754482200/) | Web5年以上、Go2年以上、クラウド、DB設計、テスト、レビュー等 | GraphQL/gRPC等は尚可。検索キャッシュで確認。直接本文取得は失敗 |
| R7 | [TechDirect：不動産TechのGo開発掲載](https://techdirect.jp/jobs?p=41&skills=Datadog%2CPostgreSQL%2CAWS+RDS) | Go3年以上、RDB、REST、React/TypeScript、Docker | IaC・マイクロサービス・CI/CD等は尚可。掲載一覧の検索キャッシュ。原文全体は取得不可 |
| R8 | [TechAdapt：Go案件の掲載一覧](https://www.techadapt.jp/search-result?language%5B%5D=5) | 例：静的型付き言語/他言語の経験、SQL、Git、クラウドに加えてGoへの関心を認める募集もある | Go実務年数を限定しない事例を確認。ただし一覧・検索キャッシュであり、現時点の紹介可否は未確認 |

検索は「Go フリーランス 必須スキル API SQL Docker」「Go PostgreSQL テスト」「Go 自己学習 バックエンド」等を組み合わせ、実務年数が厳しい案件と他言語からの移行を認める事例の両方を確認した。

参考として、[PE-BANKの金融系案件](https://pe-bank.jp/project/golang/53686-65/)は検索キャッシュに内容がある一方、直接開くと「ページがみつかりません」となるため、現行募集の根拠には採用しなかった。集約サイトの紹介数や単価をそのまま市場規模・期待収入には用いていない。

## 優先度と判断

| 優先度 | 技術・能力 | StockFlowで示すもの |
|---|---|---|
| A | GoによるWeb/API | Ginルーティング、厳密なJSON入力、共通エラー、HTTPステータス、OpenAPI |
| A | RDB・SQL・データ設計 | PostgreSQL、DDL、複合外部キー、制約、索引、行ロック、単一トランザクション |
| A | 自動テスト・品質 | 単体/HTTP/実DB競合試験、race detector、fuzz、gofmt、go vet、依存監査 |
| A | Docker・再現性 | 非rootコンテナ、Compose、DB永続化、ヘルスチェック、終了処理 |
| A | 要件整理・設計説明 | 状態遷移、API契約、業務不変条件、設計判断記録、レビュー修正記録 |
| B | React/TypeScript | 日本語操作画面、型検査、古い応答の除外、結果不明時の再送制御 |
| B | 運用・非同期処理 | Outbox、SKIP LOCKED、バックオフ、失敗キュー、JSONログ、メトリクス |
| B | クラウド・IaC | ECS/Fargate・プライベートALB・既存RDS接続・Secrets/CloudWatchの設計例。構成検査まで |
| 案件別 | gRPC、GraphQL、Redis、Kafka、Kubernetes、Lambda | この作品の実装対象外。要件がある案件では追加の学習・実装・経験が必要 |

A/Bは今回の作品で優先する実装順であり、求人市場における出現率の数値評価ではない。

## 応募時の使い方

既存のDB設計、業務整理、性能改善、PHP等の実務経歴と、自主制作で示せるGo API設計・品質の証拠を組み合わせる。R8のような他言語経験を評価する案件も検討し、Go実務年数を事実と異なる形で記入しない。作品URLは業務経歴URLと分けて掲載する。

## 技術判断の一次資料

- [Goリリース履歴](https://go.dev/doc/devel/release)：使用したGo 1.27系の確認
- [Go race detector](https://go.dev/doc/articles/race_detector)：並行アクセス検査の範囲
- [Gin公式ドキュメント](https://gin-gonic.com/en/docs/)：HTTPフレームワーク
- [pgx公式リポジトリ](https://github.com/jackc/pgx)：DBドライバー・プール
- [PostgreSQL 17の明示的ロック](https://www.postgresql.org/docs/17/explicit-locking.html)：行ロックと競合
- [HashiCorp AWS Provider](https://registry.terraform.io/providers/hashicorp/aws/latest)：IaCの構成検査

求人調査は2026-10-27の公開情報に基づく。採用・案件獲得の保証や、全Go案件の必須条件を満たすという認定ではない。
