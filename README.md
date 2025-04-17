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
      "name": "api.example.com",
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
      "name": "ipv6.example.com",
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
- `cloudflare_zone_id`: Target zone ID
- `check_interval_seconds`: Health check interval (in seconds)
- `origins`: Configuration for origins to monitor
  - `name`: DNS record name
  - `record_type`: Record type ("A" or "AAAA")
  - `health_check`: Health check configuration
    - `type`: Check type ("http", "https", "icmp")
    - `endpoint`: Path for HTTPS/HTTP (e.g., "/health")
    - `host`: Hostname for HTTPS/HTTP (e.g., "example.com")
    - `timeout`: Timeout (in seconds)
    - `insecure_skip_verify`: Skip SSL verification for HTTPS (optional, default is false)
  - `priority_failover_ips`: List of priority failover IP addresses
    - These IPs are used normally and switch to regular failover IPs only when failures occur
    - Typically used for fixed-cost servers that you want to use consistently
  - `failover_ips`: List of failover IP addresses
    - When configured, IPs from this list are used in sequence if health checks fail
    - If not configured, an IP with the last octet (IPv4) or segment (IPv6) incremented by 1 is used
  - `proxied`: Whether to enable Cloudflare's proxy feature (optional, default is false)
    - `true`: Enable Cloudflare proxy when updating DNS records
    - `false`: Disable proxy, allowing direct access to IP addresses
  - `return_to_priority`: Whether to automatically return to priority IP when it recovers (optional, default is false)
    - `true`: Automatically returns to the priority IP when it becomes healthy
    - `false`: Once failover occurs, it won't return to the priority IP until manually reset

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
docker run -v /path/to/your/config.json:/app/config/config.json ghcr.io/bootjp/cloudflare-gslb-x86:main
```

#### One-shot Mode

```bash
docker run -v /path/to/your/config.json:/app/config/config.json ghcr.io/bootjp/cloudflare-gslb-oneshot-x86:main
```

ARM64 images are also available with the `-arm` suffix.

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
