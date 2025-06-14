# Clampany

AI/人間/シェルの役割を組み合わせてタスクをDAGで自動実行するGo製CLI。

## 特徴
- 役割（Role）とタスク（Task）をYAMLで定義
- 依存関係をDAGで解決し並列実行
- Claude APIまたはローカル`claude`コマンドでAI実行
- 各ロールのタスクはtmuxペインで進捗・出力を描画（tmuxがなければ通常実行）
- 実行ログ・出力をrun-<uuid>/outputs/に保存
- clarification flow（未定義検出→自動質問）対応

## tmux連携について
Clampanyはtmuxセッション内で実行すると、各ロールのタスクごとに新しいペインを自動で開き、AIや人間・シェルの出力・進捗をリアルタイムで描画します。

- tmuxがインストールされていない場合は従来通り標準出力/入力で実行されます
- ペインは自動で分割され、終了時にEnterで閉じられます

## Claudeコマンド連携例
roles.yamlで`model: claude-3-opus`などと指定すると、APIではなくローカルの`claude`コマンドでAIを実行します。

```yaml
roles:
  - name: engineer1
    type: ai
    model: claude-3-opus
    behavior: "Write code."
```

この場合、タスク実行時に以下のようなコマンドが呼ばれます：

```sh
claude --model claude-3-opus --prompt "プロンプト内容"
```

## テスト例: hello world / fizz buzz
`tasks.yaml`を以下のように設定し、AIにGoコードを自動生成させることができます。

```yaml
tasks:
  - name: hello_world
    role: engineer1
    prompt: |
      "hello world"を出力するGoプログラムを作成してください。
    depends_on: []

  - name: fizz_buzz
    role: engineer1
    prompt: |
      1から100までの数を出力し、3の倍数はFizz、5の倍数はBuzz、両方はFizzBuzzと出力するGoプログラムを作成してください。
    depends_on: []
```

## ディレクトリ構成
```
clampany/
├── cmd/                # CLIコマンド
├── internal/           # ロジック
│   ├── loader/         # YAMLローダー
│   ├── dag/            # DAG構築
│   ├── executor/       # Executor実装
│   ├── scheduler/      # スケジューラ
│   ├── util/           # ユーティリティ
│   └── models.go       # モデル定義
├── roles.yaml          # サンプル役割定義
├── tasks.yaml          # サンプルタスク定義
├── go.mod
└── main.go
```

## ビルド
```bash
go build -o clampany .
```

## 実行例
```bash
tmux new-session -s clampany
./clampany apply -f tasks.yaml -r roles.yaml --max-parallel 2
```

## 主要コマンド
- `clampany apply` : tasks.yaml/roles.yamlを読み込みDAG実行
- `clampany version` : バージョン表示

## 仕様書
- 詳細仕様は `Clampany Implementation Specification (v0.2)` を参照

---

AI/自動化ワークフローのPoCや業務効率化にご活用ください。 