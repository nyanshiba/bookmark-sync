# bookmark-sync

Firefox Sync の開きタブを Clef で分類し、linkding の Inbox に自動投入するパイプライン。

> セットアップ・運用・トラブルシューティングの詳細は [SETUP.md](SETUP.md) を参照。

## 設計の考え方

- **ブラウザブックマークを共通規格として維持**: 日常運用は linkding（セルフホスト、API でアクセス）。Netscape HTML エクスポートはロックイン回避の避難口。
- **手動ブックマークのボトルネックを解消**: タブは全量自動で Inbox に投入。人間は週次のレビューで「残す／捨てる」だけ。
- **sync は開きタブのみを処理**: `bookmark-sync-sync` は `ffsclient tabs list`（Firefox Sync の開きタブ）だけを入力とする。Firefox ブックマーク（`bookmarks list`）とは比較・同期しない。ブックマークの移行は `bookmark-sync-import`（一度きり）が担当し、両者は独立。
- **Clef は「読むコストを下げる」役**: 各タブに既存タグから最も近いものを自動付与（新規タグは作らない）。価値判断は人間。
- **拡張・サーバー常駐不要**: バッチパイプライン（Go 単一バイナリ）が systemd timer で定期実行。

## 全体像

```
Firefox Sync (Windows + iOS)
  │
  ▼  ffsclient (Mikescher/firefox-sync-client)
bookmark-sync-sync (Go)
  ├── URL 正規化・重複排除（一覧 API で一括取得、ローカル判定）
  ├── タイトル内の短縮 URL 展開（t.co）
  ├── ドメインブロックリスト
  ├── Clef 分類（既存タグから最も近いものを付与、新規タグなし）
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
- Cloudflare アカウント（Clef 分類用。無料枠は 1 日 10,000 Neurons。目安は SETUP.md 5.2 の表を参照）
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
3. タイトル + タグを読んで判断:
   - **残す**: `inbox` タグをはずし、必要ならタグを編集
   - **捨てる**: 削除
4. ブラウザに戻って Firefox View の「他デバイスのタブ」から開いているタブを一括クローズ（Inbox に保全済みなので安心）

### 手動ブックマーク（ブックマークレット）

sync の取り込みを待たずに手動で Inbox へ入れたい場合、以下のブックマークレットを使う。
`LINKDING_URL` はブラウザから開く linkding の URL（例: `http://linkding.home.arpa:9090`）に置き換える。
ブックマークバーに新規ブックマークを作り、URL 欄に貼り付ける。

基本版（URL + タイトル + 保存後の自動クローズ）:

```javascript
javascript:void(function(){location.href='LINKDING_URL/bookmarks/new?url='+encodeURIComponent(location.href)+'&title='+encodeURIComponent(document.title)+'&auto_close'})()
```

選択テキストを説明として送る版（引用したい箇所を選択してから実行、最大 500 文字）:

```javascript
javascript:void(function(){var d=window.getSelection().toString();location.href='LINKDING_URL/bookmarks/new?url='+encodeURIComponent(location.href)+'&title='+encodeURIComponent(document.title)+'&description='+encodeURIComponent(d.substring(0,500))+'&auto_close'})()
```

手動追加したタイトル中の短縮 URL（t.co）は、毎日 5:00 実行の展開ジョブが自動で展開する（SETUP.md 13.7 参照）。

## プロジェクト構成

```
bookmark-sync/
├─ go.mod / Makefile
├─ config.example.toml
├─ bookmarks_coffee.html          ← テストデータ
├─ cmd/
│   ├─ import/main.go             # 既存ブックマーク取り込み（一度きり）
│   ├─ restore-tabs/main.go       # sessionstore → linkding 復元（一度きり）
│   ├─ expand-titles/main.go      # 既存タイトル内の短縮 URL 展開（一度きり）
│   ├─ classify-tags/main.go      # 既存 inbox の Clef 遡及分類（予算内で繰り返し）
│   └─ sync/main.go               # 定期パイプライン
├─ internal/
│   ├─ config/config.go           # TOML 設定読み込み
│   ├─ linkding/client.go         # linkding REST API クライアント
│   ├─ normalize/url.go           # URL 正規化・重複排除
│   ├─ expand/expand.go             # タイトル内の短縮 URL 展開
│   ├─ clef/clef.go               # Clef 判定クライアント（Workers AI 直結）
│   ├─ clef/budget.go             # 日次予算（Neurons・件数、sync と遡及で共有）
│   ├─ filter/filter.go           # ドメインブロックリスト
│   ├─ bookmark/parser.go         # Netscape HTML 解析
│   └─ sessionstore/sessionstore.go   # sessionstore 解析
├─ docker/
│   ├─ docker-compose.yml         # linkding コンテナ（マウント定義）
│   └─ docker.env                 # linkding の環境変数（パスワード・CSRF オリジン）
└─ deploy/
    ├─ bookmark-sync.service      # systemd oneshot（sync）
    ├─ bookmark-sync.timer        # systemd timer（1時間ごと）
    ├─ bookmark-sync-expand-titles.service  # systemd oneshot（既存タイトル展開）
    ├─ bookmark-sync-expand-titles.timer    # systemd timer（毎日 5:00）
    ├─ bookmark-sync-backup.service  # systemd oneshot（linkding 停止 → rsync → 再開）
    └─ bookmark-sync-backup.timer    # systemd timer（毎日 4:00）
```

## ライセンス

MIT
