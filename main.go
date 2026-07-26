package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"mail-server/internal/config"
	"mail-server/internal/delivery"
	"mail-server/internal/imap"
	"mail-server/internal/pop3"
	"mail-server/internal/smtp"
	"mail-server/internal/storage"
	"mail-server/internal/web"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	var configFile = flag.String("config", "config/server.yaml", "Configuration file path")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Channel to listen for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	mailStorage := storage.NewStorage(cfg.Storage.DataDirectory)
	deliveryService, err := delivery.NewService(cfg.Delivery, mailStorage)
	if err != nil {
		log.Fatalf("Failed to initialize delivery service: %v", err)
	}

	var wg sync.WaitGroup

	// Start Delivery worker
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := deliveryService.Start(ctx); err != nil {
			log.Printf("Delivery worker error: %v", err)
		}
	}()

	// Start SMTP server
	smtpServer := smtp.NewServer(cfg.SMTP, cfg.Users, mailStorage, deliveryService)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("Starting SMTP server on %s:%d", cfg.SMTP.Host, cfg.SMTP.Port)
		if err := smtpServer.Start(ctx); err != nil {
			log.Printf("SMTP server error: %v", err)
		}
	}()

	// Start POP3 server
	pop3Server := pop3.NewServer(cfg.POP3)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("Starting POP3 server on %s:%d", cfg.POP3.Host, cfg.POP3.Port)
		if err := pop3Server.Start(ctx); err != nil {
			log.Printf("POP3 server error: %v", err)
		}
	}()

	// Start IMAP server
	imapServer := imap.NewServer(cfg.IMAP)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("Starting IMAP server on %s:%d", cfg.IMAP.Host, cfg.IMAP.Port)
		if err := imapServer.Start(ctx); err != nil {
			log.Printf("IMAP server error: %v", err)
		}
	}()

	// Start Web server
	webServer := web.NewServer(cfg.Web, cfg.Users, mailStorage, deliveryService)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("Starting Web server on %s:%d", cfg.Web.Host, cfg.Web.Port)
		if err := webServer.Start(ctx); err != nil {
			log.Printf("Web server error: %v", err)
		}
	}()

	fmt.Println("Mail server started successfully!")
	fmt.Println("Press Ctrl+C to stop...")

	// Wait for interrupt signal
	<-sigChan
	log.Println("Shutting down servers...")

	// Cancel context to signal all servers to stop
	cancel()

	// Wait for all servers to finish with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All servers shut down gracefully")
	case <-time.After(30 * time.Second):
		log.Println("Timeout waiting for servers to shut down")
	}
}
