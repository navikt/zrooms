package zoom_test

import (
	"testing"

	"github.com/navikt/zrooms/internal/zoom"
)

func TestNewAPIManager(t *testing.T) {
	// Test that we can create an API manager
	manager := zoom.NewAPIManager()
	if manager == nil {
		t.Error("Expected non-nil API manager")
	}
}

func TestNewAPIClient(t *testing.T) {
	// Test that we can create an API client with an access token
	client := zoom.NewAPIClient("test-token")
	if client == nil {
		t.Error("Expected non-nil API client")
	}
}
