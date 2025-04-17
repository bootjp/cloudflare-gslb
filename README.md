# Cloudflare GSLB

CloudflareのDNSレコードに対するヘルスチェックと自動フェイルオーバーを提供するGSLB（Global Server Load Balancing）システムです。

## 機能

- AレコードとAAAAレコードに対するヘルスチェック
- HTTPSヘルスチェック（カスタムパスとホスト名の指定可能）
- HTTPヘルスチェック（カスタムパスとホスト名の指定可能）
- ICMPヘルスチェック
- 異常検知時の自動DNSレコード置き換え
- 設定可能なチェック間隔
- カスタムフェイルオーバーIPアドレスのリスト設定
- オリジンごとのCloudflareプロキシ設定

## インストール

```bash
git clone https://github.com/bootjp/cloudflare-gslb.git
cd cloudflare-gslb
go build -o gslb ./cmd/gslb
```

## 設定

`config.json.example`をコピーして`config.json`を作成し、必要な設定を行います。

```bash
cp config.json.example config.json
```

設定ファイルの例:

```json
{
  "cloudflare_api_token": "YOUR_CLOUDFLARE_API_TOKEN",
  "cloudflare_zone_id": "YOUR_CLOUDFLARE_ZONE_ID",
  "check_interval_seconds": 60,
  "origins": [
    {
      "name": "example.com",
      "record_type": "A",
      "health_check": {
        "type": "https",
        "endpoint": "/health",
        "host": "example.com",
        "timeout": 5
      },
      "priority_failover_ips": [
        "192.168.1.2",
        "192.168.1.3",
        "192.168.1.4"
      ],
      "failover_ips": [
        "192.168.1.2",
        "192.168.1.3",
        "192.168.1.4"
      ],
      "proxied": true
    },
    {
      "name": "api.example.com",
      "record_type": "A",
      "health_check": {
        "type": "http",
        "endpoint": "/status",
        "host": "api.example.com",
        "timeout": 5
      },
      "failover_ips": [
        "10.0.0.2",
        "10.0.0.3"
      ],
      "proxied": true
    },
    {
      "name": "ipv6.example.com",
      "record_type": "AAAA",
      "health_check": {
        "type": "icmp",
        "timeout": 5
      },
      "failover_ips": [
        "2001:db8::2",
        "2001:db8::3",
        "2001:db8::4"
      ],
      "proxied": false
    }
  ]
}
```

### 設定項目

- `cloudflare_api_token`: CloudflareのAPIトークン
- `cloudflare_zone_id`: 対象のゾーンID
- `check_interval_seconds`: ヘルスチェックの間隔（秒）
- `origins`: モニタリング対象のオリジン設定
  - `name`: DNSレコード名
  - `record_type`: レコードタイプ（"A"または"AAAA"）
  - `health_check`: ヘルスチェック設定
    - `type`: チェックタイプ（"http", "https", "icmp"）
    - `endpoint`: HTTPSの場合のパス（例: "/health"）
    - `host`: HTTPSの場合のホスト名（例: "example.com"）
    - `timeout`: タイムアウト（秒）
  - `priority_failover_ips`: 優先的に使用するフェイルオーバー用IPアドレスのリスト（オプション）
    - 設定した場合、通常はこのIPが使用され、障害が発生した場合のみ通常のフェイルオーバーIPに切り替わります
    - 通常は定額課金のサーバーなど、常に使用したいIPを設定します
  - `failover_ips`: フェイルオーバー用IPアドレスのリスト（オプション）
    - 設定した場合、ヘルスチェック失敗時にこのリストから順番にIPアドレスを使用
    - 設定しない場合、既存IPの最後のオクテット（IPv4）またはセグメント（IPv6）を+1したアドレスを使用
  - `proxied`: Cloudflareのプロキシ機能を有効にするかどうか（オプション、デフォルトはfalse）
    - `true`: DNSレコード更新時にCloudflareのプロキシを有効にする
    - `false`: プロキシを無効にし、直接IPアドレスにアクセスさせる
  - `return_to_priority`: 優先IPが正常に戻った際に自動的に戻すかどうか（オプション、デフォルトはfalse）
    - `true`: 優先IPが正常になった際に自動的に優先IPに戻します
    - `false`: 一度フェイルオーバーすると手動で戻すまで優先IPに戻りません

### フェイルオーバーIPリストの動作

フェイルオーバーIPリストが設定されている場合、以下のように動作します：

1. ヘルスチェックが失敗すると、リストの次のIPアドレスに切り替えます
2. リストの最後まで達した場合、最初のIPからループします
3. 各オリジンごとに独立してIPのローテーションを管理します
4. レコードタイプ（AまたはAAAA）に合わせて、適切なIPタイプかどうかチェックします

### 優先IPとフェイルオーバーIPの活用方法

優先IPとフェイルオーバーIPを組み合わせることで、以下のようなリソース効率化が可能です：

1. 通常時は定額課金（例：専用サーバー）の優先IPに転送
2. 障害発生時のみ従量課金（例：クラウドVM）のフェイルオーバーIPに転送
3. 優先IPが復旧したら自動的に優先IPに戻す（`return_to_priority: true`の場合）

これにより、以下のメリットが得られます：
- 平常時のコスト最適化（定額制リソースを優先使用）
- 障害時の可用性確保（従量課金リソースでバックアップ）
- 復旧時の自動切り戻しによる運用負荷軽減

### プロキシ設定について

各オリジンごとに個別にCloudflareのプロキシ設定を指定できます：

- プロキシ有効時（`"proxied": true`）:
  - トラフィックはCloudflareのネットワークを経由します
  - Cloudflareのセキュリティ保護（WAF、DDoS保護など）が適用されます
  - オリジンサーバーのIPアドレスは隠蔽されます
  - HTTP/2、TLS 1.3などの最新プロトコルが利用可能になります

- プロキシ無効時（`"proxied": false`）:
  - トラフィックは直接オリジンサーバーに送られます
  - Cloudflareのセキュリティ保護は適用されません
  - オリジンサーバーのIPアドレスは公開されます
  - ICMPヘルスチェックを使用する場合や、直接接続が必要な場合に適しています

## 使用方法

```bash
./gslb -config config.json
```

代替の設定ファイルパスを指定することもできます:

```bash
./gslb -config /path/to/your/config.json
```

## テスト

テストを実行するには以下のコマンドを使用します：

```bash
go test ./...
```

詳細な出力を得るには `-v` オプションを追加します：

```bash
go test ./... -v
```

カバレッジレポートを生成するには：

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## 注意事項

- このツールを使用するには、CloudflareのAPIトークンに適切な権限（DNS編集権限）が必要です。
- ICMPヘルスチェックを使用するには、特権が必要な場合があります（多くのシステムではroot権限が必要です）。
- プロキシ機能を有効にする場合、IPアドレスはCloudflareのネットワークを経由するため、特定のプロトコルや設定が制限される場合があります。
- ICMPヘルスチェックを使用する場合は、通常プロキシを無効（`"proxied": false`）にする必要があります。
- 実際の環境で使用する前に、テスト環境でテストすることをお勧めします。
- Cloudflareのプロキシフラグをオフにしている場合でも、フェイルオーバーIPリストを設定しておくことで、柔軟で信頼性の高いフェイルオーバーが可能になります。 