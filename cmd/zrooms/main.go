package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/storage"
	"github.com/navikt/zrooms/internal/api"
	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/repository/gcs"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
	"github.com/navikt/zrooms/internal/web"
	"github.com/navikt/zrooms/internal/zoom"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := config.Validate(); err != nil {
		log.Fatal(err)
	}
	var repo repository.Repository
	var tokenBackend zoom.TokenBackend
	if bucket := os.Getenv("GCS_BUCKET"); bucket != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		client, err := storage.NewClient(ctx, storage.WithJSONReads())
		if err != nil {
			cancel()
			log.Fatalf("Failed to create GCS client: %v", err)
		}
		defer client.Close()
		repo = repository.NewStore(gcs.NewBackend(client, bucket))
		tokenBackend = zoom.NewGCSTokenBackend(client, bucket)
		err = repo.Check(ctx)
		cancel()
		if err != nil {
			log.Fatalf("Failed to access meeting bucket: %v", err)
		}
		log.Print("Using GCS meeting storage")
	} else {
		log.Print("Using volatile in-memory storage for local development")
		repo = memory.NewRepository()
		tokenBackend = zoom.NewMemoryTokenBackend()
	}
	zoomConfig := config.GetZoomConfig()
	oauth := zoom.NewAPIManager(zoomConfig, zoom.NewTokenStore(tokenBackend, zoomConfig.ClientSecret))

	// Initialize the service layer
	meetingService := service.NewMeetingService(repo)

	// Set up web UI routes
	webHandler, err := web.NewHandler(meetingService, "./internal/web/templates")
	if err != nil {
		log.Fatalf("Failed to initialize web handler: %v", err)
	}

	// Set up admin routes
	adminHandler, err := web.NewAdminHandler(meetingService, repo, "./internal/web/templates", oauth)
	if err != nil {
		log.Fatalf("Failed to initialize admin handler: %v", err)
	}

	// Register the SSE update callback with the meeting service
	meetingService.RegisterUpdateCallback(webHandler.NotifyMeetingUpdate)

	// Set up API routes with repository and meeting service
	mux := api.SetupRoutes(repo, meetingService, oauth)

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
