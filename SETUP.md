# Mail System Setup

This setup guide covers:

- Mail system + Portrait OAuth login integration
- Workspace submodule additions for portrait_user_management and reciept_notebook

## 1. Ensure subrepos are present

From workspace root:

```bash
git submodule sync --recursive
git submodule update --init --recursive --remote
```

Expected paths:

- `portrait_user_management`
- `reciept_notebook`

## 2. Deploy Portrait (OAuth provider)

From workspace root on Linux host:

```bash
bash ./scripts/selfdeploy_portrait_ssl_linux.sh pam.meandering.tel admin@meandering.tel
```

This deploys Portrait behind nginx with HTTPS and a systemd service.

## 3. Register mail-system as an OAuth client in Portrait

The Portrait deploy script now auto-seeds this client entry. You only need to edit manually if you want different values.

Edit:

- `/etc/portrait_user_management/oauth_clients.json`

Example:

```json
[
  {
    "client_id": "mail-system",
    "client_secret": "replace-with-a-long-random-secret",
    "name": "Mail System",
    "redirect_uris": [
      "https://mail.meandering.tel/auth/portrait/callback"
    ]
  }
]
```

Notes:

- `redirect_uris` must match exactly.
- Keep `client_secret` private.

## 4. Configure mail-system OAuth

Update one of:

- `mail-system/config.yaml`
- `mail-system/config/server.yaml`

Required block:

```yaml
web:
  port: 8081
  host: "0.0.0.0"
  session_secret: "replace-with-a-long-random-session-secret"
  oauth:
    enabled: true
    client_id: "mail-system"
    client_secret: "replace-with-a-long-random-secret"
    authorize_url: "https://pam.meandering.tel/oauth/authorize"
    token_url: "https://pam.meandering.tel/oauth/token"
    userinfo_url: "https://pam.meandering.tel/oauth/userinfo"
    redirect_url: "https://mail.meandering.tel/auth/portrait/callback"
    scope: "profile"
    default_mailbox_user: "admin@localhost"
```

`default_mailbox_user` controls which mailbox account is used after Portrait login. If empty, a synthetic mailbox username is created from Portrait `sub`.

## 5. Run mail-system

```bash
cd mail-system
go mod tidy
go run main.go -config config.yaml
```

Open the web UI:

- `http://localhost:8081` for local dev

Login options:

- Local username/password
- **Login with Portrait** button (OAuth)

## 6. Optional: expose mail-system via domain

mail-system has no built-in TLS, so `https://mail.meandering.tel` requires an nginx
reverse proxy in front of it with a real certificate. From the workspace root on a
Linux host:

```bash
bash ./scripts/selfdeploy_mail_ssl_linux.sh mail.meandering.tel admin@meandering.tel
```

This builds the Go binary, installs it as the `mail-system.service` systemd unit,
configures nginx to forward `/` (including `/auth/portrait/callback`) to the web
port, and obtains/renews a Let's Encrypt certificate via certbot. Without this (or
an equivalent manually-configured reverse proxy with a valid certificate), browsers
will show a "connection is not private" warning when visiting the site over HTTPS.

## 7. Verify OAuth flow

1. Open mail-system web UI.
2. Click **Login with Portrait**.
3. Choose profile in Portrait consent screen.
4. Confirm redirect back to mail-system and active session.
