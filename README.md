# bookmark-sync

Firefox Sync の開きタブを LLM で要約・タグ付けし、linkding の Inbox に自動投入するパイプライン。

> セットアップ・運用・トラブルシューティングの詳細は [SETUP.md](SETUP.md) を参照。

## 設計の考え方

- **ブラウザブックマークを共通規格として維持**: 日常運用は linkding（セルフホスト、API でアクセス）。Netscape HTML エクスポートはロックイン回避の避難口。
- **手動ブックマークのボトルネックを解消**: タブは全量自動で Inbox に投入。人間は週次のレビューで「残す／捨てる」だけ。
- **sync は開きタブのみを処理**: `bookmark-sync-sync` は `ffsclient tabs list`（Firefox Sync の開きタブ）だけを入力とする。Firefox ブックマーク（`bookmarks list`）とは比較・同期しない。ブックマークの移行は `bookmark-sync-import`（一度きり）が担当し、両者は独立。
- **LLM は「読むコストを下げる」役**: 各タブに一行要約＋タグ候補を自動付与。価値判断は人間。
- **拡張・サーバー常駐不要**: バッチパイプライン（Go 単一バイナリ）が systemd timer で定期実行。

## 全体像

```
Firefox Sync (Windows + iOS)
  │
  ▼  ffsclient (Mikescher/firefox-sync-client)
bookmark-sync-sync (Go)
  ├── URL 正規化・重複排除（一覧 API で一括取得、ローカル判定）
  ├── ドメインブロックリスト
  ├── LLM 要約（ローカル llama.cpp / クラウド / 機能別切替）
  └── linkding API → 📥Inbox
         │
         ▼
  linkding Web UI でレビュー（残す／捨てる）
         │
         ▼
  tag 付け・アーカイブ、タブは手動で閉じる
```

## 前提

- Linux（systemd 対応）
- Go 1.23 以降（ビルド時のみ）
- Docker / Docker Compose（linkding）
- ローカル LLM を使う場合は [llama.cpp](https://github.com/ggml-org/llama.cpp)（llama-server）
- Firefox アカウント（Firefox Sync 利用中）

## クイックスタート

詳細手順は SETUP.md（各章番号は参照先）に従ってください。要点のみ:

```bash
# ビルド → 専用ユーザー・バイナリ・設定を配置（SETUP.md 第2章）
cd bookmark-sync
make build
sudo useradd -m -s /usr/sbin/nologin bookmark-sync        # 未作成なら
sudo mkdir -p /home/bookmark-sync/bin /home/bookmark-sync/.config/bookmark-sync
sudo cp build/bookmark-sync-import build/bookmark-sync-restore-tabs \
        build/bookmark-sync-sync /home/bookmark-sync/bin/
sudo cp config.example.toml /home/bookmark-sync/.config/bookmark-sync/config.toml
sudo chown -R bookmark-sync:bookmark-sync /home/bookmark-sync

# linkding 起動（事前に docker/docker.env でパスワード・CSRF オリジンを設定）
cd docker && sudo docker compose up -d                     # SETUP.md 第3章

# ffsclient 取得と Firefox Sync 認証（初回のみ）
sudo -u bookmark-sync /home/bookmark-sync/bin/ffsclient login me@example.com 'mypassword'

# 設定編集（linkding.base_url / api_token / sync.firefox_sync_cli など）
sudo -u bookmark-sync editor /home/bookmark-sync/.config/bookmark-sync/config.toml

# timer 登録 → 定期実行開始
sudo cp deploy/bookmark-sync.service deploy/bookmark-sync.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now bookmark-sync.timer
```

補足:

- **既存ブックマークの移行（一度きり・任意）**: `bookmark-sync-import`（SETUP.md 第7章）
- **タブ欠落時の復元**: `bookmark-sync-restore-tabs`（SETUP.md 13.6）
- **トラブルシューティング**: SETUP.md 第12〜13章

## 日々の運用

1. タブが溜まってきたら linkding の Web UI を開く
2. `tag:inbox` で検索 → 「未レビューの Inbox」一覧
3. タイトル + 要約を読んで判断:
   - **残す**: `inbox` タグをはずし、必要ならタグを編集
   - **捨てる**: 削除
4. ブラウザに戻って Firefox View の「他デバイスのタブ」から開いているタブを一括クローズ（Inbox に保全済みなので安心）

## プロジェクト構成

```
bookmark-sync/
├─ go.mod / Makefile
├─ config.example.toml
├─ bookmarks_coffee.html          ← テストデータ
├─ cmd/
│   ├─ import/main.go             # 既存ブックマーク取り込み（一度きり）
│   ├─ restore-tabs/main.go       # sessionstore → linkding 復元（一度きり）
│   └─ sync/main.go               # 定期パイプライン
├─ internal/
│   ├─ config/config.go           # TOML 設定読み込み
│   ├─ linkding/client.go         # linkding REST API クライアント
│   ├─ normalize/url.go           # URL 正規化・重複排除
│   ├─ llm/provider.go            # LLM OpenAI 互換クライアント
│   ├─ filter/filter.go           # ドメインブロックリスト
│   ├─ bookmark/parser.go         # Netscape HTML 解析
│   └─ sessionstore/sessionstore.go   # sessionstore 解析
├─ docker/
│   ├─ docker-compose.yml         # linkding コンテナ（マウント定義）
│   └─ docker.env                 # linkding の環境変数（パスワード・CSRF オリジン）
└─ deploy/
    ├─ bookmark-sync.service      # systemd oneshot（sync）
    ├─ bookmark-sync.timer        # systemd timer（1時間ごと）
    ├─ bookmark-sync-backup.service  # systemd oneshot（linkding 停止 → rsync → 再開）
    └─ bookmark-sync-backup.timer    # systemd timer（毎日 4:00）
```

## ライセンス

MIT
