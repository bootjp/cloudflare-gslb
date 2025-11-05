# Cloudflare GSLB

A Global Server Load Balancing (GSLB) system that provides health checks and automatic failover for Cloudflare DNS records.

## Features

- Health checks for A and AAAA records
- HTTPS health checks (with customizable paths and hostnames)
- HTTP health checks (with customizable paths and hostnames)
- ICMP health checks
- Automatic DNS record replacement upon anomaly detection
- Configurable check intervals
- Custom failover IP address list configuration
- Cloudflare proxy settings for each origin
- One-shot mode for batch health checks via CLI or Docker container
- **Multiple zone support** - Monitor and manage DNS records across multiple Cloudflare zones
- **Failover notifications** - Send notifications to Slack and Discord webhooks when failover events occur

## Installation

```bash
git clone https://github.com/bootjp/cloudflare-gslb.git
cd cloudflare-gslb
go build -o gslb ./cmd/gslb
```

## Configuration

Copy `config.json.example` to create `config.json` and configure the necessary settings.

```bash
cp config.json.example config.json
```

Example configuration file:

```json
{
  "cloudflare_api_token": "YOUR_CLOUDFLARE_API_TOKEN",
  "check_interval_seconds": 60,
  "cloudflare_zones": [
    {
      "zone_id": "YOUR_CLOUDFLARE_ZONE_ID_1",
      "name": "example.com"
    },
    {
      "zone_id": "YOUR_CLOUDFLARE_ZONE_ID_2",
      "name": "example.org"
    }
  ],
  "notifications": [
    {
      "type": "slack",
      "webhook_url": "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK"
    },
    {
      "type": "discord",
      "webhook_url": "https://discord.com/api/webhooks/YOUR/DISCORD/WEBHOOK"
    }
  ],
  "origins": [
    {
      "name": "www",
      "zone_name": "example.com",
      "record_type": "A",
      "health_check": {
        "type": "https",
        "endpoint": "/health",
        "host": "www.example.com",
        "timeout": 5
      },
      "priority_failover_ips": [
        "192.168.1.1"
      ],
      "failover_ips": [
        "192.168.1.2",
        "192.168.1.3",
        "192.168.1.4"
      ],
      "proxied": true,
      "return_to_priority": true
    },
    {
      "name": "api",
      "zone_name": "example.com",
      "record_type": "A",
      "health_check": {
        "type": "http",
        "endpoint": "/status",
        "host": "api.example.com",
        "timeout": 5
      },
      "priority_failover_ips": [
        "10.0.0.1"
      ],
      "failover_ips": [
        "10.0.0.2",
        "10.0.0.3"
      ],
      "proxied": true,
      "return_to_priority": true
    },
    {
      "name": "ipv6",
      "zone_name": "example.org",
      "record_type": "AAAA",
      "health_check": {
        "type": "icmp",
        "timeout": 5
      },
      "priority_failover_ips": [
        "2001:db8::1"
      ],
      "failover_ips": [
        "2001:db8::2",
        "2001:db8::3",
        "2001:db8::4"
      ],
      "proxied": false,
      "return_to_priority": true
    }
  ]
}
```

### Configuration Options

- `cloudflare_api_token`: Cloudflare API token
- `check_interval_seconds`: Health check interval (in seconds)
- `cloudflare_zones`: Array of Cloudflare zones to manage
  - `zone_id`: Cloudflare zone ID
  - `name`: A name to identify this zone (used in `zone_name` field of origins)
- `notifications` (optional): Array of notification configurations for failover events
  - `type`: Notification type (`slack` or `discord`)
  - `webhook_url`: Webhook URL for the notification service
- `origins`: Array of origin configurations
  - `name`: DNS record name (without the zone part)
  - `zone_name`: The name of the zone this record belongs to (must match one of the names in `cloudflare_zones`)
  - `record_type`: DNS record type (`A` or `AAAA`)
  - `health_check`: Health check configuration
    - `type`: Health check type (`http`, `https`, or `icmp`)
    - `endpoint`: HTTP/HTTPS endpoint path
    - `host`: HTTP/HTTPS host header
    - `timeout`: Health check timeout in seconds
    - `insecure_skip_verify`: Skip TLS verification for HTTPS checks
    - `headers`: Additional HTTP headers to include with health check requests
  - `priority_failover_ips`: Primary IP addresses to use when healthy
  - `failover_ips`: Backup IP addresses to use when priority IPs are unhealthy
  - `proxied`: Whether to enable Cloudflare proxy for this record
  - `return_to_priority`: Whether to return to priority IPs when they become healthy again

### Backward Compatibility

For backward compatibility, you can still use the old configuration format with a single zone:

```json
{
  "cloudflare_api_token": "YOUR_CLOUDFLARE_API_TOKEN",
  "cloudflare_zone_id": "YOUR_CLOUDFLARE_ZONE_ID",
  "check_interval_seconds": 60,
  "origins": [
    ...
  ]
}
```

When using the old format, all origins will be associated with the single zone specified by `cloudflare_zone_id`.

### Failover IP List Behavior

When a failover IP list is configured, it operates as follows:

1. When a health check fails, it switches to the next IP address in the list
2. If it reaches the end of the list, it loops back to the first IP
3. IP rotation is managed independently for each origin
4. It checks whether the IP type is appropriate for the record type (A or AAAA)

### Utilizing Priority IPs and Failover IPs

By combining priority IPs and failover IPs, you can optimize resource efficiency as follows:

1. During normal operation, traffic is directed to priority IPs (e.g., dedicated servers with fixed pricing)
2. During outages, traffic is directed to failover IPs (e.g., cloud VMs with pay-as-you-go pricing)
3. When the priority IP recovers, traffic automatically returns to it (if `return_to_priority: true`)

This approach offers the following benefits:
- Cost optimization during normal operation (prioritizing fixed-cost resources)
- Availability assurance during outages (backup with pay-as-you-go resources)
- Reduced operational burden with automatic failback upon recovery

### About Proxy Settings

You can specify Cloudflare proxy settings individually for each origin:

- With proxy enabled (`"proxied": true`):
  - Traffic passes through Cloudflare's network
  - Cloudflare security protections (WAF, DDoS protection, etc.) are applied
  - The origin server's IP address is masked
  - Modern protocols like HTTP/2 and TLS 1.3 become available

- With proxy disabled (`"proxied": false`):
  - Traffic is sent directly to the origin server
  - Cloudflare security protections are not applied
  - The origin server's IP address is exposed
  - Suitable when using ICMP health checks or when direct connections are required

### Notifications

Cloudflare GSLB supports sending notifications when failover events occur. This feature helps you stay informed about infrastructure health and failover activities in real-time.

#### Supported Notification Services

- **Slack**: Send notifications to Slack channels via webhook
- **Discord**: Send notifications to Discord channels via webhook

#### Setting Up Notifications

##### Slack

1. Create a Slack webhook URL:
   - Go to your Slack workspace settings
   - Navigate to "Apps" → "Incoming Webhooks"
   - Create a new webhook and select the channel
   - Copy the webhook URL

2. Add the webhook URL to your `config.json`:
   ```json
   "notifications": [
     {
       "type": "slack",
       "webhook_url": "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK"
     }
   ]
   ```

##### Discord

1. Create a Discord webhook URL:
   - Open your Discord server settings
   - Navigate to "Integrations" → "Webhooks"
   - Create a new webhook and select the channel
   - Copy the webhook URL

2. Add the webhook URL to your `config.json`:
   ```json
   "notifications": [
     {
       "type": "discord",
       "webhook_url": "https://discord.com/api/webhooks/YOUR/DISCORD/WEBHOOK"
     }
   ]
   ```

#### Multiple Notification Channels

You can configure multiple notification channels simultaneously. The system will send notifications to all configured channels:

```json
"notifications": [
  {
    "type": "slack",
    "webhook_url": "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK"
  },
  {
    "type": "discord",
    "webhook_url": "https://discord.com/api/webhooks/YOUR/DISCORD/WEBHOOK"
  }
]
```

#### Notification Events

Notifications are sent for the following events:

- **Failover to Backup IP**: When a health check fails and the system switches to a backup IP
- **Failover to Priority IP**: When switching from a backup IP to a priority IP
- **Recovery (Return to Priority)**: When a priority IP becomes healthy again and the system returns to it

Each notification includes:
- Origin name and zone
- Record type (A or AAAA)
- Old IP address
- New IP address
- Event type
- Reason for the failover
- Timestamp

## Usage

```bash
./gslb -config config.json
```

You can also specify an alternative configuration file path:

```bash
./gslb -config /path/to/your/config.json
```

### One-shot Mode

One-shot mode performs health checks and necessary failovers once without running continuously:

```bash
./cloudflare-gslb-oneshot -config config.json
```

This is useful for:
- Running health checks via cron jobs
- Batch processing in CI/CD pipelines
- Kubernetes CronJobs
- Testing configuration

### Docker Usage

The application is available as Docker images for both continuous and one-shot modes:

#### Continuous Mode

```bash
docker run -v /path/to/your/config.json:/app/config/config.json ghcr.io/bootjp/cloudflare-gslb:main
```

#### One-shot Mode

```bash
docker run -v /path/to/your/config.json:/app/config/config.json ghcr.io/bootjp/cloudflare-gslb-oneshot:main
```

Both images support multiple architectures (amd64/x86_64 and arm64) automatically.

## Testing

To run tests, use the following command:

```bash
go test ./...
```

For detailed output, add the `-v` option:

```bash
go test ./... -v
```

To generate a coverage report:

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Important Notes

- This tool requires a Cloudflare API token with appropriate permissions (DNS editing permissions).
- ICMP health checks may require privileges (often root permissions on many systems).
- When the proxy feature is enabled, IP addresses will route through Cloudflare's network, which may restrict certain protocols or configurations.
- It is recommended to test in a testing environment before using in a production environment.
- Even if you have Cloudflare's proxy flag turned off, configuring a failover IP list enables flexible and reliable failover.

## Limitations of Cloudflare DNS Round Robin

While Cloudflare advertises DNS Round Robin as a "zero-downtime" solution, it's important to note a significant limitation: **when using Cloudflare Proxy (orange cloud), DNS Round Robin does not properly failover in case of server failures**.

When a server fails behind Cloudflare Proxy:
1. The DNS Round Robin continues to include the failed server's IP in rotation
2. Cloudflare's proxy attempts to connect to the failed server
3. Users experience connection failures or timeouts when their requests are routed to the failed server

This occurs because the proxy layer masks the actual server failures from the DNS layer. To achieve true zero-downtime with Cloudflare services, consider using:
- This GSLB solution, which actively monitors servers and updates DNS records
- Cloudflare Load Balancers (a paid service that properly handles failover)
- Server-side health checks with proper error handling

If you must use DNS Round Robin with proxied records, implement additional client-side retry logic to handle potential failures. 
