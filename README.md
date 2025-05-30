# Zrooms - Zoom Room Status Dashboard

Zrooms is a Go application that tracks and displays the status of Zoom meetings. It receives webhook notifications from Zoom about meeting events, processes and stores this information, and provides a web interface to monitor meeting status in real-time.

## Features

- **Real-time Meeting Status**: Displays what meetings are taking place
- **Live Updates via SSE**: Server-Sent Events provide real-time updates without page refreshes
- **Participant Tracking**: Shows how many participants are in each meeting
- **Auto-refreshing Dashboard**: Automatically updates to show the latest meeting status (fallback for browsers without SSE support)
- **Zoom Webhook Integration**: Processes Zoom meeting events (creation, start, end, participant changes)
- **Health Check Endpoints**: API endpoints for monitoring application health
- **Graceful Degradation**: Works well across different browsers with appropriate fallbacks

## Architecture

Zrooms follows a clean architecture approach with the following components:

- **API Layer**: Handles HTTP requests and webhook events from Zoom
- **Service Layer**: Contains the business logic for room and meeting management
- **Repository Layer**: Provides data storage and retrieval abstraction
- **Web Interface**: Displays room and meeting status in a user-friendly dashboard
- **SSE Manager**: Manages real-time client connections and updates

## Getting Started

### Prerequisites

- Go 1.24 or higher
- Zoom account with webhook capabilities
- Redis (optional, falls back to in-memory storage)

### Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/navikt/zrooms.git
   cd zrooms
   ```

2. Build the application:
   ```bash
   make build
   ```

3. Run the application:
   ```bash
   ./bin/zrooms
   ```

### Docker

You can also run Zrooms using Docker:

```bash
docker build -t zrooms .
docker run -p 8080:8080 zrooms
```

### Configuration

Configuration is handled through environment variables:

- `PORT`: HTTP server port (default: 8080)
- `REFRESH_RATE`: Web UI auto-refresh interval in seconds (default: 30)
- `LOG_LEVEL`: Logging level (default: info)
- `USE_SSE`: Enable/disable Server-Sent Events (default: true)
- `ZOOM_CLIENT_ID`: Zoom OAuth client ID
- `ZOOM_CLIENT_SECRET`: Zoom OAuth client secret
- `ZOOM_REDIRECT_URI`: Redirect URI for Zoom OAuth
- `ZOOM_WEBHOOK_URL`: URL for Zoom to send webhook events
- `ZOOM_WEBHOOK_SECRET_TOKEN`: Secret token for validating Zoom webhook requests
- `REDIS_URL`: Redis connection URL (optional)

## Usage

### Web Interface

Access the web dashboard at `http://localhost:8080/` to view room and meeting status. The interface updates automatically when meeting statuses change.

### Demo Mode

To populate the application with sample data for demonstration purposes:

```bash
./scripts/demo-data.sh
```

This script will create sample rooms and meetings, simulating real Zoom activity.

## Technical Details

### Server-Sent Events (SSE)

Zrooms uses Server-Sent Events to provide real-time updates to the web interface without requiring page refreshes. This creates a more responsive user experience while maintaining simplicity and compatibility.

Key components of the SSE implementation:

- **SSE Manager**: Maintains client connections and broadcasts updates
- **Meeting Service Callbacks**: Notifies the SSE manager when meeting data changes
- **Client-side JavaScript**: Processes SSE events and updates the UI dynamically

For browsers that don't support SSE, the application falls back to traditional page refreshes.

### Repository Layer

Zrooms supports multiple storage backends:

- **In-memory Repository**: Fast, ephemeral storage for development and testing
- **Redis Repository**: Persistent storage for production deployments

The repository interface allows for easy implementation of additional storage options.

## Development

### Project Structure

```
zrooms/
├── cmd/zrooms/           # Application entrypoint
├── internal/
│   ├── api/              # API handlers and routes
│   ├── config/           # Application configuration
│   ├── models/           # Data models
│   ├── repository/       # Data access layer
│   │   ├── memory/       # In-memory implementation
│   │   └── redis/        # Redis implementation
│   ├── service/          # Business logic
│   └── web/              # Web UI templates and handlers
│       ├── static/       # CSS and JavaScript
│       └── templates/    # HTML templates
└── scripts/              # Utility scripts
```

### Running Tests

```bash
make test
```

## License

This project is licensed under the MIT License - see the [LICENSE.md](LICENSE.md) file for details.

## Contact

For any questions, issues, or feature requests, please reach out to the AppSec team:
- Internal: Either our slack channel [#appsec](https://nav-it.slack.com/archives/C06P91VN27M) or contact a [team member](https://teamkatalogen.nav.no/team/02ed767d-ce01-49b5-9350-ee4c984fd78f) directly via slack/teams/mail.
- External: [Open GitHub Issue](https://github.com/navikt/appsec-github-watcher/issues/new/choose)

## Code generated by GitHub Copilot

This project was developed with the assistance of GitHub Copilot, an AI-powered code completion tool.