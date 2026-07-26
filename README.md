# Go Mail Server

A complete mail server implementation written in Go, supporting SMTP, POP3, IMAP, and a web interface for email management.

## Features

- **SMTP Server**: Send emails with authentication support
- **POP3 Server**: Retrieve emails using POP3 protocol
- **IMAP Server**: Access emails with folder-based organization
- **Web Interface**: Browser-based email client with inbox, compose, and profile management
- **Portrait OAuth Login**: Optional "Login with Portrait" flow for web sign-in
- **User Authentication**: Secure user management with password verification
- **File-based Storage**: Email storage with JSON metadata
- **Outbound Delivery Queue**: Durable queue with retry/backoff and dead-letter handling
- **Policy Controls**: Domain blacklist checks and per-sender rate throttling
- **TLS Material Validation**: Optional certificate/key validation for self-hosted TLS paths
- **DNS Policy Checks**: Optional MX/A reachability and DNSBL lookups via your DNS server
- **Graceful Shutdown**: Proper server lifecycle management

## Requirements

- Go 1.19 or higher
- No external dependencies required (uses standard library + gorilla packages)

## Installation

1. Clone or download the project
2. Install dependencies:
   ```bash
   go mod tidy
   ```

## Configuration

For a complete setup including Portrait OAuth and workspace subrepos, see `SETUP.md`.

The server uses a YAML configuration file. Create a `config.yaml` file in the project root:

```yaml
smtp:
  port: 3025
  host: "0.0.0.0"
  domain: "localhost"
  max_connections: 100
  require_auth: true

pop3:
  port: 3110
  host: "0.0.0.0"
  max_connections: 50

imap:
  port: 3143
  host: "0.0.0.0"
  max_connections: 50

web:
  port: 8081
  host: "0.0.0.0"

delivery:
  queue_directory: "data/outbound-queue"
  worker_count: 1
  poll_interval_millis: 1000
  max_attempts: 6
  base_retry_delay_seconds: 5
  max_retry_delay_seconds: 300
  blocked_sender_domains: []
  blocked_recipient_domains: []
  per_sender_rate_limit_per_minute: 120
  enable_tls_cert_validation: false
  tls_cert_file: ""
  tls_key_file: ""
  enable_dns_policy_checks: false
  dns_server_address: ""
  mx_dns_server_address: ""
  dnsbl_dns_server_address: ""
  use_system_resolver_for_dnsbl: true
  dns_lookup_timeout_seconds: 3
  require_reachable_mx: false
  dnsbl_zones: []
  skip_dns_policy_for_domains:
    - "localhost"
    - "local"

users:
  user1@localhost:
    password: "password1"
    full_name: "User One"
  admin@localhost:
    password: "admin123"
    full_name: "Administrator"

security:
  allowed_domains:
    - "localhost"
  max_message_size: 10485760
  require_auth: true

storage:
  data_directory: "data"
  log_directory: "logs"
```

## Usage

### Split DNS Resolver Mode

When `delivery.enable_dns_policy_checks` is enabled, you can direct lookup types to different resolvers:

- `mx_dns_server_address`: resolver for recipient MX/A reachability checks (for example your local dns-server)
- `dnsbl_dns_server_address`: resolver for DNSBL zone lookups
- `use_system_resolver_for_dnsbl`: when true and `dnsbl_dns_server_address` is empty, DNSBL uses the host resolver

If these are empty, the service falls back to `dns_server_address`, then the system resolver.

### Starting the Server

```bash
# Run with default config
go run main.go

# Run with custom config file
go run main.go -config config.yaml

# Build and run
go build -o mail-server
./mail-server -config config.yaml
```

### Building for Production

```bash
# Build optimized binary
go build -ldflags="-s -w" -o mail-server main.go
```

### Accessing the Services

- **Web Interface**: http://localhost:8081
- **SMTP**: Connect to localhost:3025
- **POP3**: Connect to localhost:3110  
- **IMAP**: Connect to localhost:3143

### Default Users

- Username: `admin@localhost`, Password: `admin123`
- Username: `user1@localhost`, Password: `password1`

## Architecture

```
internal/
├── auth/          # User authentication
├── config/        # Configuration management
├── imap/          # IMAP protocol implementation
├── pop3/          # POP3 protocol implementation
├── smtp/          # SMTP protocol implementation
├── storage/       # Email storage system
└── web/           # Web interface server
```

## Email Client Configuration

### Thunderbird/Outlook Configuration

**Incoming Mail (POP3):**
- Server: localhost
- Port: 3110
- Security: None
- Authentication: Normal password

**Incoming Mail (IMAP):**
- Server: localhost
- Port: 3143
- Security: None
- Authentication: Normal password

**Outgoing Mail (SMTP):**
- Server: localhost
- Port: 3025
- Security: None
- Authentication: Normal password

## Development

### Project Structure

- `main.go`: Server entry point with graceful shutdown
- `internal/config/`: YAML configuration loading
- `internal/auth/`: User authentication and password verification  
- `internal/storage/`: File-based email storage with JSON metadata
- `internal/delivery/`: Queue worker for retryable outbound delivery
- `internal/smtp/`: SMTP protocol implementation (RFC 5321)
- `internal/pop3/`: POP3 protocol implementation (RFC 1939)
- `internal/imap/`: Basic IMAP protocol implementation (RFC 3501)
- `internal/web/`: HTTP web server with REST API

### Adding New Features

1. **New Protocols**: Add new packages under `internal/`
2. **Storage Backends**: Implement storage.Storage interface
3. **Authentication**: Extend auth.Authenticator interface
4. **Web Features**: Add routes to web.Server

## Testing

```bash
# Run tests
go test ./...

# Test with verbose output
go test -v ./...

# Test individual packages
go test ./internal/auth
go test ./internal/storage
```

## Security Notes

- This is a development/learning mail server
- Uses plain text authentication
- No TLS/SSL encryption implemented
- File-based storage is not optimized for high volume
- Suitable for local development and testing

## License

MIT License - feel free to use and modify as needed.