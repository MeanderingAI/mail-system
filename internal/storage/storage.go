package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Message represents an email message
type Message struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        []string  `json:"to"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	Timestamp time.Time `json:"timestamp"`
	Size      int       `json:"size"`
	Flags     []string  `json:"flags"`
}

// Storage handles email storage operations
type Storage struct {
	dataDir string
}

// NewStorage creates a new storage instance
func NewStorage(dataDir string) *Storage {
	return &Storage{
		dataDir: dataDir,
	}
}

// StoreMessage stores an email message for the specified recipients
func (s *Storage) StoreMessage(msg *Message) error {
	// Ensure data directory exists
	if err := os.MkdirAll(s.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Store message for each recipient
	for _, recipient := range msg.To {
		userDir := filepath.Join(s.dataDir, "mailboxes", recipient)
		if err := os.MkdirAll(userDir, 0755); err != nil {
			return fmt.Errorf("failed to create user directory: %w", err)
		}

		// Store raw message
		emailPath := filepath.Join(userDir, msg.ID+".eml")
		if err := os.WriteFile(emailPath, []byte(msg.Body), 0644); err != nil {
			return fmt.Errorf("failed to write email file: %w", err)
		}

		// Store metadata
		metaPath := filepath.Join(userDir, msg.ID+".meta")
		metaData, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
			return fmt.Errorf("failed to write metadata file: %w", err)
		}
	}

	return nil
}

// GetMessages retrieves all messages for a user
func (s *Storage) GetMessages(username string) ([]*Message, error) {
	userDir := filepath.Join(s.dataDir, "mailboxes", username)

	// Check if user directory exists
	if _, err := os.Stat(userDir); os.IsNotExist(err) {
		return []*Message{}, nil
	}

	files, err := os.ReadDir(userDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read user directory: %w", err)
	}

	var messages []*Message
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".meta") {
			metaPath := filepath.Join(userDir, file.Name())
			data, err := os.ReadFile(metaPath)
			if err != nil {
				continue // Skip corrupted files
			}

			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				continue // Skip corrupted files
			}

			messages = append(messages, &msg)
		}
	}

	// Sort messages by timestamp
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	return messages, nil
}

// GetMessage retrieves a specific message by ID for a user
func (s *Storage) GetMessage(username, messageID string) (*Message, error) {
	userDir := filepath.Join(s.dataDir, "mailboxes", username)
	metaPath := filepath.Join(userDir, messageID+".meta")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("message not found: %w", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse message metadata: %w", err)
	}

	// Load the full body
	emailPath := filepath.Join(userDir, messageID+".eml")
	bodyData, err := os.ReadFile(emailPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read message body: %w", err)
	}

	msg.Body = string(bodyData)
	return &msg, nil
}

// DeleteMessage deletes a message for a user
func (s *Storage) DeleteMessage(username, messageID string) error {
	userDir := filepath.Join(s.dataDir, "mailboxes", username)

	emailPath := filepath.Join(userDir, messageID+".eml")
	metaPath := filepath.Join(userDir, messageID+".meta")

	// Remove both files
	os.Remove(emailPath)
	os.Remove(metaPath)

	return nil
}

// GetMessageCount returns the number of messages for a user
func (s *Storage) GetMessageCount(username string) (int, error) {
	messages, err := s.GetMessages(username)
	if err != nil {
		return 0, err
	}
	return len(messages), nil
}

// GetTotalSize returns the total size of all messages for a user
func (s *Storage) GetTotalSize(username string) (int64, error) {
	messages, err := s.GetMessages(username)
	if err != nil {
		return 0, err
	}

	var total int64
	for _, msg := range messages {
		total += int64(msg.Size)
	}

	return total, nil
}

// ParseRawMessage parses a raw email message and extracts headers and body
func ParseRawMessage(rawMessage string) *Message {
	lines := strings.Split(rawMessage, "\r\n")

	msg := &Message{
		Timestamp: time.Now(),
		Flags:     []string{},
	}

	// Find the blank line that separates headers from body
	headerEnd := -1
	for i, line := range lines {
		if line == "" {
			headerEnd = i
			break
		}
	}

	// Parse headers
	if headerEnd > 0 {
		for i := 0; i < headerEnd; i++ {
			line := lines[i]
			if strings.HasPrefix(strings.ToLower(line), "from:") {
				msg.From = strings.TrimSpace(line[5:])
			} else if strings.HasPrefix(strings.ToLower(line), "to:") {
				to := strings.TrimSpace(line[3:])
				msg.To = strings.Split(to, ",")
				for j := range msg.To {
					msg.To[j] = strings.TrimSpace(msg.To[j])
				}
			} else if strings.HasPrefix(strings.ToLower(line), "subject:") {
				msg.Subject = strings.TrimSpace(line[8:])
			}
		}
	}

	// Get body
	if headerEnd >= 0 && headerEnd < len(lines)-1 {
		msg.Body = strings.Join(lines[headerEnd+1:], "\r\n")
	} else {
		msg.Body = rawMessage
	}

	msg.Size = len(rawMessage)
	return msg
}

// CreateUserDirectory creates a directory for a new user
func (s *Storage) CreateUserDirectory(username string) error {
	userDir := filepath.Join(s.dataDir, "mailboxes", username)
	return os.MkdirAll(userDir, 0755)
}

// WriteMessageReader writes a message from an io.Reader
func (s *Storage) WriteMessageReader(username, messageID string, reader io.Reader) error {
	userDir := filepath.Join(s.dataDir, "mailboxes", username)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return fmt.Errorf("failed to create user directory: %w", err)
	}

	emailPath := filepath.Join(userDir, messageID+".eml")
	file, err := os.Create(emailPath)
	if err != nil {
		return fmt.Errorf("failed to create email file: %w", err)
	}
	defer file.Close()

	_, err = io.Copy(file, reader)
	return err
}
