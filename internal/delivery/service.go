package delivery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"mail-server/internal/config"
	"mail-server/internal/storage"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	statusPending   = "pending"
	statusDelivered = "delivered"
	statusFailed    = "failed"
)

type queueItem struct {
	ID          string          `json:"id"`
	Message     storage.Message `json:"message"`
	Attempts    int             `json:"attempts"`
	NextAttempt time.Time       `json:"next_attempt"`
	LastError   string          `json:"last_error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Status      string          `json:"status"`
}

type Service struct {
	cfg          config.DeliveryConfig
	store        *storage.Storage
	queueDir     string
	pendingDir   string
	deliveredDir string
	failedDir    string
	mu           sync.Mutex
	senderWindow map[string][]time.Time
}

type policyError struct {
	message   string
	permanent bool
}

func (e *policyError) Error() string {
	return e.message
}

func NewService(cfg config.DeliveryConfig, store *storage.Storage) (*Service, error) {
	queueDir := strings.TrimSpace(cfg.QueueDirectory)
	if queueDir == "" {
		queueDir = "data/outbound-queue"
	}

	service := &Service{
		cfg:          normalizeConfig(cfg),
		store:        store,
		queueDir:     queueDir,
		pendingDir:   filepath.Join(queueDir, statusPending),
		deliveredDir: filepath.Join(queueDir, statusDelivered),
		failedDir:    filepath.Join(queueDir, statusFailed),
		senderWindow: map[string][]time.Time{},
	}

	for _, dirPath := range []string{service.pendingDir, service.deliveredDir, service.failedDir} {
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create delivery queue directory %s: %w", dirPath, err)
		}
	}

	if err := service.validateTLSMaterial(); err != nil {
		return nil, err
	}

	return service, nil
}

func (s *Service) Enqueue(msg *storage.Message) (string, error) {
	if msg == nil {
		return "", fmt.Errorf("nil message")
	}
	if strings.TrimSpace(msg.ID) == "" {
		return "", fmt.Errorf("message id is required")
	}

	now := time.Now().UTC()
	item := queueItem{
		ID:          msg.ID,
		Message:     *msg,
		Attempts:    0,
		NextAttempt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
		Status:      statusPending,
	}

	if err := s.writeItem(filepath.Join(s.pendingDir, fmt.Sprintf("%s.json", msg.ID)), &item); err != nil {
		return "", err
	}

	return msg.ID, nil
}

func (s *Service) Start(ctx context.Context) error {
	workerCount := s.cfg.WorkerCount
	if workerCount < 1 {
		workerCount = 1
	}

	log.Printf("Delivery queue worker started (%d worker(s), queue=%s)", workerCount, s.queueDir)

	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			s.runWorker(ctx, workerID)
		}(worker + 1)
	}

	<-ctx.Done()
	wg.Wait()
	return nil
}

func (s *Service) runWorker(ctx context.Context, workerID int) {
	ticker := time.NewTicker(time.Duration(s.cfg.PollIntervalMillis) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processPendingBatch(workerID)
		}
	}
}

func (s *Service) processPendingBatch(workerID int) {
	entries, err := os.ReadDir(s.pendingDir)
	if err != nil {
		log.Printf("Delivery worker %d failed to read queue: %v", workerID, err)
		return
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	for _, fileName := range files {
		path := filepath.Join(s.pendingDir, fileName)
		if err := s.processItem(path); err != nil {
			log.Printf("Delivery worker %d failed to process %s: %v", workerID, fileName, err)
		}
	}
}

func (s *Service) processItem(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, err := s.readItem(path)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if now.Before(item.NextAttempt) {
		return nil
	}

	if err := s.applyPolicy(item, now); err != nil {
		if pErr, ok := err.(*policyError); ok && pErr.permanent {
			item.Attempts++
			item.Status = statusFailed
			item.UpdatedAt = now
			item.LastError = pErr.Error()
			if err := s.writeItem(filepath.Join(s.failedDir, filepath.Base(path)), item); err != nil {
				return err
			}
			return os.Remove(path)
		}

		item.Attempts++
		item.Status = statusPending
		item.UpdatedAt = now
		item.LastError = err.Error()
		if item.Attempts >= s.cfg.MaxAttempts {
			item.Status = statusFailed
			if err := s.writeItem(filepath.Join(s.failedDir, filepath.Base(path)), item); err != nil {
				return err
			}
			return os.Remove(path)
		}
		item.NextAttempt = now.Add(s.backoffDuration(item.Attempts))
		return s.writeItem(path, item)
	}

	if err := s.store.StoreMessage(&item.Message); err != nil {
		item.Attempts++
		item.UpdatedAt = now
		item.LastError = err.Error()

		if item.Attempts >= s.cfg.MaxAttempts {
			item.Status = statusFailed
			if err := s.writeItem(filepath.Join(s.failedDir, filepath.Base(path)), item); err != nil {
				return err
			}
			if removeErr := os.Remove(path); removeErr != nil {
				return removeErr
			}
			return nil
		}

		item.Status = statusPending
		item.NextAttempt = now.Add(s.backoffDuration(item.Attempts))
		return s.writeItem(path, item)
	}

	item.Status = statusDelivered
	item.UpdatedAt = now
	item.LastError = ""
	if err := s.writeItem(filepath.Join(s.deliveredDir, filepath.Base(path)), item); err != nil {
		return err
	}
	return os.Remove(path)
}

func (s *Service) readItem(path string) (*queueItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var item queueItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("failed to decode queue item %s: %w", path, err)
	}

	if item.Status == "" {
		item.Status = statusPending
	}
	return &item, nil
}

func (s *Service) writeItem(path string, item *queueItem) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *Service) backoffDuration(attempt int) time.Duration {
	base := float64(s.cfg.BaseRetryDelaySeconds)
	maxDelay := float64(s.cfg.MaxRetryDelaySeconds)
	multiplier := math.Pow(2, float64(attempt-1))
	seconds := base * multiplier
	if seconds > maxDelay {
		seconds = maxDelay
	}
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(seconds) * time.Second
}

func normalizeConfig(cfg config.DeliveryConfig) config.DeliveryConfig {
	if cfg.WorkerCount < 1 {
		cfg.WorkerCount = 1
	}
	if cfg.PollIntervalMillis < 100 {
		cfg.PollIntervalMillis = 1000
	}
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 6
	}
	if cfg.BaseRetryDelaySeconds < 1 {
		cfg.BaseRetryDelaySeconds = 5
	}
	if cfg.MaxRetryDelaySeconds < cfg.BaseRetryDelaySeconds {
		cfg.MaxRetryDelaySeconds = 300
	}
	if cfg.PerSenderRateLimitPerMinute < 1 {
		cfg.PerSenderRateLimitPerMinute = 120
	}
	if cfg.DNSLookupTimeoutSeconds < 1 {
		cfg.DNSLookupTimeoutSeconds = 3
	}
	return cfg
}

func (s *Service) validateTLSMaterial() error {
	if !s.cfg.EnableTLSCertValidation {
		return nil
	}
	certFile := strings.TrimSpace(s.cfg.TLSCertFile)
	keyFile := strings.TrimSpace(s.cfg.TLSKeyFile)
	if certFile == "" || keyFile == "" {
		return fmt.Errorf("delivery TLS cert validation enabled but tls_cert_file or tls_key_file is empty")
	}
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return fmt.Errorf("failed to load TLS certificate pair: %w", err)
	}
	log.Printf("Delivery TLS certificate material validated successfully")
	return nil
}

func (s *Service) applyPolicy(item *queueItem, now time.Time) error {
	senderDomain := domainFromAddress(item.Message.From)
	if senderDomain != "" && containsDomain(s.cfg.BlockedSenderDomains, senderDomain) {
		return &policyError{message: fmt.Sprintf("blocked sender domain: %s", senderDomain), permanent: true}
	}

	for _, recipient := range item.Message.To {
		recipientDomain := domainFromAddress(recipient)
		if recipientDomain != "" && containsDomain(s.cfg.BlockedRecipientDomains, recipientDomain) {
			return &policyError{message: fmt.Sprintf("blocked recipient domain: %s", recipientDomain), permanent: true}
		}
		if err := s.applyDNSPolicyForRecipient(recipientDomain); err != nil {
			if pErr, ok := err.(*policyError); ok {
				return pErr
			}
			return &policyError{message: err.Error(), permanent: false}
		}
	}

	if err := s.applyDNSPolicyForSender(senderDomain); err != nil {
		if pErr, ok := err.(*policyError); ok {
			return pErr
		}
		return &policyError{message: err.Error(), permanent: false}
	}

	sender := strings.ToLower(strings.TrimSpace(item.Message.From))
	if sender == "" {
		return nil
	}
	windowStart := now.Add(-1 * time.Minute)
	history := s.senderWindow[sender]
	filtered := history[:0]
	for _, ts := range history {
		if ts.After(windowStart) {
			filtered = append(filtered, ts)
		}
	}
	if len(filtered) >= s.cfg.PerSenderRateLimitPerMinute {
		s.senderWindow[sender] = filtered
		return &policyError{message: "sender rate limit exceeded", permanent: false}
	}
	filtered = append(filtered, now)
	s.senderWindow[sender] = filtered
	return nil
}

func (s *Service) applyDNSPolicyForRecipient(domain string) error {
	if !s.cfg.EnableDNSPolicyChecks {
		return nil
	}
	if domain == "" || containsDomain(s.cfg.SkipDNSPolicyForDomains, domain) {
		return nil
	}
	if !s.cfg.RequireReachableMX {
		return nil
	}

	resolver := s.resolverForLookup("mx")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.DNSLookupTimeoutSeconds)*time.Second)
	defer cancel()

	mxRecords, err := resolver.LookupMX(ctx, domain)
	if err == nil && len(mxRecords) > 0 {
		return nil
	}

	ipAddrs, ipErr := resolver.LookupIPAddr(ctx, domain)
	if ipErr == nil && len(ipAddrs) > 0 {
		return nil
	}

	return &policyError{message: fmt.Sprintf("recipient domain has no reachable MX/A records: %s", domain), permanent: true}
}

func (s *Service) applyDNSPolicyForSender(domain string) error {
	if !s.cfg.EnableDNSPolicyChecks {
		return nil
	}
	if domain == "" || containsDomain(s.cfg.SkipDNSPolicyForDomains, domain) {
		return nil
	}
	if len(s.cfg.DNSBLZones) == 0 {
		return nil
	}

	resolver := s.resolverForLookup("dnsbl")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.DNSLookupTimeoutSeconds)*time.Second)
	defer cancel()

	ipAddrs, err := resolver.LookupIPAddr(ctx, domain)
	if err != nil || len(ipAddrs) == 0 {
		return nil
	}

	for _, addr := range ipAddrs {
		v4 := addr.IP.To4()
		if v4 == nil {
			continue
		}
		reversed := fmt.Sprintf("%d.%d.%d.%d", v4[3], v4[2], v4[1], v4[0])
		for _, zone := range s.cfg.DNSBLZones {
			zoneName := strings.Trim(strings.TrimSpace(zone), ".")
			if zoneName == "" {
				continue
			}
			queryName := fmt.Sprintf("%s.%s", reversed, zoneName)
			listed, lookupErr := resolver.LookupHost(ctx, queryName)
			if lookupErr == nil && len(listed) > 0 {
				return &policyError{message: fmt.Sprintf("sender domain listed in DNSBL (%s) via %s", zoneName, queryName), permanent: true}
			}
		}
	}

	return nil
}

func (s *Service) resolverForLookup(kind string) *net.Resolver {
	dnsServer := ""
	switch kind {
	case "mx":
		dnsServer = strings.TrimSpace(s.cfg.MXDNSServerAddress)
	case "dnsbl":
		dnsServer = strings.TrimSpace(s.cfg.DNSBLDNSServerAddress)
		if dnsServer == "" && s.cfg.UseSystemResolverForDNSBL {
			return net.DefaultResolver
		}
	}

	if dnsServer == "" {
		dnsServer = strings.TrimSpace(s.cfg.DNSServerAddress)
	}
	if dnsServer == "" {
		return net.DefaultResolver
	}

	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := net.Dialer{}
			return dialer.DialContext(ctx, "udp", dnsServer)
		},
	}
}

func domainFromAddress(address string) string {
	trimmed := strings.ToLower(strings.TrimSpace(address))
	at := strings.LastIndex(trimmed, "@")
	if at == -1 || at == len(trimmed)-1 {
		return ""
	}
	return strings.TrimSpace(trimmed[at+1:])
}

func containsDomain(list []string, domain string) bool {
	for _, entry := range list {
		if strings.EqualFold(strings.TrimSpace(entry), domain) {
			return true
		}
	}
	return false
}
