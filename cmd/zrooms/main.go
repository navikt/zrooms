package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/navikt/zrooms/internal/api"
	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/service"
	"github.com/navikt/zrooms/internal/web"
)

func main() {
	// Get Redis configuration
	redisConfig := config.GetRedisConfig()

	// Initialize the repository using the factory
	repo, err := repository.NewRepository(redisConfig)
	if err != nil {
		log.Fatalf("Failed to initialize repository: %v", err)
	}

	// Check if we're using a Redis repository, and if so, close it properly on exit
	if redisRepo, ok := repo.(interface{ Close() error }); ok {
		defer func() {
			if err := redisRepo.Close(); err != nil {
				log.Printf("Error closing Redis connection: %v", err)
			}
		}()
	}

	// Initialize the service layer
	meetingService := service.NewMeetingService(repo)

	// Set up web UI routes
	webHandler, err := web.NewHandler(meetingService, "./internal/web/templates")
	if err != nil {
		log.Fatalf("Failed to initialize web handler: %v", err)
	}

	// Set up admin routes
	adminHandler, err := web.NewAdminHandler(meetingService, repo, "./internal/web/templates")
	if err != nil {
		log.Fatalf("Failed to initialize admin handler: %v", err)
	}

	// Register the SSE update callback with the meeting service
	meetingService.RegisterUpdateCallback(webHandler.NotifyMeetingUpdate)

	// Set up API routes with repository and meeting service
	mux := api.SetupRoutes(repo, meetingService)

	// Set up web UI routes
	webHandler.SetupRoutes(mux)

	// Set up admin routes
	adminHandler.SetupAdminRoutes(mux)

	// Get port from environment variable or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Configure the HTTP server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux, // Use the mux directly without middleware
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Disable write timeout for SSE connections
		IdleTimeout:  60 * time.Second,
	}

	// Channel to listen for errors coming from the listener.
	serverErrors := make(chan error, 1)

	// Start the server in a goroutine
	go func() {
		log.Printf("Starting zrooms server on port %s", port)
		serverErrors <- server.ListenAndServe()
	}()

	// Channel to listen for an interrupt or terminate signal from the OS
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Block until a signal is received or an error occurs
	select {
	case err := <-serverErrors:
		log.Fatalf("Error starting server: %v", err)

	case <-shutdown:
		log.Println("Shutting down server...")

		// First, shutdown the web handler to close SSE connections
		webHandler.Shutdown()

		// Create a deadline to wait for
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Doesn't block if there are no connections, but will otherwise
		// wait until the timeout deadline.
		if err := server.Shutdown(ctx); err != nil {
			server.Close()
			log.Fatalf("Error shutting down server: %v", err)
		}

		log.Println("Server gracefully stopped")
	}
}
