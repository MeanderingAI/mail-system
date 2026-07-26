package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mail-server/internal/auth"
	"mail-server/internal/config"
	"mail-server/internal/delivery"
	"mail-server/internal/storage"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

// Server represents a web server
type Server struct {
	config        config.WebConfig
	authenticator *auth.Authenticator
	storage       *storage.Storage
	delivery      *delivery.Service
	store         *sessions.CookieStore
	server        *http.Server
}

// NewServer creates a new web server
func NewServer(cfg config.WebConfig, users map[string]config.User, store *storage.Storage, deliveryService *delivery.Service) *Server {
	// Initialize authenticator
	authenticator := auth.NewAuthenticator(users)
	sessionSecret := cfg.SessionSecret
	if strings.TrimSpace(sessionSecret) == "" {
		sessionSecret = "replace-with-a-long-random-session-secret"
	}

	// Initialize session store
	sessionStore := sessions.NewCookieStore([]byte(sessionSecret))
	sessionStore.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	}

	return &Server{
		config:        cfg,
		authenticator: authenticator,
		storage:       store,
		delivery:      deliveryService,
		store:         sessionStore,
	}
}

// Start starts the web server
func (s *Server) Start(ctx context.Context) error {
	router := mux.NewRouter()

	// Static files
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("web/static/"))))

	// Web interface
	router.HandleFunc("/favicon.svg", handleFavicon).Methods("GET")
	router.HandleFunc("/", s.handleIndex).Methods("GET")
	router.HandleFunc("/auth/portrait/login", s.handlePortraitLogin).Methods("GET")
	router.HandleFunc("/auth/portrait/callback", s.handlePortraitCallback).Methods("GET")

	// API routes
	api := router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/auth/login", s.handleLogin).Methods("POST")
	api.HandleFunc("/auth/logout", s.handleLogout).Methods("POST")
	api.HandleFunc("/auth/session", s.handleSession).Methods("GET")

	// Protected API routes
	protected := api.PathPrefix("").Subrouter()
	protected.Use(s.authMiddleware)
	protected.HandleFunc("/mail/inbox", s.handleInbox).Methods("GET")
	protected.HandleFunc("/mail/message/{id}", s.handleGetMessage).Methods("GET")
	protected.HandleFunc("/mail/message/{id}", s.handleDeleteMessage).Methods("DELETE")
	protected.HandleFunc("/mail/send", s.handleSendMail).Methods("POST")
	protected.HandleFunc("/users/profile", s.handleGetProfile).Methods("GET")
	protected.HandleFunc("/users/profile", s.handleUpdateProfile).Methods("PUT")

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: router,
	}

	log.Printf("Web server listening on http://%s", addr)

	// Start server in a goroutine
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Web server error: %v", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Shutdown server gracefully
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.server.Shutdown(shutdownCtx)
}

// handleIndex serves the main web interface
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Mail Server - Web Client</title>
	<link rel="icon" type="image/svg+xml" href="/favicon.svg">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; line-height: 1.6; color: #333; background-color: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
        .header { background: #007bff; color: white; padding: 1rem; border-radius: 8px; margin-bottom: 20px; display: flex; justify-content: space-between; align-items: center; }
        .login-form { background: white; padding: 2rem; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); max-width: 400px; margin: 50px auto; }
        .mail-interface { display: none; grid-template-columns: 300px 1fr; gap: 20px; height: 80vh; }
        .sidebar { background: white; border-radius: 8px; padding: 20px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .main-content { background: white; border-radius: 8px; padding: 20px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); overflow-y: auto; }
        .btn { background: #007bff; color: white; border: none; padding: 10px 20px; border-radius: 4px; cursor: pointer; font-size: 14px; text-decoration: none; display: inline-block; }
        .btn:hover { background: #0056b3; }
        .btn-danger { background: #dc3545; }
        .btn-danger:hover { background: #c82333; }
        .form-group { margin-bottom: 15px; }
        .form-group label { display: block; margin-bottom: 5px; font-weight: bold; }
        .form-group input, .form-group textarea { width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 4px; font-size: 14px; }
        .form-group textarea { height: 100px; resize: vertical; }
        .message-list { border: 1px solid #ddd; border-radius: 4px; max-height: 60vh; overflow-y: auto; }
        .message-item { padding: 10px; border-bottom: 1px solid #eee; cursor: pointer; }
        .message-item:hover { background: #f8f9fa; }
        .message-item.selected { background: #e3f2fd; }
        .message-from { font-weight: bold; margin-bottom: 5px; }
        .message-subject { margin-bottom: 5px; }
        .message-preview { color: #666; font-size: 12px; }
        .message-date { font-size: 12px; color: #999; float: right; }
        .message-view { border: 1px solid #ddd; border-radius: 4px; padding: 20px; background: white; }
        .message-header { border-bottom: 1px solid #eee; padding-bottom: 15px; margin-bottom: 15px; }
        .message-body { white-space: pre-wrap; line-height: 1.6; }
        .compose-form { display: none; }
        .alert { padding: 10px; margin-bottom: 15px; border-radius: 4px; }
        .alert-success { background: #d4edda; color: #155724; border: 1px solid #c3e6cb; }
        .alert-error { background: #f8d7da; color: #721c24; border: 1px solid #f5c6cb; }
        .hidden { display: none !important; }
        .nav-item { padding: 10px; cursor: pointer; border-radius: 4px; margin-bottom: 5px; }
        .nav-item:hover { background: #f8f9fa; }
        .nav-item.active { background: #007bff; color: white; }
        .toolbar { margin-bottom: 20px; display: flex; gap: 10px; align-items: center; }
        #logout-btn { background: none; border: none; color: white; cursor: pointer; font-size: 14px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Mail Server</h1>
            <div>
                <span id="user-info"></span>
                <button id="logout-btn" style="display: none;">Logout</button>
            </div>
        </div>

        <!-- Login Form -->
        <div id="login-section">
            <form class="login-form" id="login-form">
                <h2>Login</h2>
                <div id="login-alert"></div>
                <div class="form-group">
                    <label for="username">Username:</label>
                    <input type="text" id="username" name="username" required value="admin@localhost">
                </div>
                <div class="form-group">
                    <label for="password">Password:</label>
                    <input type="password" id="password" name="password" required value="admin123">
                </div>
                <button type="submit" class="btn">Login</button>
                {{if .OAuthEnabled}}
                <div style="margin: 18px 0; text-align: center; color: #666;">or</div>
                <a class="btn" href="/auth/portrait/login" style="width: 100%; text-align: center;">Login with Portrait</a>
                {{end}}
            </form>
        </div>

        <!-- Mail Interface -->
        <div id="mail-interface" class="mail-interface">
            <div class="sidebar">
                <div class="nav-item active" data-view="inbox">📥 Inbox</div>
                <div class="nav-item" data-view="compose">✉️ Compose</div>
                <div class="nav-item" data-view="profile">👤 Profile</div>
            </div>

            <div class="main-content">
                <!-- Inbox View -->
                <div id="inbox-view">
                    <div class="toolbar">
                        <button class="btn" onclick="loadInbox()">🔄 Refresh</button>
                        <button class="btn btn-danger" onclick="deleteSelected()" id="delete-btn" disabled>🗑️ Delete</button>
                    </div>
                    <div id="message-alert"></div>
                    <div class="message-list" id="message-list">
                        <div style="padding: 20px; text-align: center; color: #666;">Loading messages...</div>
                    </div>
                    <div id="message-view" class="message-view" style="display: none; margin-top: 20px;">
                        <div class="message-header">
                            <h3 id="msg-subject"></h3>
                            <p><strong>From:</strong> <span id="msg-from"></span></p>
                            <p><strong>To:</strong> <span id="msg-to"></span></p>
                            <p><strong>Date:</strong> <span id="msg-date"></span></p>
                        </div>
                        <div class="message-body" id="msg-body"></div>
                    </div>
                </div>

                <!-- Compose View -->
                <div id="compose-view" class="compose-form">
                    <h2>Compose Email</h2>
                    <div id="compose-alert"></div>
                    <form id="compose-form">
                        <div class="form-group">
                            <label for="compose-to">To:</label>
                            <input type="email" id="compose-to" name="to" required>
                        </div>
                        <div class="form-group">
                            <label for="compose-subject">Subject:</label>
                            <input type="text" id="compose-subject" name="subject" required>
                        </div>
                        <div class="form-group">
                            <label for="compose-body">Message:</label>
                            <textarea id="compose-body" name="body" rows="10" required></textarea>
                        </div>
                        <button type="submit" class="btn">Send Email</button>
                    </form>
                </div>

                <!-- Profile View -->
                <div id="profile-view" class="hidden">
                    <h2>Profile</h2>
                    <div id="profile-alert"></div>
                    <form id="profile-form">
                        <div class="form-group">
                            <label for="profile-username">Username:</label>
                            <input type="text" id="profile-username" readonly>
                        </div>
                        <div class="form-group">
                            <label for="profile-fullname">Full Name:</label>
                            <input type="text" id="profile-fullname" name="fullName">
                        </div>
                        <div class="form-group">
                            <label for="profile-password">New Password (leave blank to keep current):</label>
                            <input type="password" id="profile-password" name="password">
                        </div>
                        <button type="submit" class="btn">Update Profile</button>
                    </form>
                </div>
            </div>
        </div>
    </div>

    <script>
        let currentUser = null;
        let messages = [];
        let selectedMessageId = null;

        document.addEventListener('DOMContentLoaded', function() {
            setupEventListeners();
            checkSession();
        });

        async function checkSession() {
            try {
                const response = await fetch('/api/auth/session');
                if (!response.ok) {
                    showLogin();
                    return;
                }

                const data = await response.json();
                currentUser = data.user;
                showMailInterface();
                loadProfile();
                loadInbox();
            } catch (err) {
                showLogin();
            }
        }

        function setupEventListeners() {
            document.getElementById('login-form').addEventListener('submit', handleLogin);
            document.getElementById('logout-btn').addEventListener('click', handleLogout);
            
            document.querySelectorAll('.nav-item').forEach(item => {
                item.addEventListener('click', function() { showView(this.dataset.view); });
            });
            
            document.getElementById('compose-form').addEventListener('submit', handleCompose);
            document.getElementById('profile-form').addEventListener('submit', handleProfile);
        }

        async function handleLogin(e) {
            e.preventDefault();
            const formData = new FormData(e.target);
            
            try {
                const response = await fetch('/api/auth/login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        username: formData.get('username'),
                        password: formData.get('password')
                    })
                });
                
                const data = await response.json();
                
                if (response.ok) {
                    currentUser = data.user;
                    showMailInterface();
                    loadProfile();
                    loadInbox();
                } else {
                    showAlert('login-alert', data.error, 'error');
                }
            } catch (err) {
                showAlert('login-alert', 'Login failed', 'error');
            }
        }

        function handleLogout() {
            fetch('/api/auth/logout', { method: 'POST' });
            currentUser = null;
            showLogin();
        }

        function showLogin() {
            document.getElementById('login-section').style.display = 'block';
            document.getElementById('mail-interface').style.display = 'none';
            document.getElementById('logout-btn').style.display = 'none';
        }

        function showMailInterface() {
            document.getElementById('login-section').style.display = 'none';
            document.getElementById('mail-interface').style.display = 'grid';
            document.getElementById('logout-btn').style.display = 'inline';
            document.getElementById('user-info').textContent = currentUser ? currentUser.fullName : '';
        }

        function showView(view) {
            document.querySelectorAll('.nav-item').forEach(item => {
                item.classList.toggle('active', item.dataset.view === view);
            });
            
            document.getElementById('inbox-view').style.display = view === 'inbox' ? 'block' : 'none';
            document.getElementById('compose-view').style.display = view === 'compose' ? 'block' : 'none';
            document.getElementById('profile-view').classList.toggle('hidden', view !== 'profile');
        }

        async function loadInbox() {
            try {
                const response = await fetch('/api/mail/inbox');
                const data = await response.json();
                
                if (response.ok) {
                    messages = data.messages || [];
                    displayMessages();
                } else {
                    showAlert('message-alert', data.error, 'error');
                }
            } catch (err) {
                showAlert('message-alert', 'Failed to load inbox', 'error');
            }
        }

        function displayMessages() {
            const messageList = document.getElementById('message-list');
            
            if (messages.length === 0) {
                messageList.innerHTML = '<div style="padding: 20px; text-align: center; color: #666;">No messages</div>';
                return;
            }
            
            messageList.innerHTML = messages.map(msg => 
                '<div class="message-item" data-id="' + msg.id + '" onclick="selectMessage(\'' + msg.id + '\')">' +
                    '<div class="message-date">' + new Date(msg.timestamp).toLocaleString() + '</div>' +
                    '<div class="message-from">' + msg.from + '</div>' +
                    '<div class="message-subject">' + (msg.subject || '(No Subject)') + '</div>' +
                    '<div class="message-preview">' + (msg.body ? msg.body.substring(0, 100) : '') + '</div>' +
                '</div>'
            ).join('');
        }

        async function selectMessage(messageId) {
            document.querySelectorAll('.message-item').forEach(item => {
                item.classList.toggle('selected', item.dataset.id === messageId);
            });
            
            selectedMessageId = messageId;
            document.getElementById('delete-btn').disabled = false;
            
            try {
                const response = await fetch('/api/mail/message/' + messageId);
                const data = await response.json();
                
                if (response.ok) {
                    displayMessage(data);
                } else {
                    showAlert('message-alert', data.error, 'error');
                }
            } catch (err) {
                showAlert('message-alert', 'Failed to load message', 'error');
            }
        }

        function displayMessage(message) {
            document.getElementById('msg-subject').textContent = message.subject || '(No Subject)';
            document.getElementById('msg-from').textContent = message.from;
            document.getElementById('msg-to').textContent = message.to ? message.to.join(', ') : '';
            document.getElementById('msg-date').textContent = new Date(message.timestamp).toLocaleString();
            document.getElementById('msg-body').textContent = message.body;
            document.getElementById('message-view').style.display = 'block';
        }

        async function deleteSelected() {
            if (!selectedMessageId) return;
            if (!confirm('Are you sure you want to delete this message?')) return;
            
            try {
                const response = await fetch('/api/mail/message/' + selectedMessageId, { method: 'DELETE' });
                
                if (response.ok) {
                    showAlert('message-alert', 'Message deleted successfully', 'success');
                    loadInbox();
                    document.getElementById('message-view').style.display = 'none';
                    document.getElementById('delete-btn').disabled = true;
                    selectedMessageId = null;
                } else {
                    const data = await response.json();
                    showAlert('message-alert', data.error, 'error');
                }
            } catch (err) {
                showAlert('message-alert', 'Failed to delete message', 'error');
            }
        }

        async function handleCompose(e) {
            e.preventDefault();
            const formData = new FormData(e.target);
            
            try {
                const response = await fetch('/api/mail/send', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        to: formData.get('to'),
                        subject: formData.get('subject'),
                        body: formData.get('body')
                    })
                });
                
                const data = await response.json();
                
                if (response.ok) {
                    showAlert('compose-alert', 'Email sent successfully!', 'success');
                    e.target.reset();
                } else {
                    showAlert('compose-alert', data.error, 'error');
                }
            } catch (err) {
                showAlert('compose-alert', 'Failed to send email', 'error');
            }
        }

        async function loadProfile() {
            try {
                const response = await fetch('/api/users/profile');
                const data = await response.json();
                
                if (response.ok) {
                    document.getElementById('profile-username').value = data.username;
                    document.getElementById('profile-fullname').value = data.fullName;
                }
            } catch (err) {
                console.error('Failed to load profile:', err);
            }
        }

        async function handleProfile(e) {
            e.preventDefault();
            const formData = new FormData(e.target);
            
            const updateData = {};
            if (formData.get('fullName')) updateData.fullName = formData.get('fullName');
            if (formData.get('password')) updateData.password = formData.get('password');
            
            try {
                const response = await fetch('/api/users/profile', {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(updateData)
                });
                
                const data = await response.json();
                
                if (response.ok) {
                    showAlert('profile-alert', 'Profile updated successfully!', 'success');
                    document.getElementById('profile-password').value = '';
                } else {
                    showAlert('profile-alert', data.error, 'error');
                }
            } catch (err) {
                showAlert('profile-alert', 'Failed to update profile', 'error');
            }
        }

        function showAlert(containerId, message, type) {
            const container = document.getElementById(containerId);
            container.innerHTML = '<div class="alert alert-' + type + '">' + message + '</div>';
            setTimeout(() => { container.innerHTML = ''; }, 5000);
        }
    </script>
</body>
</html>`

	t, err := template.New("index").Parse(tmpl)
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	t.Execute(w, map[string]bool{"OAuthEnabled": s.oauthEnabled()})
}

func handleFavicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
	<rect width="64" height="64" rx="12" fill="#00b8f0"/>
	<rect x="9" y="16" width="46" height="34" rx="6" fill="#fff"/>
	<path d="M11 20l21 17 21-17v8L32 45 11 28z" fill="#ffe135"/>
	<circle cx="50" cy="15" r="9" fill="#ff2d8f"/>
	<path d="M46 15h8M50 11v8" stroke="#fff" stroke-width="3" stroke-linecap="round"/>
</svg>`))
}

// authMiddleware checks for valid session
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, _ := s.store.Get(r, "mail-session")

		username, ok := session.Values["username"].(string)
		if !ok || username == "" {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		// Add username to request context for handlers to use
		r = r.WithContext(context.WithValue(r.Context(), "username", username))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) oauthEnabled() bool {
	return s.config.OAuth.Enabled &&
		s.config.OAuth.ClientID != "" &&
		s.config.OAuth.AuthorizeURL != "" &&
		s.config.OAuth.TokenURL != "" &&
		s.config.OAuth.UserInfoURL != "" &&
		s.config.OAuth.RedirectURL != ""
}

// handleSession returns the currently authenticated session user.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	session, _ := s.store.Get(r, "mail-session")
	username, ok := session.Values["username"].(string)
	if !ok || username == "" {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fullName, _ := session.Values["fullName"].(string)
	if fullName == "" {
		if user, exists := s.authenticator.GetUser(username); exists {
			fullName = user.FullName
		} else {
			fullName = username
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user": map[string]string{
			"username": username,
			"fullName": fullName,
		},
	})
}

// handlePortraitLogin starts OAuth authorization with portrait_user_management.
func (s *Server) handlePortraitLogin(w http.ResponseWriter, r *http.Request) {
	if !s.oauthEnabled() {
		http.Error(w, "Portrait OAuth is not configured", http.StatusNotFound)
		return
	}

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		http.Error(w, "Failed to start OAuth login", http.StatusInternalServerError)
		return
	}
	state := hex.EncodeToString(stateBytes)

	session, _ := s.store.Get(r, "mail-session")
	session.Values["oauth_state"] = state
	if err := session.Save(r, w); err != nil {
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	scope := strings.TrimSpace(s.config.OAuth.Scope)
	if scope == "" {
		scope = "profile"
	}

	authorizeURL, err := url.Parse(s.config.OAuth.AuthorizeURL)
	if err != nil {
		http.Error(w, "Invalid authorize URL", http.StatusInternalServerError)
		return
	}

	query := authorizeURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", s.config.OAuth.ClientID)
	query.Set("redirect_uri", s.config.OAuth.RedirectURL)
	query.Set("scope", scope)
	query.Set("state", state)
	authorizeURL.RawQuery = query.Encode()

	http.Redirect(w, r, authorizeURL.String(), http.StatusFound)
}

// handlePortraitCallback exchanges the OAuth code and creates a mail session.
func (s *Server) handlePortraitCallback(w http.ResponseWriter, r *http.Request) {
	if !s.oauthEnabled() {
		http.Error(w, "Portrait OAuth is not configured", http.StatusNotFound)
		return
	}

	if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
		http.Error(w, fmt.Sprintf("OAuth failed: %s", oauthErr), http.StatusUnauthorized)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Error(w, "Missing OAuth callback parameters", http.StatusBadRequest)
		return
	}

	session, _ := s.store.Get(r, "mail-session")
	expectedState, _ := session.Values["oauth_state"].(string)
	if expectedState == "" || expectedState != state {
		http.Error(w, "Invalid OAuth state", http.StatusUnauthorized)
		return
	}

	accessToken, err := s.exchangePortraitCode(code)
	if err != nil {
		http.Error(w, fmt.Sprintf("OAuth token exchange failed: %v", err), http.StatusUnauthorized)
		return
	}

	username, fullName, err := s.fetchPortraitIdentity(accessToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load profile: %v", err), http.StatusUnauthorized)
		return
	}

	session.Values["username"] = username
	session.Values["fullName"] = fullName
	delete(session.Values, "oauth_state")
	if err := session.Save(r, w); err != nil {
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) exchangePortraitCode(code string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", s.config.OAuth.RedirectURL)

	if s.config.OAuth.ClientSecret == "" {
		form.Set("client_id", s.config.OAuth.ClientID)
	}

	req, err := http.NewRequest(http.MethodPost, s.config.OAuth.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if s.config.OAuth.ClientSecret != "" {
		credentials := s.config.OAuth.ClientID + ":" + s.config.OAuth.ClientSecret
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credentials)))
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token response missing access_token")
	}

	return tokenResp.AccessToken, nil
}

func (s *Server) fetchPortraitIdentity(accessToken string) (string, string, error) {
	req, err := http.NewRequest(http.MethodGet, s.config.OAuth.UserInfoURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("userinfo endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var userInfo struct {
		Subject string `json:"sub"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return "", "", err
	}

	username := strings.TrimSpace(s.config.OAuth.DefaultMailboxUser)
	if username == "" {
		subject := strings.TrimSpace(userInfo.Subject)
		subject = strings.ReplaceAll(subject, "@", "-")
		if subject == "" {
			subject = "portrait-user"
		}
		username = subject + "@portrait.local"
	}

	fullName := strings.TrimSpace(userInfo.Name)
	if fullName == "" {
		fullName = username
	}

	return username, fullName, nil
}

// handleLogin handles user login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if s.authenticator.Authenticate(req.Username, req.Password) {
		session, _ := s.store.Get(r, "mail-session")
		session.Values["username"] = req.Username
		user, _ := s.authenticator.GetUser(req.Username)
		session.Values["fullName"] = user.FullName
		session.Save(r, w)

		response := map[string]interface{}{
			"user": map[string]string{
				"username": req.Username,
				"fullName": user.FullName,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	} else {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
	}
}

// handleLogout handles user logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	session, _ := s.store.Get(r, "mail-session")
	session.Values["username"] = ""
	session.Values["fullName"] = ""
	session.Options.MaxAge = -1
	session.Save(r, w)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Logged out successfully"})
}

// handleInbox returns inbox messages
func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value("username").(string)

	messages, err := s.storage.GetMessages(username)
	if err != nil {
		http.Error(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
	})
}

// handleGetMessage returns a specific message
func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value("username").(string)
	vars := mux.Vars(r)
	messageID := vars["id"]

	message, err := s.storage.GetMessage(username, messageID)
	if err != nil {
		http.Error(w, "Message not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(message)
}

// handleDeleteMessage deletes a message
func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value("username").(string)
	vars := mux.Vars(r)
	messageID := vars["id"]

	if err := s.storage.DeleteMessage(username, messageID); err != nil {
		http.Error(w, "Failed to delete message", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Message deleted successfully"})
}

// handleSendMail sends a new email
func (s *Server) handleSendMail(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value("username").(string)

	var req struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Create message
	messageID := auth.GenerateMessageID("localhost")
	timestamp := time.Now()

	// Create email content
	emailContent := fmt.Sprintf("Message-ID: <%s>\r\nDate: %s\r\nFrom: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		messageID, timestamp.Format(time.RFC1123Z), username, req.To, req.Subject, req.Body)

	message := &storage.Message{
		ID:        messageID,
		From:      username,
		To:        []string{req.To},
		Subject:   req.Subject,
		Body:      emailContent,
		Timestamp: timestamp,
		Size:      len(emailContent),
		Flags:     []string{},
	}

	if _, err := s.delivery.Enqueue(message); err != nil {
		http.Error(w, "Failed to enqueue email", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message":   "Email sent successfully",
		"messageId": messageID,
	})
}

// handleGetProfile returns user profile
func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value("username").(string)
	session, _ := s.store.Get(r, "mail-session")
	sessionFullName, _ := session.Values["fullName"].(string)

	user, exists := s.authenticator.GetUser(username)
	fullName := sessionFullName
	if fullName == "" {
		if exists {
			fullName = user.FullName
		} else {
			fullName = username
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"username": username,
		"fullName": fullName,
	})
}

// handleUpdateProfile updates user profile
func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FullName string `json:"fullName"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Note: In a real implementation, you'd update the config file here
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Profile updated successfully"})
}
