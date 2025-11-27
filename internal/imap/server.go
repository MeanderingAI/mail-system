package imap

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

// Server represents an IMAP server
type Server struct {
	config        config.IMAPConfig
	authenticator *auth.Authenticator
	storage       *storage.Storage
	listener      net.Listener
}

// NewServer creates a new IMAP server
func NewServer(cfg config.IMAPConfig) *Server {
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

// Start starts the IMAP server
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	var err error
	s.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	defer s.listener.Close()

	log.Printf("IMAP server listening on %s", addr)

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
					log.Printf("IMAP accept error: %v", err)
					continue
				}
			}

			go s.handleConnection(conn)
		}
	}
}

// handleConnection handles a single IMAP connection
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	session := &IMAPSession{
		conn:   conn,
		server: s,
		state:  stateNotAuthenticated,
		reader: bufio.NewReader(conn),
	}

	session.handle()
}

// IMAPSession represents a single IMAP session
type IMAPSession struct {
	conn            net.Conn
	server          *Server
	state           IMAPState
	reader          *bufio.Reader
	username        string
	selectedMailbox string
	messages        []*storage.Message
}

// IMAPState represents the current state of an IMAP session
type IMAPState int

const (
	stateNotAuthenticated IMAPState = iota
	stateAuthenticated
	stateSelected
	stateLogout
)

// handle manages the IMAP session
func (session *IMAPSession) handle() {
	// Set timeout
	session.conn.SetDeadline(time.Now().Add(30 * time.Minute))

	// Send greeting
	session.writeLine("* OK IMAP4rev1 Server Ready")

	for {
		lineBytes, _, err := session.reader.ReadLine()
		if err != nil {
			log.Printf("IMAP read error: %v", err)
			return
		}
		line := lineBytes

		command := strings.TrimSpace(string(line))
		log.Printf("IMAP IN: %s", command)

		if err := session.processCommand(command); err != nil {
			log.Printf("IMAP command error: %v", err)
			return
		}
	}
}

// processCommand processes a single IMAP command
func (session *IMAPSession) processCommand(command string) error {
	parts := strings.Fields(command)
	if len(parts) < 2 {
		session.writeLine("* BAD Invalid command")
		return nil
	}

	tag := parts[0]
	cmd := strings.ToUpper(parts[1])
	args := parts[2:]

	switch cmd {
	case "CAPABILITY":
		session.handleCapability(tag)
	case "LOGIN":
		session.handleLogin(tag, args)
	case "LOGOUT":
		session.handleLogout(tag)
	case "SELECT":
		session.handleSelect(tag, args)
	case "EXAMINE":
		session.handleExamine(tag, args)
	case "LIST":
		session.handleList(tag, args)
	case "FETCH":
		session.handleFetch(tag, args)
	case "SEARCH":
		session.handleSearch(tag, args)
	case "STORE":
		session.handleStore(tag, args)
	case "EXPUNGE":
		session.handleExpunge(tag)
	case "NOOP":
		session.writeLine("%s OK NOOP completed", tag)
	default:
		session.writeLine("%s BAD Command not recognized", tag)
	}

	return nil
}

// handleCapability handles CAPABILITY command
func (session *IMAPSession) handleCapability(tag string) {
	session.writeLine("* CAPABILITY IMAP4rev1 LOGIN AUTH=PLAIN")
	session.writeLine("%s OK CAPABILITY completed", tag)
}

// handleLogin handles LOGIN command
func (session *IMAPSession) handleLogin(tag string, args []string) {
	if len(args) < 2 {
		session.writeLine("%s BAD LOGIN requires username and password", tag)
		return
	}

	username := strings.Trim(args[0], "\"")
	password := strings.Trim(args[1], "\"")

	if session.server.authenticator.Authenticate(username, password) {
		session.username = username
		session.state = stateAuthenticated
		session.writeLine("%s OK LOGIN completed", tag)
	} else {
		session.writeLine("%s NO LOGIN failed", tag)
	}
}

// handleLogout handles LOGOUT command
func (session *IMAPSession) handleLogout(tag string) {
	session.state = stateLogout
	session.writeLine("* BYE LOGOUT requested")
	session.writeLine("%s OK LOGOUT completed", tag)
}

// handleSelect handles SELECT command
func (session *IMAPSession) handleSelect(tag string, args []string) {
	if session.state == stateNotAuthenticated {
		session.writeLine("%s NO Not authenticated", tag)
		return
	}

	if len(args) < 1 {
		session.writeLine("%s BAD SELECT requires mailbox name", tag)
		return
	}

	mailbox := strings.Trim(args[0], "\"")
	if strings.ToUpper(mailbox) == "INBOX" {
		session.selectedMailbox = "INBOX"
		session.state = stateSelected

		// Load messages
		var err error
		session.messages, err = session.server.storage.GetMessages(session.username)
		if err != nil {
			log.Printf("Failed to load messages: %v", err)
			session.messages = []*storage.Message{}
		}

		session.writeLine("* %d EXISTS", len(session.messages))
		session.writeLine("* 0 RECENT")
		session.writeLine("* OK [UIDVALIDITY 1] UID validity status")
		session.writeLine("* OK [UIDNEXT %d] Predicted next UID", len(session.messages)+1)
		session.writeLine("* FLAGS (\\Answered \\Flagged \\Deleted \\Seen \\Draft)")
		session.writeLine("* OK [PERMANENTFLAGS (\\Answered \\Flagged \\Deleted \\Seen \\Draft)] Flags permitted")
		session.writeLine("%s OK [READ-WRITE] SELECT completed", tag)
	} else {
		session.writeLine("%s NO Mailbox does not exist", tag)
	}
}

// handleExamine handles EXAMINE command (read-only select)
func (session *IMAPSession) handleExamine(tag string, args []string) {
	session.handleSelect(tag, args)
	if session.selectedMailbox != "" {
		session.writeLine("%s OK [READ-ONLY] EXAMINE completed", tag)
	}
}

// handleList handles LIST command
func (session *IMAPSession) handleList(tag string, args []string) {
	if session.state == stateNotAuthenticated {
		session.writeLine("%s NO Not authenticated", tag)
		return
	}

	session.writeLine("* LIST () \"/\" INBOX")
	session.writeLine("%s OK LIST completed", tag)
}

// handleFetch handles FETCH command
func (session *IMAPSession) handleFetch(tag string, args []string) {
	if session.state != stateSelected {
		session.writeLine("%s NO No mailbox selected", tag)
		return
	}

	if len(args) < 2 {
		session.writeLine("%s BAD FETCH requires sequence set and items", tag)
		return
	}

	sequenceSet := args[0]
	items := strings.Join(args[1:], " ")

	messageNums := session.parseSequenceSet(sequenceSet)

	for _, msgNum := range messageNums {
		if msgNum > 0 && msgNum <= len(session.messages) {
			session.fetchMessage(msgNum, session.messages[msgNum-1], items)
		}
	}

	session.writeLine("%s OK FETCH completed", tag)
}

// fetchMessage fetches specific message data
func (session *IMAPSession) fetchMessage(msgNum int, msg *storage.Message, items string) {
	itemsUpper := strings.ToUpper(items)
	response := fmt.Sprintf("* %d FETCH (", msgNum)
	parts := []string{}

	if strings.Contains(itemsUpper, "UID") {
		parts = append(parts, fmt.Sprintf("UID %d", msgNum))
	}

	if strings.Contains(itemsUpper, "FLAGS") {
		parts = append(parts, "FLAGS ()")
	}

	if strings.Contains(itemsUpper, "RFC822.SIZE") {
		parts = append(parts, fmt.Sprintf("RFC822.SIZE %d", msg.Size))
	}

	if strings.Contains(itemsUpper, "ENVELOPE") {
		envelope := fmt.Sprintf("ENVELOPE (\"%s\" \"%s\" ((\"%s\" NIL \"%s\" \"%s\")) ((\"%s\" NIL \"%s\" \"%s\")) NIL NIL NIL NIL)",
			msg.Timestamp.Format("02-Jan-2006 15:04:05 -0700"), msg.Subject, msg.From, msg.From, msg.From, msg.From, msg.From, msg.From)
		parts = append(parts, envelope)
	}

	if strings.Contains(itemsUpper, "BODY[]") || strings.Contains(itemsUpper, "RFC822") {
		// Get full message
		fullMsg, err := session.server.storage.GetMessage(session.username, msg.ID)
		if err == nil {
			parts = append(parts, fmt.Sprintf("BODY[] {%d}", len(fullMsg.Body)))
			response += strings.Join(parts, " ") + ")\r\n"
			session.conn.Write([]byte(response))
			session.conn.Write([]byte(fullMsg.Body))
			session.writeLine("")
			return
		}
	}

	response += strings.Join(parts, " ") + ")"
	session.writeLine(response)
}

// parseSequenceSet parses an IMAP sequence set
func (session *IMAPSession) parseSequenceSet(sequenceSet string) []int {
	var numbers []int
	parts := strings.Split(sequenceSet, ",")

	for _, part := range parts {
		if strings.Contains(part, ":") {
			rangeParts := strings.Split(part, ":")
			start, _ := strconv.Atoi(rangeParts[0])
			end := len(session.messages)
			if rangeParts[1] != "*" {
				end, _ = strconv.Atoi(rangeParts[1])
			}
			for i := start; i <= end; i++ {
				numbers = append(numbers, i)
			}
		} else if part == "*" {
			numbers = append(numbers, len(session.messages))
		} else {
			num, _ := strconv.Atoi(part)
			numbers = append(numbers, num)
		}
	}

	return numbers
}

// handleSearch handles SEARCH command
func (session *IMAPSession) handleSearch(tag string, args []string) {
	if session.state != stateSelected {
		session.writeLine("%s NO No mailbox selected", tag)
		return
	}

	// Simple search - return all messages
	var results []string
	for i := 1; i <= len(session.messages); i++ {
		results = append(results, strconv.Itoa(i))
	}

	session.writeLine("* SEARCH %s", strings.Join(results, " "))
	session.writeLine("%s OK SEARCH completed", tag)
}

// handleStore handles STORE command
func (session *IMAPSession) handleStore(tag string, args []string) {
	if session.state != stateSelected {
		session.writeLine("%s NO No mailbox selected", tag)
		return
	}

	// Simple implementation - just acknowledge
	session.writeLine("%s OK STORE completed", tag)
}

// handleExpunge handles EXPUNGE command
func (session *IMAPSession) handleExpunge(tag string) {
	if session.state != stateSelected {
		session.writeLine("%s NO No mailbox selected", tag)
		return
	}

	session.writeLine("%s OK EXPUNGE completed", tag)
}

// writeLine writes a formatted line to the client
func (session *IMAPSession) writeLine(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	session.conn.Write([]byte(line + "\r\n"))
	log.Printf("IMAP OUT: %s", line)
}
