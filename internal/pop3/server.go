package pop3

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"mail-server/internal/auth"
	"mail-server/internal/config"
	"mail-server/internal/storage"
	"net"
	"strconv"
	"strings"
	"time"
)

// Server represents a POP3 server
type Server struct {
	config        config.POP3Config
	authenticator *auth.Authenticator
	storage       *storage.Storage
	listener      net.Listener
}

// NewServer creates a new POP3 server
func NewServer(cfg config.POP3Config) *Server {
	// Initialize storage
	store := storage.NewStorage("data")

	// We'll need to pass users from main
	authenticator := &auth.Authenticator{}

	return &Server{
		config:        cfg,
		authenticator: authenticator,
		storage:       store,
	}
}

// Start starts the POP3 server
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	var err error
	s.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	defer s.listener.Close()

	log.Printf("POP3 server listening on %s", addr)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return nil
				default:
					log.Printf("POP3 accept error: %v", err)
					continue
				}
			}

			go s.handleConnection(conn)
		}
	}
}

// handleConnection handles a single POP3 connection
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	session := &POP3Session{
		conn:    conn,
		server:  s,
		state:   stateAuthorization,
		reader:  bufio.NewReader(conn),
		deleted: make(map[int]bool),
	}

	session.handle()
}

// POP3Session represents a single POP3 session
type POP3Session struct {
	conn     net.Conn
	server   *Server
	state    POP3State
	reader   *bufio.Reader
	username string
	messages []*storage.Message
	deleted  map[int]bool
}

// POP3State represents the current state of a POP3 session
type POP3State int

const (
	stateAuthorization POP3State = iota
	stateTransaction
	stateUpdate
)

// handle manages the POP3 session
func (session *POP3Session) handle() {
	// Set timeout
	session.conn.SetDeadline(time.Now().Add(10 * time.Minute))

	// Send greeting
	session.writeLine("+OK POP3 server ready")

	for {
		line, _, err := session.reader.ReadLine()
		if err != nil {
			log.Printf("POP3 read error: %v", err)
			return
		}

		command := strings.TrimSpace(string(line))
		log.Printf("POP3 IN: %s", command)

		if err := session.processCommand(command); err != nil {
			log.Printf("POP3 command error: %v", err)
			return
		}
	}
}

// processCommand processes a single POP3 command
func (session *POP3Session) processCommand(command string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		session.writeLine("-ERR Invalid command")
		return nil
	}

	cmd := strings.ToUpper(parts[0])
	args := parts[1:]

	switch session.state {
	case stateAuthorization:
		return session.handleAuthCommand(cmd, args)
	case stateTransaction:
		return session.handleTransactionCommand(cmd, args)
	default:
		session.writeLine("-ERR Invalid state")
		return nil
	}
}

// handleAuthCommand handles commands in authorization state
func (session *POP3Session) handleAuthCommand(cmd string, args []string) error {
	switch cmd {
	case "USER":
		if len(args) < 1 {
			session.writeLine("-ERR Missing username")
			return nil
		}
		session.username = args[0]
		session.writeLine("+OK User accepted")
		return nil

	case "PASS":
		if len(args) < 1 {
			session.writeLine("-ERR Missing password")
			return nil
		}
		if session.username == "" {
			session.writeLine("-ERR USER command required first")
			return nil
		}

		password := args[0]
		if session.server.authenticator.Authenticate(session.username, password) {
			// Load messages
			var err error
			session.messages, err = session.server.storage.GetMessages(session.username)
			if err != nil {
				log.Printf("Failed to load messages for %s: %v", session.username, err)
				session.writeLine("-ERR Failed to load mailbox")
				return nil
			}

			session.state = stateTransaction
			session.writeLine("+OK Mailbox ready, %d messages", len(session.messages))
		} else {
			session.writeLine("-ERR Authentication failed")
		}
		return nil

	case "QUIT":
		session.writeLine("+OK Goodbye")
		return fmt.Errorf("client quit")

	default:
		session.writeLine("-ERR Command not available in this state")
		return nil
	}
}

// handleTransactionCommand handles commands in transaction state
func (session *POP3Session) handleTransactionCommand(cmd string, args []string) error {
	switch cmd {
	case "STAT":
		count := 0
		size := 0
		for i, msg := range session.messages {
			if !session.deleted[i+1] {
				count++
				size += msg.Size
			}
		}
		session.writeLine("+OK %d %d", count, size)
		return nil

	case "LIST":
		if len(args) > 0 {
			// LIST specific message
			msgNum, err := strconv.Atoi(args[0])
			if err != nil || msgNum < 1 || msgNum > len(session.messages) {
				session.writeLine("-ERR No such message")
				return nil
			}
			if session.deleted[msgNum] {
				session.writeLine("-ERR Message deleted")
				return nil
			}
			msg := session.messages[msgNum-1]
			session.writeLine("+OK %d %d", msgNum, msg.Size)
		} else {
			// LIST all messages
			session.writeLine("+OK")
			for i, msg := range session.messages {
				if !session.deleted[i+1] {
					session.writeLine("%d %d", i+1, msg.Size)
				}
			}
			session.writeLine(".")
		}
		return nil

	case "RETR":
		if len(args) < 1 {
			session.writeLine("-ERR Missing message number")
			return nil
		}
		msgNum, err := strconv.Atoi(args[0])
		if err != nil || msgNum < 1 || msgNum > len(session.messages) {
			session.writeLine("-ERR No such message")
			return nil
		}
		if session.deleted[msgNum] {
			session.writeLine("-ERR Message deleted")
			return nil
		}

		// Get full message
		msg, err := session.server.storage.GetMessage(session.username, session.messages[msgNum-1].ID)
		if err != nil {
			session.writeLine("-ERR Failed to retrieve message")
			return nil
		}

		session.writeLine("+OK %d octets", msg.Size)
		session.writeMessage(msg.Body)
		session.writeLine(".")
		return nil

	case "DELE":
		if len(args) < 1 {
			session.writeLine("-ERR Missing message number")
			return nil
		}
		msgNum, err := strconv.Atoi(args[0])
		if err != nil || msgNum < 1 || msgNum > len(session.messages) {
			session.writeLine("-ERR No such message")
			return nil
		}
		if session.deleted[msgNum] {
			session.writeLine("-ERR Message already deleted")
			return nil
		}

		session.deleted[msgNum] = true
		session.writeLine("+OK Message %d deleted", msgNum)
		return nil

	case "NOOP":
		session.writeLine("+OK")
		return nil

	case "RSET":
		session.deleted = make(map[int]bool)
		session.writeLine("+OK")
		return nil

	case "TOP":
		if len(args) < 2 {
			session.writeLine("-ERR Missing arguments")
			return nil
		}
		msgNum, err := strconv.Atoi(args[0])
		if err != nil || msgNum < 1 || msgNum > len(session.messages) {
			session.writeLine("-ERR No such message")
			return nil
		}
		if session.deleted[msgNum] {
			session.writeLine("-ERR Message deleted")
			return nil
		}

		lineCount, err := strconv.Atoi(args[1])
		if err != nil {
			session.writeLine("-ERR Invalid line count")
			return nil
		}

		// Get message and return headers + specified lines
		msg, err := session.server.storage.GetMessage(session.username, session.messages[msgNum-1].ID)
		if err != nil {
			session.writeLine("-ERR Failed to retrieve message")
			return nil
		}

		session.writeLine("+OK")
		session.writeTop(msg.Body, lineCount)
		session.writeLine(".")
		return nil

	case "UIDL":
		if len(args) > 0 {
			// UIDL specific message
			msgNum, err := strconv.Atoi(args[0])
			if err != nil || msgNum < 1 || msgNum > len(session.messages) {
				session.writeLine("-ERR No such message")
				return nil
			}
			if session.deleted[msgNum] {
				session.writeLine("-ERR Message deleted")
				return nil
			}
			msg := session.messages[msgNum-1]
			session.writeLine("+OK %d %s", msgNum, msg.ID)
		} else {
			// UIDL all messages
			session.writeLine("+OK")
			for i, msg := range session.messages {
				if !session.deleted[i+1] {
					session.writeLine("%d %s", i+1, msg.ID)
				}
			}
			session.writeLine(".")
		}
		return nil

	case "QUIT":
		session.state = stateUpdate
		// Apply deletions
		deletedCount := 0
		for msgNum := range session.deleted {
			if msgNum > 0 && msgNum <= len(session.messages) {
				msg := session.messages[msgNum-1]
				if err := session.server.storage.DeleteMessage(session.username, msg.ID); err != nil {
					log.Printf("Failed to delete message %s: %v", msg.ID, err)
				} else {
					deletedCount++
				}
			}
		}
		session.writeLine("+OK %d messages deleted", deletedCount)
		return fmt.Errorf("client quit")

	default:
		session.writeLine("-ERR Command not recognized")
		return nil
	}
}

// writeLine writes a formatted line to the client
func (session *POP3Session) writeLine(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	session.conn.Write([]byte(line + "\r\n"))
	log.Printf("POP3 OUT: %s", line)
}

// writeMessage writes a message with dot-stuffing
func (session *POP3Session) writeMessage(message string) {
	lines := strings.Split(message, "\r\n")
	for _, line := range lines {
		if strings.HasPrefix(line, ".") {
			session.conn.Write([]byte("." + line + "\r\n"))
		} else {
			session.conn.Write([]byte(line + "\r\n"))
		}
	}
}

// writeTop writes headers and specified number of body lines
func (session *POP3Session) writeTop(message string, bodyLines int) {
	lines := strings.Split(message, "\r\n")

	// Find header/body separator
	headerEnd := -1
	for i, line := range lines {
		if line == "" {
			headerEnd = i
			break
		}
	}

	// Write headers
	if headerEnd >= 0 {
		for i := 0; i <= headerEnd; i++ {
			line := lines[i]
			if strings.HasPrefix(line, ".") {
				session.conn.Write([]byte("." + line + "\r\n"))
			} else {
				session.conn.Write([]byte(line + "\r\n"))
			}
		}
	}

	// Write requested body lines
	if headerEnd >= 0 && headerEnd < len(lines)-1 {
		bodyStart := headerEnd + 1
		for i := 0; i < bodyLines && bodyStart+i < len(lines); i++ {
			line := lines[bodyStart+i]
			if strings.HasPrefix(line, ".") {
				session.conn.Write([]byte("." + line + "\r\n"))
			} else {
				session.conn.Write([]byte(line + "\r\n"))
			}
		}
	}
}
