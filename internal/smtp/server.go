package smtp

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"mail-server/internal/auth"
	"mail-server/internal/config"
	"mail-server/internal/delivery"
	"mail-server/internal/storage"
	"net"
	"strings"
	"time"
)

// Server represents an SMTP server
type Server struct {
	config        config.SMTPConfig
	authenticator *auth.Authenticator
	storage       *storage.Storage
	delivery      *delivery.Service
	listener      net.Listener
}

// NewServer creates a new SMTP server
func NewServer(cfg config.SMTPConfig, users map[string]config.User, store *storage.Storage, deliveryService *delivery.Service) *Server {
	authenticator := auth.NewAuthenticator(users)
	return &Server{
		config:        cfg,
		authenticator: authenticator,
		storage:       store,
		delivery:      deliveryService,
	}
}

// Start starts the SMTP server
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	var err error
	s.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	defer s.listener.Close()

	log.Printf("SMTP server listening on %s", addr)

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
					log.Printf("SMTP accept error: %v", err)
					continue
				}
			}

			go s.handleConnection(conn)
		}
	}
}

// handleConnection handles a single SMTP connection
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	session := &SMTPSession{
		conn:   conn,
		server: s,
		state:  stateInitial,
		reader: bufio.NewReader(conn),
	}

	session.handle()
}

// SMTPSession represents a single SMTP session
type SMTPSession struct {
	conn          net.Conn
	server        *Server
	state         SMTPState
	reader        *bufio.Reader
	clientHost    string
	mailFrom      string
	rcptTo        []string
	authenticated bool
	username      string
	data          strings.Builder
}

// SMTPState represents the current state of an SMTP session
type SMTPState int

const (
	stateInitial SMTPState = iota
	stateGreeted
	stateMailFrom
	stateRcptTo
	stateData
	stateDataContent
)

// handle manages the SMTP session
func (session *SMTPSession) handle() {
	// Set timeout
	session.conn.SetDeadline(time.Now().Add(5 * time.Minute))

	// Send greeting
	session.writeLine("220 %s SMTP Server ready", session.server.config.Domain)

	for {
		line, _, err := session.reader.ReadLine()
		if err != nil {
			log.Printf("SMTP read error: %v", err)
			return
		}

		command := strings.TrimSpace(string(line))
		log.Printf("SMTP IN: %s", command)

		if err := session.processCommand(command); err != nil {
			log.Printf("SMTP command error: %v", err)
			return
		}
	}
}

// processCommand processes a single SMTP command
func (session *SMTPSession) processCommand(command string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		session.writeLine("500 Syntax error")
		return nil
	}

	cmd := strings.ToUpper(parts[0])
	args := strings.Join(parts[1:], " ")

	switch cmd {
	case "HELO", "EHLO":
		return session.handleHelo(cmd, args)
	case "AUTH":
		return session.handleAuth(args)
	case "MAIL":
		return session.handleMail(args)
	case "RCPT":
		return session.handleRcpt(args)
	case "DATA":
		return session.handleData()
	case "RSET":
		return session.handleRset()
	case "QUIT":
		return session.handleQuit()
	case "NOOP":
		session.writeLine("250 OK")
		return nil
	default:
		session.writeLine("502 Command not implemented")
		return nil
	}
}

// handleHelo handles HELO/EHLO commands
func (session *SMTPSession) handleHelo(cmd, hostname string) error {
	session.clientHost = hostname
	session.state = stateGreeted

	if cmd == "EHLO" {
		session.writeLine("250-%s Hello %s", session.server.config.Domain, hostname)
		session.writeLine("250-AUTH PLAIN LOGIN")
		session.writeLine("250-PIPELINING")
		session.writeLine("250 8BITMIME")
	} else {
		session.writeLine("250 %s Hello %s", session.server.config.Domain, hostname)
	}

	return nil
}

// handleAuth handles authentication
func (session *SMTPSession) handleAuth(args string) error {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		session.writeLine("504 Authentication mechanism not specified")
		return nil
	}

	mechanism := strings.ToUpper(parts[0])

	switch mechanism {
	case "PLAIN":
		if len(parts) > 1 {
			return session.handleAuthPlain(parts[1])
		} else {
			session.writeLine("334 ")
			// Read auth data from next line
			line, _, err := session.reader.ReadLine()
			if err != nil {
				return err
			}
			return session.handleAuthPlain(string(line))
		}
	case "LOGIN":
		session.writeLine("334 VXNlcm5hbWU6") // "Username:" in base64
		line, _, err := session.reader.ReadLine()
		if err != nil {
			return err
		}
		username, _ := base64.StdEncoding.DecodeString(string(line))

		session.writeLine("334 UGFzc3dvcmQ6") // "Password:" in base64
		line, _, err = session.reader.ReadLine()
		if err != nil {
			return err
		}
		password, _ := base64.StdEncoding.DecodeString(string(line))

		return session.authenticateUser(string(username), string(password))
	default:
		session.writeLine("504 Authentication mechanism not supported")
		return nil
	}
}

// handleAuthPlain handles PLAIN authentication
func (session *SMTPSession) handleAuthPlain(credentials string) error {
	decoded, err := base64.StdEncoding.DecodeString(credentials)
	if err != nil {
		session.writeLine("535 Authentication failed")
		return nil
	}

	parts := strings.Split(string(decoded), "\000")
	if len(parts) < 3 {
		session.writeLine("535 Authentication failed")
		return nil
	}

	username := parts[1]
	password := parts[2]

	return session.authenticateUser(username, password)
}

// authenticateUser authenticates a user
func (session *SMTPSession) authenticateUser(username, password string) error {
	if session.server.authenticator.Authenticate(username, password) {
		session.authenticated = true
		session.username = username
		session.writeLine("235 Authentication successful")
	} else {
		session.writeLine("535 Authentication failed")
	}
	return nil
}

// handleMail handles MAIL FROM command
func (session *SMTPSession) handleMail(args string) error {
	if session.server.config.RequireAuth && !session.authenticated {
		session.writeLine("530 Authentication required")
		return nil
	}

	// Parse MAIL FROM:<address>
	if !strings.HasPrefix(strings.ToUpper(args), "FROM:") {
		session.writeLine("501 Syntax error in MAIL FROM")
		return nil
	}

	fromAddr := strings.TrimSpace(args[5:])
	if strings.HasPrefix(fromAddr, "<") && strings.HasSuffix(fromAddr, ">") {
		fromAddr = fromAddr[1 : len(fromAddr)-1]
	}

	session.mailFrom = fromAddr
	session.rcptTo = []string{}
	session.state = stateMailFrom
	session.writeLine("250 OK")

	return nil
}

// handleRcpt handles RCPT TO command
func (session *SMTPSession) handleRcpt(args string) error {
	if session.state != stateMailFrom && session.state != stateRcptTo {
		session.writeLine("503 Bad sequence of commands")
		return nil
	}

	// Parse RCPT TO:<address>
	if !strings.HasPrefix(strings.ToUpper(args), "TO:") {
		session.writeLine("501 Syntax error in RCPT TO")
		return nil
	}

	toAddr := strings.TrimSpace(args[3:])
	if strings.HasPrefix(toAddr, "<") && strings.HasSuffix(toAddr, ">") {
		toAddr = toAddr[1 : len(toAddr)-1]
	}

	session.rcptTo = append(session.rcptTo, toAddr)
	session.state = stateRcptTo
	session.writeLine("250 OK")

	return nil
}

// handleData handles DATA command
func (session *SMTPSession) handleData() error {
	if session.state != stateRcptTo {
		session.writeLine("503 Bad sequence of commands")
		return nil
	}

	session.state = stateDataContent
	session.data.Reset()
	session.writeLine("354 End data with <CR><LF>.<CR><LF>")

	// Read data until "."
	for {
		line, _, err := session.reader.ReadLine()
		if err != nil {
			return err
		}

		lineStr := string(line)
		if lineStr == "." {
			break
		}

		// Unescape dot-stuffing
		if strings.HasPrefix(lineStr, "..") {
			lineStr = lineStr[1:]
		}

		session.data.WriteString(lineStr)
		session.data.WriteString("\r\n")
	}

	return session.processMessage()
}

// processMessage processes and stores the received message
func (session *SMTPSession) processMessage() error {
	messageData := session.data.String()

	// Parse the message
	msg := storage.ParseRawMessage(messageData)
	msg.ID = auth.GenerateMessageID(session.server.config.Domain)
	msg.From = session.mailFrom
	msg.To = session.rcptTo

	// Enqueue message for asynchronous delivery.
	if _, err := session.server.delivery.Enqueue(msg); err != nil {
		log.Printf("Failed to enqueue message: %v", err)
		session.writeLine("451 Temporary failure")
		return nil
	}

	log.Printf("Message %s accepted from %s to %v", msg.ID, msg.From, msg.To)
	session.writeLine("250 Message accepted for delivery")

	// Reset session state
	session.mailFrom = ""
	session.rcptTo = []string{}
	session.state = stateGreeted

	return nil
}

// handleRset handles RSET command
func (session *SMTPSession) handleRset() error {
	session.mailFrom = ""
	session.rcptTo = []string{}
	session.state = stateGreeted
	session.writeLine("250 OK")
	return nil
}

// handleQuit handles QUIT command
func (session *SMTPSession) handleQuit() error {
	session.writeLine("221 Bye")
	return fmt.Errorf("client quit")
}

// writeLine writes a formatted line to the client
func (session *SMTPSession) writeLine(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	session.conn.Write([]byte(line + "\r\n"))
	log.Printf("SMTP OUT: %s", line)
}
