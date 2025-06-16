# Clampany

## 概要
Clampanyは、複数のAI・人間ロールが協調してプロジェクトを推進するためのCLIツールです。tmux上で各ロール（ceo/pm/planner/engineer）が独立したペインで動作し、コマンドや仕様書を通じてタスクを分担・連携します。

## 特徴
- CEO/PM/Planner/Engineerなどのロール分担によるプロジェクト推進
- 指示・仕様・実装・レビューの流れをCLIコマンドで自動化
- tmuxペインを活用したマルチロール同時実行
- 仕様書・コンテキスト・タスクキューの自動管理

## ロール一覧
| ロール       | 説明                                                                 |
|--------------|----------------------------------------------------------------------|
| `ceo`        | ビジョンの定義者。プロジェクト全体の目的・意義を明確化し、方向性を提示します。 |
| `pm`         | プロジェクトマネージャー。CEOの意図を受けて、目的を具体的なタスクへと分解します。 |
| `planner`    | プランナー。タスクを元に詳細な仕様書や要件に変換します。                     |
| `engineer`   | エンジニア。仕様を基に実際のコード・システムを構築します。                  |

## ディレクトリ構成
```
clampany/
├── main.go                # エントリポイント
├── go.mod, go.sum         # Goモジュール・依存管理
├── cmd/                   # CLIコマンド群
│   ├── root.go            # メインコマンド
│   ├── inqueue.go         # 指示キューコマンド
│   ├── send.go            # tmuxペイン送信コマンド
│   └── instructions/      # ロールごとの指示・ルール
├── internal/              # 内部ロジック
│   ├── models.go          # ロール・タスク定義
│   ├── scheduler/         # タスクスケジューラ
│   ├── executor/          # 実行エンジン
│   ├── loader/            # ローダー
│   └── util/              # ユーティリティ
├── .gitignore             # Git管理除外
└── README.md              # 本ファイル
```

## インストール・ビルド
Go 1.21以上が必要です。
```sh
git clone <このリポジトリ>
cd clampany
go build -o clampany main.go
```

## 使い方
### 初期化
```sh
./clampany init
```
`_clampany/instructions`ディレクトリが作成され、ロールごとの指示ファイルがコピーされます。

### ワーカーモード起動（tmux上で実行推奨）
```sh
tmux
./clampany
```
各ロールごとにペインが自動生成され、永続ワーカーとして起動します。

### 指示の送信
- ロール間の指示は`inqueue`コマンドで行います。
```sh
./clampany inqueue planner "○○の仕様化"
```
- 直接ペインにプロンプトを送る場合は`send`コマンドを利用します。
```sh
./clampany send --role planner --prompt "新しい仕様を確認してください"
```

## 主要コマンド
- `init` : 必要なディレクトリ・指示ファイルを初期化
- `inqueue <role> <message>` : 指定ロールのキューに指示を追加
- `send --role <role> --prompt <text>` : 指定ロールのtmuxペインに直接送信

## 運用ルール
- 指示・応答は必ず一行コマンド形式で返すこと
- 不要な会話・挨拶・確認は一切禁止
- 仕様書は`/_clampany/specification`に保存し、plannerのみが編集可能
- プロジェクトの目的や意図は`/_clampany/context`に保存
- 指示待ち状態では`[READY]`のみ出力

## 依存パッケージ
- github.com/spf13/cobra
- github.com/google/uuid
- gopkg.in/yaml.v3

## .gitignore
- `run` ディレクトリ（実行時生成物）
- `clampany` バイナリ

## ライセンス
本プロジェクトはMITライセンスです。

---

### 参考: ロールごとの指示例
#### CEO
```
./clampany inqueue pm "ユーザーが時間を有効に活用し、ストレスを軽減するサービスを開発する。..."
```
#### PM
```
./clampany inqueue planner "○○の仕様を作成し、エンジニアへのタスクを作成してください。"
```
#### Planner
```
./clampany inqueue engineer "/_clampany/specification/xxx.md の仕様に基づき、機能を実装してください"
```
#### Engineer
```
./clampany inqueue planner "xxxに関する仕様が不足しているので、詳細な仕様を提供してください。"
```
