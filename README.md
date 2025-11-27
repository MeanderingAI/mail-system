# Go Mail Server

A complete mail server implementation written in Go, supporting SMTP, POP3, IMAP, and a web interface for email management.

## Features

- **SMTP Server**: Send emails with authentication support
- **POP3 Server**: Retrieve emails using POP3 protocol
- **IMAP Server**: Access emails with folder-based organization
- **Web Interface**: Browser-based email client with inbox, compose, and profile management
- **User Authentication**: Secure user management with password verification
- **File-based Storage**: Email storage with JSON metadata
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