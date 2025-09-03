# Zroom - Zoom Room Status Application

This document provides guidelines for developing the Zroom application, a Golang API and website that handles Zoom meeting events via webhooks and displays meeting status.

## Project Overview

Zroom is a production ready Go application that:
1. Receives webhook notifications from Zoom about meeting events
2. Processes and stores meeting status information with participant counts
3. Provides a web interface to view meeting status
4. Proves a web interface for admins to manage stored meetings and show stats
5. Implements user authentication and authorization used for admin access

## Technology Stack

- **Language**: Go 1.24+
- **Framework**: Go standard libraries
- **Testing**: Go standard testing package with testify for assertions
- **Database**: Valkey/Redis for storage to handle application state
- **Web Interface**: HTML, CSS, HTMX with SSE extension for real-time updates
- **Webhook Integration**: Zoom API

## Coding Standards

### Development flow

- Implement features in small, manageable steps.
- Confirm each step and ask for feedback before proceeding to the next.
- Do not implement more than what is asked or strictly necessary for the current task.
- Functionality should be tested as part of the development process using go test.
- Avoid using shell scripts for testing and verification.

### Go Code Standards

1. Follow [Effective Go](https://golang.org/doc/effective_go) guidelines
2. Use meaningful variable and function names
3. Handle errors explicitly
4. Use dependency injection for better testability
5. Keep functions small and focused on a single responsibility
6. Avoid code duplication
7. Less is more - avoid unnecessary complexity

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
  - Meeting updated
  - Meeting ended
  - Participant joined
  - Participant left
- Store relevant meeting data for status display
- Minimize personally identifiable information, but keep a record of which user creates a meeting and make it accessible to the admin. Preferably by user email.

## Web Interface Requirements

1. Display a list of meetings with status
2. Show current occupancy of each meeting
3. Use a responsive design for mobile compatibility

## Deployment

- The application should be containerized for deployment
- Environment variables should be used for configuration
- Logs should be structured in JSON format for easier analysis

## Security Considerations
1. Validate and sanitize all input
2. Use HTTPS for all communications
3. Implement proper authentication for admin functions
4. Protect sensitive data
5. Dont expose internal errors to users
6. Avoid personally identifiable information as much as possible

This document serves as a guide for GitHub Copilot when assisting with code development. It should be updated as the project evolves.