package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the main configuration structure
type Config struct {
	SMTP     SMTPConfig      `yaml:"smtp"`
	POP3     POP3Config      `yaml:"pop3"`
	IMAP     IMAPConfig      `yaml:"imap"`
	Web      WebConfig       `yaml:"web"`
	Delivery DeliveryConfig  `yaml:"delivery"`
	Users    map[string]User `yaml:"users"`
	Security SecurityConfig  `yaml:"security"`
	Storage  StorageConfig   `yaml:"storage"`
}

// SMTPConfig contains SMTP server configuration
type SMTPConfig struct {
	Port           int    `yaml:"port"`
	Host           string `yaml:"host"`
	Domain         string `yaml:"domain"`
	MaxConnections int    `yaml:"max_connections"`
	RequireAuth    bool   `yaml:"require_auth"`
}

// POP3Config contains POP3 server configuration
type POP3Config struct {
	Port           int    `yaml:"port"`
	Host           string `yaml:"host"`
	MaxConnections int    `yaml:"max_connections"`
}

// IMAPConfig contains IMAP server configuration
type IMAPConfig struct {
	Port           int    `yaml:"port"`
	Host           string `yaml:"host"`
	MaxConnections int    `yaml:"max_connections"`
}

// WebConfig contains web server configuration
type WebConfig struct {
	Port          int            `yaml:"port"`
	Host          string         `yaml:"host"`
	SessionSecret string         `yaml:"session_secret"`
	OAuth         WebOAuthConfig `yaml:"oauth"`
}

// WebOAuthConfig contains OAuth login configuration for the web UI
type WebOAuthConfig struct {
	Enabled            bool   `yaml:"enabled"`
	ClientID           string `yaml:"client_id"`
	ClientSecret       string `yaml:"client_secret"`
	AuthorizeURL       string `yaml:"authorize_url"`
	TokenURL           string `yaml:"token_url"`
	UserInfoURL        string `yaml:"userinfo_url"`
	RedirectURL        string `yaml:"redirect_url"`
	Scope              string `yaml:"scope"`
	DefaultMailboxUser string `yaml:"default_mailbox_user"`
}

// DeliveryConfig contains outbound delivery queue settings
type DeliveryConfig struct {
	QueueDirectory              string   `yaml:"queue_directory"`
	WorkerCount                 int      `yaml:"worker_count"`
	PollIntervalMillis          int      `yaml:"poll_interval_millis"`
	MaxAttempts                 int      `yaml:"max_attempts"`
	BaseRetryDelaySeconds       int      `yaml:"base_retry_delay_seconds"`
	MaxRetryDelaySeconds        int      `yaml:"max_retry_delay_seconds"`
	BlockedSenderDomains        []string `yaml:"blocked_sender_domains"`
	BlockedRecipientDomains     []string `yaml:"blocked_recipient_domains"`
	PerSenderRateLimitPerMinute int      `yaml:"per_sender_rate_limit_per_minute"`
	EnableTLSCertValidation     bool     `yaml:"enable_tls_cert_validation"`
	TLSCertFile                 string   `yaml:"tls_cert_file"`
	TLSKeyFile                  string   `yaml:"tls_key_file"`
	EnableDNSPolicyChecks       bool     `yaml:"enable_dns_policy_checks"`
	DNSServerAddress            string   `yaml:"dns_server_address"`
	MXDNSServerAddress          string   `yaml:"mx_dns_server_address"`
	DNSBLDNSServerAddress       string   `yaml:"dnsbl_dns_server_address"`
	UseSystemResolverForDNSBL   bool     `yaml:"use_system_resolver_for_dnsbl"`
	DNSLookupTimeoutSeconds     int      `yaml:"dns_lookup_timeout_seconds"`
	RequireReachableMX          bool     `yaml:"require_reachable_mx"`
	DNSBLZones                  []string `yaml:"dnsbl_zones"`
	SkipDNSPolicyForDomains     []string `yaml:"skip_dns_policy_for_domains"`
}

// User represents a user account
type User struct {
	Password string `yaml:"password"`
	FullName string `yaml:"full_name"`
}

// SecurityConfig contains security settings
type SecurityConfig struct {
	AllowedDomains []string `yaml:"allowed_domains"`
	MaxMessageSize int64    `yaml:"max_message_size"`
	RequireAuth    bool     `yaml:"require_auth"`
}

// StorageConfig contains storage settings
type StorageConfig struct {
	DataDirectory string `yaml:"data_directory"`
	LogDirectory  string `yaml:"log_directory"`
}

// Load loads configuration from a YAML file
func Load(filename string) (*Config, error) {
	// Create default config
	cfg := &Config{
		SMTP: SMTPConfig{
			Port:           25,
			Host:           "0.0.0.0",
			Domain:         "localhost",
			MaxConnections: 100,
			RequireAuth:    true,
		},
		POP3: POP3Config{
			Port:           110,
			Host:           "0.0.0.0",
			MaxConnections: 50,
		},
		IMAP: IMAPConfig{
			Port:           143,
			Host:           "0.0.0.0",
			MaxConnections: 50,
		},
		Web: WebConfig{
			Port:          8080,
			Host:          "0.0.0.0",
			SessionSecret: "replace-with-a-long-random-session-secret",
			OAuth: WebOAuthConfig{
				Enabled:            false,
				ClientID:           "",
				ClientSecret:       "",
				AuthorizeURL:       "https://pam.meandering.tel/oauth/authorize",
				TokenURL:           "https://pam.meandering.tel/oauth/token",
				UserInfoURL:        "https://pam.meandering.tel/oauth/userinfo",
				RedirectURL:        "http://localhost:8081/auth/portrait/callback",
				Scope:              "profile",
				DefaultMailboxUser: "admin@localhost",
			},
		},
		Delivery: DeliveryConfig{
			QueueDirectory:              "data/outbound-queue",
			WorkerCount:                 1,
			PollIntervalMillis:          1000,
			MaxAttempts:                 6,
			BaseRetryDelaySeconds:       5,
			MaxRetryDelaySeconds:        300,
			BlockedSenderDomains:        []string{},
			BlockedRecipientDomains:     []string{},
			PerSenderRateLimitPerMinute: 120,
			EnableTLSCertValidation:     false,
			TLSCertFile:                 "",
			TLSKeyFile:                  "",
			EnableDNSPolicyChecks:       false,
			DNSServerAddress:            "",
			MXDNSServerAddress:          "",
			DNSBLDNSServerAddress:       "",
			UseSystemResolverForDNSBL:   true,
			DNSLookupTimeoutSeconds:     3,
			RequireReachableMX:          false,
			DNSBLZones:                  []string{},
			SkipDNSPolicyForDomains:     []string{"localhost", "local"},
		},
		Users: map[string]User{
			"admin@localhost": {
				Password: "admin123",
				FullName: "Administrator",
			},
			"user@localhost": {
				Password: "user123",
				FullName: "Test User",
			},
		},
		Security: SecurityConfig{
			AllowedDomains: []string{"localhost"},
			MaxMessageSize: 10485760, // 10MB
			RequireAuth:    true,
		},
		Storage: StorageConfig{
			DataDirectory: "data",
			LogDirectory:  "logs",
		},
	}

	// Try to load from file
	if _, err := os.Stat(filename); err == nil {
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	} else {
		// Create default config file
		if err := cfg.Save(filename); err != nil {
			return nil, fmt.Errorf("failed to create default config: %w", err)
		}
	}

	return cfg, nil
}

// Save saves the configuration to a YAML file
func (c *Config) Save(filename string) error {
	// Ensure directory exists
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
