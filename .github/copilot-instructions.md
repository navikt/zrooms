# Zroom - Zoom Room Status Application

This document provides guidelines for developing the Zroom application, a Golang API and website that handles Zoom meeting events via webhooks and displays meeting status.

## Project Overview

Zroom is a Go application that:
1. Receives webhook notifications from Zoom about meeting events
2. Processes and stores meeting status information with participant counts
3. Provides a web interface to view meeting status

## Development Approach

We follow test-driven development (TDD):
1. Write tests first
2. Implement the minimal code to pass the tests
3. Refactor while keeping tests passing

## Technology Stack

- **Language**: Go 1.24+
- **Framework**: Go standard libraries
- **Testing**: Go standard testing package with testify for assertions
- **Database**: Valkey/Redis for storage to handle application state
- **Web Interface**: HTML, CSS, and minimal JavaScript
- **Webhook Integration**: Zoom API

## Project Structure

```
zrooms/
├── .github/
│   └── copilot-instructions.md  # This file
├── cmd/
│   └── zrooms/               # Application entrypoint
│       └── main.go
├── internal/
│   ├── api/                  # API handlers
│   │   ├── handlers.go
│   │   └── routes.go
│   ├── config/               # Application configuration
│   │   └── config.go
│   ├── models/               # Data models
│   │   └── meeting.go
│   ├── repository/           # Data access layer
│   │   ├── interface.go
│   │   └── memory/           # In-memory implementation
│   │       └── repository.go
│   ├── service/              # Business logic
│   │   └── meeting_service.go
│   ├── web/                  # Web UI
│   │   ├── handlers.go
│   │   ├── templates/
│   │   └── static/
│   └── zoom/                 # Zoom webhook processing
│       ├── events.go
│       └── webhook.go
├── tests/                    # Integration tests
│   └── api_test.go
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## Coding Standards

### Go Code Standards

1. Follow [Effective Go](https://golang.org/doc/effective_go) guidelines
2. Use meaningful variable and function names
3. Document public functions, types, and packages
4. Handle errors explicitly
5. Use dependency injection for better testability
6. Keep functions small and focused on a single responsibility

### Test Conventions

1. Name test files with `_test.go` suffix
2. Use table-driven tests where appropriate
3. Aim for high test coverage, especially for business logic
4. Write both unit and integration tests
5. Mock external dependencies in unit tests

## Zoom Webhook Integration

- The application should validate webhook requests from Zoom
- Handle the following Zoom events:
  - Meeting created
  - Meeting started
  - Meeting ended
  - Participant joined
  - Participant left
- Store relevant meeting data for status display

## Web Interface Requirements

1. Display a list of meetings with status
2. Show current occupancy of each meeting
3. Provide filtering and sorting options
4. Use a responsive design for mobile compatibility
5. Auto-refresh data at appropriate intervals

## Development Workflow

1. Create feature branches from main
2. Write tests first
3. Implement code to pass tests
4. Refactor as needed
5. Submit pull requests for review
6. Merge after approval and passing CI checks

## Deployment

- The application should be containerized for deployment
- Environment variables should be used for configuration
- Logs should be structured in JSON format for easier analysis
- API endpoints should be documented

## Security Considerations

1. Validate and sanitize all input
2. Use HTTPS for all communications
3. Implement proper authentication for admin functions
4. Protect sensitive data
5. Dont expose internal errors to users
6. Avoid personally identifiable information as much as possible

This document serves as a guide for GitHub Copilot when assisting with code development. It should be updated as the project evolves.