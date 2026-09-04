# bookmark-sync セットアップ手順書

Firefox Sync の開きタブを linkding の Inbox に自動投入するパイプライン `bookmark-sync` の
セットアップ手順です。前提は systemd が使える Linux で、root または sudo 権限を持つこと。

概要・設計・日々の運用・プロジェクト構成は [README.md](README.md) を参照。
以下の手順は「新規にゼロからセットアップする」順に並んでいます。

---

## 0. 全体の流れ

```
① 前提確認（Go / Docker / systemd / Firefox Sync）               … 第1章
② 取得・ビルド・専用ユーザー作成                                  … 第2章
③ linkding 起動と API トークン発行                                … 第3章
④ ffsclient 取得と Firefox Sync 認証                              … 第4章
⑤ 設定ファイル作成                                                … 第5章
⑥ 動作確認（テスト + テストデータ import）                        … 第6章
⑦ 既存ブックマークの移行 import（一度きり・任意）                  … 第7章
⑧ systemd timer 登録（メイン機能）                                … 第8章
⑨ バックアップ設定                                                … 第9章
```

---

## 1. 前提条件の確認

必要なもの:

| 項目 | 確認方法 | 備考 |
|---|---|---|
| Go 1.23+ | `go version` | ビルド時のみ。無ければ `dnf install golang` または `apt install golang` |
| Docker / Docker Compose | `docker compose version` | linkding の実行に使う。無ければ [Docker Engine 公式インストール手順](https://docs.docker.com/engine/install/) に従う（`docker compose` プラグインは Engine にバンドルされている） |
| systemd | `systemctl --version` | 通常 Linux 標準で入っている |
| Firefox アカウント | ブラウザで Sync を有効にしていること | パイプラインで Firefox Sync の Tabs コレクションを読む |

> **Docker が使えない環境の場合**:
> linkding は pip でも実行可能です（`pip install linkding`）。
> ただし設定が複雑になるため、この手順書では Docker を使用する前提で進めます。
> Docker 無しで動かす場合は linkding 公式ドキュメントを参照してください。

---

## 2. プロジェクトの取得とビルド

### 2.1 コードの取得

```bash
# あなたの Linux マシン上の任意の場所で
mkdir -p ~/src
cd ~/src

# このリポジトリを clone する（または scp / rsync で転送する）
# すでにコードがある場合はそのディレクトリに移動
cd bookmark-sync
```

### 2.2 Go のインストール（無い場合）

```bash
# Debian / Ubuntu
sudo apt update && sudo apt install golang-go

# Fedora / RHEL
sudo dnf install golang

# 確認
go version
# → go 1.23.0 以上なら OK
```

### 2.3 依存ダウンロードとビルド

```bash
cd ~/src/bookmark-sync

# 依存パッケージをダウンロード（初回のみ）
go mod tidy

# フォーマット（コードが読みやすくなる）
gofmt -w .

# ビルド
make build

# ビルド成果物を確認
ls -la build/
# → bookmark-sync-import, bookmark-sync-restore-tabs, bookmark-sync-sync の3つのバイナリ
```

### 2.4 専用ユーザーの作成（推奨）

bookmark-sync は専用のシステムユーザーで動作させることを推奨します。

```bash
sudo useradd -m -s /usr/sbin/nologin bookmark-sync

# バイナリを配置
sudo mkdir -p /home/bookmark-sync/bin
sudo cp build/bookmark-sync-import build/bookmark-sync-restore-tabs \
        build/bookmark-sync-sync /home/bookmark-sync/bin/

# 設定ディレクトリ
sudo mkdir -p /home/bookmark-sync/.config

# 所有者
sudo chown -R bookmark-sync:bookmark-sync /home/bookmark-sync
```

---

## 3. linkding の起動と初期設定

### 3.1 Docker Compose の起動

```bash
cd ~/src/bookmark-sync/docker

# データ保存用ディレクトリを作成（ホスト側の永続化先）
mkdir -p linkding-data

# 初回起動前に docker.env でスーパーユーザーのパスワードと
# アクセスするドメイン名（LD_CSRF_TRUSTED_ORIGINS）を自分の環境に合わせて変更する
editor docker.env
# - LD_SUPERUSER_PASSWORD をあなたのパスワードに書き換える
# - LD_CSRF_TRUSTED_ORIGINS を実際のアクセス URL に合わせる
#   （例: http://linkding.home.arpa:9090）

# 起動
sudo docker compose up -d

# 起動確認
sudo docker compose ps
# → linkding が Up 状態なら OK
```

> **ポート公開について**:
> `docker-compose.yml` は `"9090:9090"` で全インタフェースに公開している。
> `linkding.home.arpa` 等のドメイン名で LAN 内からアクセスするための設定。
> 外部公開する場合は、直接ポートを晒すのではなく nginx / Caddy で
> リバースプロキシし、そちらで TLS を終端することを推奨する。

### 3.2 初期設定（Web UI）

1. ブラウザでリンクディングの URL を開く（例: `http://linkding.home.arpa:9090`）
2. 以下の初期スーパーユーザーでログイン:
   - ユーザー名: `bookmark-sync`
   - パスワード: `docker.env` の `LD_SUPERUSER_PASSWORD` で設定した値
3. ログイン後、右上の歯車アイコン → 「設定」を開く
4. 「API トークン」の欄で「作成」をクリック
5. 表示されたトークンをコピーして保存する（次のステップで使う）

> **注意**: 初回ログイン後は `docker.env` の `LD_SUPERUSER_NAME` と
> `LD_SUPERUSER_PASSWORD` を空にして再起動すると安全です。
> ただし、空にすると後からスーパーユーザーを Web UI で作成できなくなるので、
> パスワードは必ず記録しておいてください。

> **ドメイン名アクセスの CSRF 設定**:
> ドメイン名（`linkding.home.arpa` 等）でブラウザからアクセスする場合、
> `docker.env` の `LD_CSRF_TRUSTED_ORIGINS` を実際に使う URL に合わせておくこと。
> 変更後は `sudo docker compose restart` で反映する。
> これを怠ると、ログインや保存時に「CSRF 検証に失敗」エラーになる。

---

## 4. ffsclient のセットアップ

### 4.1 ダウンロード

```bash
# Linux amd64（一般的な64ビットPC）:
sudo curl -L -o /home/bookmark-sync/bin/ffsclient \
  https://github.com/Mikescher/firefox-sync-client/releases/latest/download/ffsclient_linux-amd64
sudo chmod +x /home/bookmark-sync/bin/ffsclient
sudo chown bookmark-sync:bookmark-sync /home/bookmark-sync/bin/ffsclient

# Linux arm64（Raspberry Pi など）:
#   ffsclient_linux-arm64 をダウンロードする
# 32ビット:
#   ffsclient_linux-386 をダウンロードする
# 静的リンク版が必要な場合（glibc 非互換）:
#   ffsclient_linux-amd64-static をダウンロードする
```

> **自前ビルド（任意・最も確実な検証）**:
> リリースバイナリに署名・チェックサムが同梱されないため、
> 「公開ソースから生成されたもの」を確実に検証したい場合は
> ソースから自前ビルドする（監査注記は 4.2 参照）。
>
> ```bash
> # 開発マシン or このサーバー上で
> git clone https://github.com/Mikescher/firefox-sync-client.git
> cd firefox-sync-client
> make build          # _out/ffsclient が生成される
>
> # 生成物を配置
> sudo install -o bookmark-sync -g bookmark-sync -m 755 \
>   _out/ffsclient /home/bookmark-sync/bin/ffsclient
> ```

### 4.2 Firefox Sync 認証

```bash
# bookmark-sync ユーザーで実行
# 実際の Firefox アカウントのメールアドレスとパスワードに置き換える。
# 注意: <email> <password> のように角カッコ < > で囲むと bash が
# リダイレクト記号と解釈し、引数が 0 個になり
# 「Not enough arguments for ffsclient login (must be exactly 2)」エラーになる。
# シングルクォート '…' で囲んでも <email> が文字列としてそのまま渡され
# 認証に失敗するだけなので、< > は使わず以下の例のように直に書く。
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient login me@example.com 'mypassword'

# 2段階認証を使っている場合（TOTP）:
# sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient login me@example.com 'mypassword' --otp 123456
```

> **パスワードに `!` `$` 等の特殊文字が含まれる場合**:
> シングルクォートで囲めば bash による展開を防げる。
> ただし `<` `>` の角カッコは使わないこと。シングルクォートで囲むと
> 今度は `<email>` という文字列がそのまま渡されて認証に失敗するだけになる。

セッションは `~/.config/firefox-sync-client.secret` に保存されます。
ffsclient はセッションを自動更新するため、パイプラインの定期実行で再認証は不要です。

> **セキュリティ監査メモ（2026-09 実施）**:
> `Mikescher/firefox-sync-client` v1.10.0 を、リポジトリのソースコード実物・
> コミット履歴・リリース・依存関係・Issue を一次ソースとして監査した。
>
> **判定: 悪意の証拠なし**。根拠は以下。
> - 認証情報の送信先は Mozilla 公式のみ（`api.accounts.firefox.com` /
>   `token.services.mozilla.com` ほか）。外部への送信コードなし
> - 復号後の同期データは stdout / ローカルファイルにのみ出力
> - 難読化・自己改変・バックドア・C2 的挙動はなし
> - コミットは作者の PGP 鍵で署名済み（GitHub で Verified 表示）
> - 依存関係は公式・広く使われるモジュールと作者自身の管理下ライブラリ
>   （go.sum でハッシュ固定）
>
> **軽微な懸念（悪意とは別に、運用時の注意点）**:
> - ログイン時のパスワードがコマンドライン引数で渡るため、
>   シェル履歴と `ps` に露出する。ログイン後は `history -c` で履歴を消去するか、
>   `HISTFILE` を無効化してから実行する
> - `~/.config/firefox-sync-client.secret` に復号鍵が平文保存される
>   （パーミッション 0600 で保護）。`ls -l` で所有者のみ読めることを確認する
> - リリースバイナリに署名・チェックサムファイルは同梱されない。
>   最も確実な検証はソースからの自前ビルド（下記 4.1 代替参照）

### 4.3 動作確認

```bash
# タブが取得できることを確認
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient tabs list --format json

# Firefox で開いているタブが JSON の配列として出力されれば OK
# タブを開いていなければ空配列 [] が返る
```

---

## 5. 設定ファイルの作成

```bash
# 設定ファイルをコピー
sudo cp ~/src/bookmark-sync/config.example.toml /home/bookmark-sync/.config/bookmark-sync/config.toml

# 編集
sudo -u bookmark-sync editor /home/bookmark-sync/.config/bookmark-sync/config.toml
```

### 5.1 最低限の設定項目

| 設定キー | 説明 | 設定例 |
|---|---|---|
| `linkding.base_url` | linkding のアドレス | `http://localhost:9090` |
| `linkding.api_token` | 3.2 で発行した API トークン | `xxxxxxxxxxxxx` |
| `sync.firefox_sync_cli` | ffsclient の絶対パス | `/home/bookmark-sync/bin/ffsclient` |
| `llm.enabled` | LLM を使う場合は `true`（既定は `false`、無効） | `true` |
| `filter.blocked_domains` | 取り込み除外ドメイン（必要なら） | `["twitter.com", "youtube.com"]` |

### 5.2 LLM プロバイダの設定

ローカルに llama.cpp が動いている場合:

```toml
[llm.summarize]
base_url = "http://127.0.0.1:8080/v1"
api_key = ""
model = "llama-3.1-8b"

[llm.tag]
base_url = "http://127.0.0.1:8080/v1"
api_key = ""
model = "llama-3.1-8b"
```

クラウド API を使う場合（例: OpenAI）:

```toml
[llm.summarize]
base_url = "https://api.openai.com/v1"
api_key = "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
model = "gpt-4o-mini"

[llm.tag]
base_url = "https://api.openai.com/v1"
api_key = "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
model = "gpt-4o-mini"
```

> **LLM を使わない場合**:
> `[llm]` の `enabled` は**既定で `false`（無効）**です。
> LLM を使う場合は `enabled = true` を設定します。
> 無効時は要約・タグ生成を一切行わず、タイトルと URL だけ（タグは `inbox` のみ）で保存します。
> 最初にパイプラインの動作を確認したい場合は、このまま（無効のまま）動かせます。
> 後から `enabled = true` を追加するだけです。

---

## 6. 動作確認（テスト）

### 6.1 パーサーテストの実行

```bash
cd ~/src/bookmark-sync

# パーサーのテストを実行（bookmarks_coffee.html を使用）
go test -v ./internal/bookmark/

# 期待される結果:
#   TestParseCoffee: 20 エントリが正しくパースされ、最下層フォルダ名がタグ化されること
#   TestParseFileNotFound: エラーが返ること
#   TestParseEmptyFile: 0 エントリが返ること
#   PASS なら OK
```

### 6.2 テストデータの import（dry-run で確認）

`bookmarks_coffee.html` はプロジェクトルートに置いてあるテストデータです。
これを linkding に import して、パイプライン全体の動作確認をします。

`sudo -u bookmark-sync` で実行するため、`bookmark-sync` ユーザーが
読める場所にテストデータをコピーしておきます。

```bash
# テストデータを bookmark-sync ユーザーのホームへコピー
sudo cp ~/src/bookmark-sync/bookmarks_coffee.html /home/bookmark-sync/
sudo chown bookmark-sync:bookmark-sync /home/bookmark-sync/bookmarks_coffee.html

# まず dry-run で確認（何も書き込まれない）
sudo -u bookmark-sync /home/bookmark-sync/bin/bookmark-sync-import \
  -config /home/bookmark-sync/.config/bookmark-sync/config.toml \
  -input /home/bookmark-sync/bookmarks_coffee.html \
  -dry-run

# 出力例:
#   would create "マキネッタのお手入れ方法..." -> https://pina817.com/aftermacchinetta/ [Coffee]
#   20 件の would create が表示される
```

### 6.3 実際に import する

```bash
# 本番実行
sudo -u bookmark-sync /home/bookmark-sync/bin/bookmark-sync-import \
  -config /home/bookmark-sync/.config/bookmark-sync/config.toml \
  -input /home/bookmark-sync/bookmarks_coffee.html

# 期待される出力:
#   parsed 20 bookmarks from ...
#   20 created, 0 skipped, 0 failed
```

### 6.4 linkding で確認

1. ブラウザで `http://localhost:9090` を開く
2. 検索欄にタグ `Coffee` で検索 → 20件のコーヒーブックマークが表示される
3. 各ブックマークのタグが `Coffee` だけになっていることを確認
   （フォルダ階層はタグ化せず、最下層フォルダ名のみをタグにする）
4. `date_added` が元の `ADD_DATE`（`1601816117` など）に対応する日時になっていることを確認

### 6.5 重複排除の確認

もう一度同じ import を実行すると、全件 skipped になることを確認します。
（重複判定の仕組みは 12.6 参照）

```bash
sudo -u bookmark-sync /home/bookmark-sync/bin/bookmark-sync-import \
  -config /home/bookmark-sync/.config/bookmark-sync/config.toml \
  -input /home/bookmark-sync/bookmarks_coffee.html

# 期待される出力:
#   0 created, 20 skipped, 0 failed
```

---

## 7. 本番データの import（既存ブックマークの移行）

`bookmark-sync-import` は Firefox からエクスポートした Netscape HTML 形式の
ブックマークを linkding に取り込む、**一度だけ実行する手動ツール**です。
この章は定期実行（第8章）とは独立しています。既存ブックマークの移行が不要なら
飛ばして構いません。

### 7.1 Firefox からのエクスポート

1. Firefox で `Ctrl+Shift+O`（ライブラリ → ブックマーク → すべてのブックマークを表示）
2. メニューから「インポートとバックアップ」→「HTML でエクスポート」
3. 任意の場所に保存（例: `bookmarks_export.html`）

### 7.2 エクスポートファイルをサーバーに転送

```bash
# ローカルで scp するか、サーバー上で直接ダウンロードする
scp bookmarks_export.html bookmark-sync@your-server:/tmp/
```

### 7.3 import の実行

```bash
# まず dry-run で件数を確認
sudo -u bookmark-sync /home/bookmark-sync/bin/bookmark-sync-import \
  -config /home/bookmark-sync/.config/bookmark-sync/config.toml \
  -input /tmp/bookmarks_export.html \
  -dry-run 2>&1 | tail -5

# 想定される件数（例: 10,042 件）を確認
# 本番実行
sudo -u bookmark-sync /home/bookmark-sync/bin/bookmark-sync-import \
  -config /home/bookmark-sync/.config/bookmark-sync/config.toml \
  -input /tmp/bookmarks_export.html

# 完了までに時間がかかる場合があります（10,000件で数分程度）
```

> **注意**: 重複判定の仕組み（一覧 API によるローカル判定）は 12.6 を参照。

### 7.4 書き込み時のエラー処理（linkding クライアント共通）

import / sync / restore-tabs のいずれも同じ linkding クライアントを使用するため、
以下のエラー処理は全コマンドで共通です。

- **コネクション切断**: サーバーがアイドル接続を閉じた場合
  （`http: server closed idle connection`）は、**同じブックマークを最大4回
  再試行**します。切断中のブックマークをスキップせず、接続を作り直して再開
  します。それでも失敗した場合のみ `create failed` になります。
- **title が 512 文字を超える**: linkding は title を最大512文字
  （コードポイント）に制限します。超える場合は**文末を切り詰め、タグ
  `title-truncated` を付けて**保存します（失敗になりません）。

---

## 8. systemd timer の登録（定期実行）

**この章がこのプロジェクトのメイン機能です。**
`bookmark-sync-sync` は一定間隔ごとに起動し、Firefox Sync の開きタブを取得 →
LLM で要約・タグ付け → linkding の Inbox に自動投入します。

> **`bookmark-sync-sync` の入力は `ffsclient tabs list`（開きタブ）だけ**です。
> Firefox ブックマーク（`ffsclient bookmarks list`）とは比較も同期もしません。
> ブックマークの移行が必要なら第7章の `bookmark-sync-import`（一度きり）を使い、
> 両者は独立しています。

### 8.1 ユニットファイルの編集と配置

```bash
# パスが実際の環境と合っているか確認
cat ~/src/bookmark-sync/deploy/bookmark-sync.service
# ExecStart のパスを確認:
#   /home/bookmark-sync/bin/bookmark-sync-sync
#   /home/bookmark-sync/.config/bookmark-sync/config.toml
# 実環境と異なる場合は編集する

# 配置
sudo cp ~/src/bookmark-sync/deploy/bookmark-sync.service /etc/systemd/system/
sudo cp ~/src/bookmark-sync/deploy/bookmark-sync.timer /etc/systemd/system/

# 権限
sudo chmod 644 /etc/systemd/system/bookmark-sync.service
sudo chmod 644 /etc/systemd/system/bookmark-sync.timer

# ユニットを読み込み直す
sudo systemctl daemon-reload
```

### 8.2 手動での試運転（初回確認）

**初回は timer を有効化する前に手動で実行し、動作と処理時間を確認します。**

```bash
# 手動で service を起動
sudo systemctl start bookmark-sync.service

# 終了を待ち、ログを確認
journalctl -u bookmark-sync.service --since "1 min ago" --no-pager

# 正常な出力例:
#   fetching tabs from Firefox Sync…
#   fetched 3 tab(s)
#   done: 1 created, 2 skipped (existing/blocked), 0 failed
```

**処理時間を確認します。**

```bash
# oneshot サービスは systemctl start が完了までブロックするため、
# time で直接所要時間を計測できる
time sudo systemctl start bookmark-sync.service

# 正常な出力例（time の実測値）:
#   real    0m42.315s
#   user    0m0.010s
#   sys     0m0.012s
```

処理時間が長い場合（タブ件数が多い）は `TimeoutStartSec` の調整が必要です
（12.5 参照）。

### 8.3 timer の有効化と起動

手動試運転で正常動作と処理時間を確認したら、timer を有効化します。

```bash
sudo systemctl enable bookmark-sync.timer
sudo systemctl start bookmark-sync.timer

# 状態確認
systemctl status bookmark-sync.timer
# → active (waiting) と表示されれば OK

# 次回の実行予定を確認
systemctl list-timers --all | grep bookmark-sync
```

> **timer のスケジュール**: 起動後 5 分で初回実行、以降 1 時間ごとに実行されます。
> 電源オフ中に逃した実行は、次回起動後に補完されます（`Persistent=true`）。
> 乱打防止のため、実行開始時刻に 0〜60 秒のランダム遅延が入ります。

---

## 9. バックアップの設定

`bookmark-sync-backup.service` + `bookmark-sync-backup.timer` で、linkding のデータを
**毎日 4:00** に **rsync ミラー**でバックアップします。
方式は「コンテナを停止 → rsync → 再開」で、SQLite の一貫性を保証します。

### 9.1 バックアップ対象の把握

```bash
# マウント構成:
#   ./linkding-data  →  /etc/linkding/data  （コンテナ内）
# ホスト側のデータディレクトリ:
#   /home/your-user/src/bookmark-sync/docker/linkding-data
#     ├─ db/        # SQLite DB
#     ├─ uploads/   # アップロード
#     ├─ favicons/  # ファビコン
#     ├─ previews/  # プレビュー
#     └─ backups/   # リンクディング自動バックアップ（LD_BACKUP_DIR）
```

### 9.2 サービスファイルの配置と有効化

`deploy/bookmark-sync-backup.service` 内の絶対パスを自分の環境に合わせてから配置します。
（`/home/your-user` を実ユーザー名に置き換え、必要なら docker / rsync のパスも調整）

```bash
# パスを確認・編集
grep -n your-user ~/src/bookmark-sync/deploy/bookmark-sync-backup.service

# 配置
sudo cp ~/src/bookmark-sync/deploy/bookmark-sync-backup.service /etc/systemd/system/
sudo cp ~/src/bookmark-sync/deploy/bookmark-sync-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload

# 有効化（毎日 4:00）
sudo systemctl enable --now bookmark-sync-backup.timer

# 両タイマーの次回実行予定を確認
systemctl list-timers --all | grep bookmark-sync
```

### 9.3 手動実行

```bash
sudo systemctl start bookmark-sync-backup.service

# ログで確認
journalctl -u bookmark-sync-backup.service --since "1 min ago" --no-pager
```

### 9.4 動作の仕組みと SQLite の一貫性

- **コンテナを停止してから rsync**: 稼働中の SQLite を直接コピーすると書き込み途中の
  ファイルが保存され不整合になるため、`docker compose stop` → `rsync -a --delete` →
  `docker compose start` の順で行う
- **rsync 失敗時もコンテナは必ず再開**: 再開コマンドは `ExecStopPost` に置く。
  `ExecStopPost` はサービスが成功しても失敗しても必ず実行されるため、ディスク容量不足などで
  rsync が失敗しても linkding が落ちたままになることはない
- **ミラー**: `--delete` 付き rsync のため、元で消えたファイルはバックアップ先からも
  削除される。世代管理はしない

### 9.5 bookmark-sync.timer との重なり

backup（毎日 4:00）と sync（毎時）の実行が重なると、backup がコンテナを停止している間、
sync は linkding への接続エラーで失敗します。ただし**無害**です。

- sync は URL ベースの重複排除で冪等なため、次回（1 時間後）の実行時に
  未登録分だけが処理される
- データが失われる・二重登録される事故は起きない

### 9.6 リンクディング側の自動バックアップ（二重の保険）

> **注意**: リンクディングは `LD_BACKUP_DIR`（= `linkding-data/backups`）に
> 毎日自動で tar.gz バックアップを作成します。
> バックアップパスワードが未設定の場合、バックアップは平文で保存されます。
> 本節の rsync ミラーは DB とアップロードのスナップショットを取る目的で、
> リンクディング側の自動バックアップとは二重の保護になります。

---

## 10. 既存環境の更新手順

すでに稼働中の環境を更新する手順です。

### 10.1 パイプライン（Go バイナリ）の更新

```bash
cd ~/src/bookmark-sync

# コードを最新化（このリポジトリの更新を反映）
git pull
# または scp / rsync で更新されたファイルを転送

# 再ビルド
go mod tidy
make build

# 全バイナリを配置し直す
sudo cp build/bookmark-sync-import build/bookmark-sync-restore-tabs \
        build/bookmark-sync-sync /home/bookmark-sync/bin/
sudo chown bookmark-sync:bookmark-sync /home/bookmark-sync/bin/bookmark-sync-*
```

### 10.2 systemd ユニットファイルの更新

`deploy/` 内の service / timer を変更したら、再配置して反映します。

```bash
cd ~/src/bookmark-sync

# sync ユニット
sudo cp deploy/bookmark-sync.service /etc/systemd/system/
sudo cp deploy/bookmark-sync.timer /etc/systemd/system/

# backup ユニット（ファイル内の絶対パスを確認してから）
grep -n your-user deploy/bookmark-sync-backup.service
sudo cp deploy/bookmark-sync-backup.service /etc/systemd/system/
sudo cp deploy/bookmark-sync-backup.timer /etc/systemd/system/

sudo systemctl daemon-reload
```

### 10.3 linkding コンテナの更新（イメージ更新時）

linkding の新バージョンが公開されたとき:

```bash
cd ~/src/bookmark-sync/docker
sudo docker compose pull     # 最新イメージを取得
sudo docker compose up -d    # コンテナを作り直して起動
```

### 10.4 設定変更（docker.env）の反映

`docker.env` を編集したら:

```bash
cd ~/src/bookmark-sync/docker
sudo docker compose up -d    # 設定差分があれば自動で作り直す
# 環境変数だけの変更なら再起動でも反映される
sudo docker compose restart
```

### 10.5 docker-compose.yml を変更したとき

マウントやポートの定義を変えた場合は、コンテナの作り直しが必要です。

```bash
cd ~/src/bookmark-sync/docker
sudo docker compose rm -fs linkding   # 既存コンテナを強制削除
sudo docker compose up -d             # 作り直し
```

> **注意**: `rm -f` でコンテナを削除しても、データはホスト側の
> `./linkding-data` に永続化されているため消失しません。

---

## 11. テストの再実行

コードに変更を加えた後は、以下のコマンドで全テストを実行できます。

```bash
cd ~/src/bookmark-sync

# 全パッケージのテストを実行
go test -v ./...

# 特定のパッケージだけ
go test -v ./internal/bookmark/
go test -v ./internal/normalize/
go test -v ./internal/config/
```

---

## 12. トラブルシューティング

### 12.1 `go mod tidy` がエラーになる

```
go: github.com/BurntSushi/toml@v1.4.0: ...
```

Go のバージョンが古い可能性があります。`go version` で 1.23 以上かを確認してください。
古い場合は `go mod tidy -go=1.21` でバージョンを下げるか、Go をアップデートしてください。

### 12.2 ffsclient の認証が通らない

```bash
# セッションを削除して再認証
sudo -u bookmark-sync rm -f /home/bookmark-sync/.config/firefox-sync-client.secret
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient login <email> <password>
```

Fastly のチャレンジ（HTTP 406）が発生する場合、ffsclient が自動解決します。
解決に失敗した場合（`client-side Javascript challenge` のエラー）、
ffsclient のバージョンを最新にアップデートしてください。

### 12.3 linkding の API トークンが不明

```bash
# ブラウザでリンクディングの URL にログイン（例: http://linkding.home.arpa:9090）
# 設定 → API トークン → 再作成
```

### 12.4 パイプラインが失敗する（LLM エラー）

LLM のエンドポイントや API キーが正しくない場合、エラーログが出ますが、
パイプラインは LLM が失敗してもタイトルと URL だけでリンクディングに保存します。
エラーログの意味を切り分けるには:

- **LLM を一時的に止めたい**: `[llm]` の `enabled = false` にすると、LLM 呼び出し自体を
  スキップしてエラーログも出なくなります（タイトル + URL + inbox タグのみで保存）。
- **LLM を直したい**: `enabled = true` のまま、`base_url` / `api_key` / `model` を確認します。
  ローカル llama.cpp なら `curl http://127.0.0.1:8080/v1/models` で応答を確認できます。

後から要約を追加したい場合は、LLM 設定を直して `bookmark-sync-sync` を再実行するか、
手動でリンクディングの Web UI から編集してください。

### 12.5 systemd サービスがタイムアウトする

タブ件数が想定以上に多い場合（孤児レコードの原因 → 第13章）、
`ffsclient tabs list` の取得件数が膨れ上がり、`TimeoutStartSec` が処理時間より
短いと途中で切られて**歯抜け**になります。

`deploy/bookmark-sync.service` の `TimeoutStartSec` を十分な値に設定してください。

```
TimeoutStartSec=3600   # 1時間（48,000件規模でも処理しきる目安）
```

タイムアウトで切れても、sync は URL 重複で冪等（次回実行時に未登録分だけを
処理）なので、タイマーの次回実行で残りが拾われます。歯抜けを残さないには
`TimeoutStartSec` を増やすのが確実です。

### 12.6 重複したタブが何度も取り込まれる

`bookmark-sync-sync` は起動時に linkding の一覧 API で既存 URL を一括取得し、
ローカルで重複判定を行います。
（check API は毎回 URL をスクレイピングするため使いません。）
同一 URL のタブは 2 回目以降 skipped になります。
URL が異なる（例えば `?utm_source=...` が付いた）場合は、
正規化で同一と見なされるはずです。
正規化が効かない URL パターンがあれば、`internal/normalize/url.go` の
`trackingParams` マップにパラメータ名を追加してください。

### 12.7 コンテナが起動しない「exec: ./bootstrap.sh: no such file or directory」

**原因**: `docker-compose.yml` のボリュームマウント先が間違っている。

リンクディングの最新イメージでは、アプリ本体（`bootstrap.sh` を含む）が
コンテナ内の `/etc/linkding` 直下に展開され、`CMD ["./bootstrap.sh"]` で起動します。
ここにホストの空ディレクトリをマウントすると、空ディレクトリがアプリ本体を
覆い隠し、`exec: ./bootstrap.sh: no such file` エラーで起動に失敗します。

**対処**: マウント先を `/etc/linkding/data` に修正して作り直す。

```bash
cd ~/src/bookmark-sync/docker

# docker-compose.yml の volumes を確認:
#   volumes:
#     - ./linkding-data:/etc/linkding/data   ← これが正しい
#   （/etc/linkding 直下へのマウントは NG）

# 起動に失敗したコンテナを削除
sudo docker compose rm -fs linkding

# データディレクトリ作成 → 起動
sudo mkdir -p linkding-data
sudo docker compose up -d
sudo docker compose ps
```

起動に失敗した時点では bootstrap.sh が実行されていないため、
コンテナ内にデータは生成されていません。安全に作り直せます。

---

## 13. Firefox Sync タブコレクションの整理

`bookmark-sync-sync` が処理するタブ数が、開いているタブ数と釣り合わず
想定以上に多い場合（実測 48,054 件）、Firefox Sync の tabs コレクションに
**孤児レコード**が残っている可能性が高いです。

**代表的な原因**: Firefox アカウントから**ログアウトした端末**のタブが
サーバー上に残る。ログアウト済みの端末は再同期しないため、その端末に
紐づくタブはクライアント側で整理されず、tabs コレクションに死骸として
残り続けます。この状態だと `bookmark-sync-sync` は過去分も全部拾うため、
Inbox に大量投入されます。

以下で確認・削除します。

### 13.1 コレクションとクライアントの確認

```bash
# コレクションごとのレコード数（tabs の件数を確認）
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient collections

# 登録済みクライアント一覧（タブを持つ正当な端末）
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient list clients --decoded
```

`collections` で `tabs` のレコード数が異常に多い場合は、以下へ進みます。

### 13.2 特定クライアントのタブを確認・削除

`list clients --decoded` で分かった不要な端末（ログアウト済み端末）の
クライアント ID を指定して、そのタブだけ確認・削除できます。

```bash
# 特定クライアントのタブを確認
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient tabs list --client <クライアントID>

# そのクライアントの全タブを削除（--hard でサーバーから完全削除）
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient delete tabs <クライアントID> --hard
```

### 13.3 clients に存在しない孤児タブの炙り出し

`list clients` に載っていないクライアント ID のタブが tabs コレクションに
残っていることがあります（ログアウト時にクライアント情報は消えるが
タブは消えないケース）。全タブを CSV で出力し、先頭列のクライアント ID を
ユニーク化して、13.1 の `list clients --decoded` と突き合わせます。
CSV の 1 行目はヘッダ行（`ClientID,ClientName,...`）なので
`NR>1` で読み飛ばします。

```bash
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient tabs list --format csv | awk -F, 'NR>1 {print $1}' | sort -u
```

出力されたクライアント ID のうち、`list clients --decoded` に**無い**ものが
孤児タブの持ち主です。

### 13.4 孤児レコードの削除

tabs コレクションは**クライアントごとに 1 レコード**（そのクライアントの
全タブ）として格納されているため、孤児タブもそのレコード単位で削除します。
`tabs list` の `--limit` は**クライアント数**の制限で
（`--limit 0`＝無制限・全件、`--limit 1`＝1 クライアント分のみ）です。

```bash
# 1 クライアント分だけ表示してレコード ID を確認
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient tabs list --limit 1

# 特定レコードを削除（--hard でサーバーから完全削除）
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient delete tabs <レコードID> --hard
```

孤児タブが複数ある場合は、削除後に 13.3 を再実行して残りを炙り直します。

### 13.5 削除後の確認

```bash
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient collections
```

tabs のレコード数が開いているタブ数と釣り合うまで減っていれば完了です。
`bookmark-sync-sync` のログ（`fetched N tab(s)`）で取得件数が
正常な水準に戻ったことを確認してください。

### 13.6 タブが想定より少ない場合（sessionstore からの復元）

13.1〜13.5 の整理でタブが減りすぎた、または旧プロファイルのタブが元々
同期されていなかった場合、`fetched N tab(s)` が手元のセッション
（sessionstore.jsonlz4）と釣り合わないことがあります。

`bookmark-sync-sync` の入力は Firefox Sync サーバーの tabs コレクションで、
sessionstore ファイルは**ローカルのみ**です。sessionstore.jsonlz4 を
新プロファイルへ入れて Firefox に復元させる方法は、タブ数が 10K 超だと
同期が安定しないため当てにできません。

代わりに `bookmark-sync-restore-tabs` で sessionstore ファイルを直接解析し、
linkding へ追加します。タブの最終使用時刻（`lastAccessed`）は
`date_added` として保存されるため、通常の同期経路（タブの `lastUsed` → 
`date_added`）と同じく日付情報は失われません。

```bash
# ビルド（build/bookmark-sync-restore-tabs が生成される）
cd bookmark-sync
make build

# バイナリを配置（未導入なら）
sudo cp build/bookmark-sync-restore-tabs /home/bookmark-sync/bin/
sudo chown bookmark-sync:bookmark-sync /home/bookmark-sync/bin/bookmark-sync-restore-tabs

# 復元（mozLz4 圧縮 / 平文 JSON のどちらも解釈する）
sudo -u bookmark-sync /home/bookmark-sync/bin/bookmark-sync-restore-tabs \
  -config /home/bookmark-sync/.config/bookmark-sync/config.toml \
  /backup/sessionstore-A.jsonlz4 /backup/sessionstore-B.jsonlz4 \
  /backup/sessionstore-C.jsonlz4 /backup/sessionstore-D.jsonlz4

# 実行例（ログ）
# parsed 10125 tabs from /backup/sessionstore-A.jsonlz4
# total 11438 tab(s) from 4 file(s)
# done: 8762 created, 2676 skipped, 0 failed
```

動作の要点:
- **重複はスキップ**。linkding 内の既存 URL に加え、同一実行内で
  既に追加した URL（複数ファイル間の重複含む）も追加しない
- **ブロックドメイン・不正 URL（`about:` など）はスキップ**
- `-dry-run` を付けると何も書き込まずに追加予定だけ表示する
- 書き込み後も通常の `bookmark-sync-sync`（timer）が重複を作らない
  （URL ベースの重複排除）