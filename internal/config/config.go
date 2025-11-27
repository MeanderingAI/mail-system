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
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
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
			Port: 8080,
			Host: "0.0.0.0",
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
