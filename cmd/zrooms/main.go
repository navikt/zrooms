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
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
	"github.com/navikt/zrooms/internal/web"
)

func main() {
	// Initialize the repository
	repo := memory.NewRepository()

	// Initialize the service layer
	roomService := service.NewRoomService(repo)

	// Set up API routes with repository
	mux := api.SetupRoutes(repo)

	// Set up web UI routes
	webHandler, err := web.NewHandler(roomService, "./internal/web/templates", 30) // 30-second refresh rate
	if err != nil {
		log.Fatalf("Failed to initialize web handler: %v", err)
	}
	webHandler.SetupRoutes(mux)

	// Get port from environment variable or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Configure the HTTP server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
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
