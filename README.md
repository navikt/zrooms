# Zrooms - Zoom Room Status Dashboard

Zrooms is a Go application that tracks and displays the status of Zoom meetings. It receives webhook notifications from Zoom about meeting events, processes and stores this information, and provides a web interface to monitor meeting status in real-time.

## Features

- **Real-time Meeting Status**: Displays what meetings are taking place
- **Live Updates via SSE**: Server-Sent Events provide real-time updates without page refreshes
- **Participant Tracking**: Shows how many participants are in each meeting
- **Auto-refreshing Dashboard**: Automatically updates to show the latest meeting status (fallback for browsers without SSE support)
- **Zoom Webhook Integration**: Processes Zoom meeting events (creation, start, end, participant changes)
- **Admin Ownership Details**: Shows the Zoom host ID and resolves the owner's email separately from creator/editor emails
- **App Deauthorization**: Removes stored meetings belonging to a user who uninstalls the Zoom app
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

- Go version matching `go.mod`
- Zoom account with webhook capabilities
- Google Cloud Storage for production; no external storage is needed locally

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
   export ZOOM_WEBHOOK_SECRET_TOKEN=local-development-secret
   ./bin/zrooms
   ```

### Docker

You can also run Zrooms using Docker:

```bash
docker build -t zrooms .
docker run -p 8080:8080 -e ZOOM_WEBHOOK_SECRET_TOKEN=local-development-secret zrooms
```

### Configuration

Configuration is handled through environment variables:

- `PORT`: HTTP server port (default: 8080)
- `ZOOM_CLIENT_ID`: Zoom OAuth client ID
- `ZOOM_CLIENT_SECRET`: Zoom OAuth client secret
- `ZOOM_REDIRECT_URI`: Redirect URI for Zoom OAuth
- `ZOOM_WEBHOOK_URL`: URL for Zoom to send webhook events
- `ZOOM_WEBHOOK_SECRET_TOKEN`: Required secret for validating all webhook requests. Startup fails without it.
- `GCS_BUCKET`: Meeting-state bucket. Required on NAIS; omitted locally to use volatile memory.
- `NAIS_TOKEN_INTROSPECTION_ENDPOINT`: NAIS-injected endpoint for validating Entra ID access tokens
- `NAV_IDENT_ADMINS`: Comma-separated allowlist for admin access

On NAIS, GCS uses the application's workload identity through Application Default Credentials, not a service-account key. The existing `zrooms` secret supplies Zoom configuration. Production does not fall back to memory if GCS fails.

## Usage

### Web Interface

Access the web dashboard at `http://localhost:8080/` to view room and meeting status. The interface updates automatically when meeting statuses change.

### Admin meeting owners

The authenticated admin dashboard, meeting list, and detail page show the meeting owner using Zoom's `host_id`. Creator and last-editor emails remain separate because someone can schedule or edit a meeting on another host's behalf.

Use the existing Zoom OAuth app and the existing `ZOOM_CLIENT_ID`, `ZOOM_CLIENT_SECRET`, and `ZOOM_REDIRECT_URI`. No separate Server-to-Server app or `ZOOM_API_*` variables are required. Enable the `user:read:user` scope (classic equivalent: `user:read`) so the application can identify the authorizing user through `/users/me`. The optional admin **Raw** action also needs `meeting:read:meeting` (classic equivalent: `meeting:read`). Keep the existing meeting webhook subscriptions and their required permissions. Users must authorize again after scopes change.

Start installation using **Add Your Meetings** on the dashboard, which opens `/oauth/start`. This sets a signed, ten-minute, HttpOnly, SameSite=Lax state cookie and redirects to Zoom. `/oauth/redirect` requires that cookie and matching state, exchanges the code using the configured redirect URI, and identifies the token owner before persisting anything. Both paths are explicitly excluded from NAIS automatic Entra login; admin authentication is unchanged. Unsolicited or expired callbacks, including direct Marketplace callbacks without state, display a link to restart through `/oauth/start`. Production redirect URIs must use HTTPS; HTTP is allowed only on localhost for development. Existing installations must use this flow once because older versions did not retain OAuth tokens.

Emails are fetched only while serving authenticated admin pages, once per distinct host in that response, with a five-second total lookup deadline. Each lookup uses that host's own authorization and verifies the returned user ID; an account administrator's installation does not automatically supply authorizations for other hosts. Emails are not persisted, logged, or included in the public dashboard. If authorization, scopes, or Zoom are unavailable, the page shows a notice and keeps the host ID visible.

### OAuth token storage and refresh

Access and rotating refresh tokens are encrypted with AES-256-GCM using a purpose-specific key derived from the existing `ZOOM_CLIENT_SECRET`; the Zoom user ID is authenticated as associated data. The private GCS bucket holds one encrypted record per hashed user ID under `oauth/v1/`, separate from `meetings/v1/`. Tokens are never included in meeting objects, pages, or logs. Local development uses an encrypted in-memory store and loses authorizations on restart.

The existing bucket's seven-day lifecycle also applies to OAuth records, measured from their last write. Successful refresh writes a new record. If no lookup triggers refresh for seven days, the owner must authorize again. A missing authorization never falls back to another user's tokens. Keep soft delete, object versioning, and retention policies disabled as described below, so uninstall cleanup does not leave recoverable token copies.

Generation preconditions serialize token rotation across concurrent requests or overlapping pods. A refresh claim is persisted before contacting Zoom, and the rotated tokens must replace that exact generation. Uninstall or a newer installation therefore cannot be overwritten by an older in-flight refresh. A failed or interrupted refresh is not blindly retried with a potentially consumed refresh token: other callers wait briefly for completion, then require reauthorization. Monitor OAuth errors and ask affected owners to use **Add Your Meetings** again.

Rotating `ZOOM_CLIENT_SECRET` also changes the encryption key and invalidates in-progress state cookies. Coordinate rotation with removal of the old `oauth/v1/` objects by a bucket administrator, then have users authorize again. Do not delete meeting objects. Keep the old key available for recovery until this procedure is complete; the application deliberately does not overwrite undecryptable records or treat corruption as missing credentials.

### Zoom app deauthorization

Set the existing Zoom app's **Deauthorization Notification Endpoint URL** to `https://<application-host>/webhook/deauthorization`. Keep `ZOOM_CLIENT_ID` and `ZOOM_WEBHOOK_SECRET_TOKEN` set to that app's values. The NAIS manifest exempts this callback from browser login redirects; the application verifies Zoom signatures instead.

The endpoint accepts signed `app_deauthorized` POST requests and signed `endpoint.url_validation` challenges using the same timestamp and HMAC verification as `/webhook`. A payload must contain `account_id`, `user_id`, the matching `client_id`, and `deauthorization_time`; `event_ts` is not required. Unsigned requests are rejected, including legacy requests that contain only a payload-level `signature`.

Cleanup first replaces the user's OAuth credentials with a minimal revocation marker, preventing an older in-flight authorization or refresh from restoring them. It then matches the notification's `user_id` to stored meeting host IDs, not creator/editor emails or participant identifiers, removes meeting content, and notifies connected dashboards after persisted deletions. Duplicate notifications are safe, concurrent changes are rechecked using generation preconditions, and storage failures return `503` so Zoom can retry partial cleanup.

Deletion markers prevent events at or before the deauthorization time from restoring the deleted meetings for seven days. A delayed uninstall notification does not delete meeting state with newer event timestamps. This cleanup covers currently stored meetings with a known host ID; it is not an account-wide revocation registry. Previously unseen meeting IDs, events newer than the uninstall time, and meetings without a known host ID are not blocked. Configure Zoom to stop delivering events after uninstall, and monitor notification failures.

### Demo Mode

To populate the application with sample data for demonstration purposes:

```bash
./scripts/demo-data.sh
```

This script requires Python 3 and the same exported `ZOOM_WEBHOOK_SECRET_TOKEN` as the local application. It signs its sample webhook requests; signature verification is not disabled for demo mode.

## Technical Details

### Server-Sent Events (SSE)

Zrooms uses Server-Sent Events to provide real-time updates to the web interface without requiring page refreshes. This creates a more responsive user experience while maintaining simplicity and compatibility.

Key components of the SSE implementation:

- **SSE Manager**: Maintains client connections and broadcasts updates
- **Meeting Service Callbacks**: Notifies the SSE manager when meeting data changes
- **Client-side JavaScript**: Processes SSE events and updates the UI dynamically

Each connection has its own bounded update signal. Bursts are coalesced because the browser fetches a complete snapshot. HTMX also refreshes the list every 60 seconds and when SSE reconnects, so a missed notification or pod replacement does not leave the page permanently stale. This fallback still requires JavaScript and HTMX; without them the initial server-rendered page is static.

### Repository Layer

Zrooms supports multiple storage backends:

- **In-memory Repository**: Fast, ephemeral storage for development and testing
- **GCS Repository**: One private JSON object per meeting ID, containing the latest occurrence, minimal host/creator metadata, and participant identifiers with ordering markers. Participant names and emails are not persisted.

Both backends share the same event reducer. Metadata updates do not change the lifecycle status; participant events are ordered independently per participant; ended occurrences cannot be restarted by delayed events. A newer start/join with another UUID begins a new occurrence. This is a latest-state dashboard, not a historical record of every occurrence.

GCS writes compare object generations and retry conflicts after rereading. A single pod can still receive concurrent HTTP requests or overlap a replacement during deployment. Reads do not use a process-local cache. Listing reads each meeting object, which is intentionally optimized for simplicity at very low traffic, not large datasets.

State is hidden seven days after its last accepted update. GCS lifecycle cleanup deletes objects asynchronously at age seven days; overwriting an object creates a new generation and restarts its age. No bucket retention lock is configured because it would prevent updates and admin deletion. Admin deletion removes meeting content and keeps a minimal seven-day marker to prevent older webhook retries from restoring it; genuinely newer events can create new state.

Cloud Storage enables seven-day soft delete by default, including for overwritten objects. To avoid retaining recovery copies beyond the agreed retention window, **disable soft delete and object versioning on the new bucket in the Google Cloud console before running this application**. Leave object retention policies unset so updates and admin deletion remain possible. The current NAIS bucket manifest fields do not expose soft delete or versioning settings. These settings are managed manually; the application does not validate them. Lifecycle cleanup itself is asynchronous, so expiry is not a guarantee of physical deletion at the exact seven-day boundary.

Disabling these settings does not erase previously soft-deleted or noncurrent generations. If the bucket has already held data, its administrator must assess those existing copies separately.

### Webhook delivery and operations

Supported events are `meeting.created`, `meeting.updated`, `meeting.started`, `meeting.ended`, `meeting.participant_joined`, and `meeting.participant_left`. Requests are size-limited before signature verification, require a signature timestamp within five minutes, and use the event timestamp for ordering. The header timestamp is distinct from `event_ts`, so delayed retries can still carry an older event.

An event is acknowledged only after persistence completes. Storage failures return `503` within a 2.5-second processing deadline, allowing Zoom to retry. There is no durable queue or automatic reconciliation: Zoom's finite retry window means a prolonged outage can still lose events. Monitor webhook failures and Zoom delivery logs, and reconcile affected meetings after such an outage.

`/health/live` reports process liveness. `/health/ready` checks object listing and reading when the bucket is nonempty; it does not validate bucket policies or prove write permission. The application does not require `storage.buckets.get`. Logs are JSON in the running application and do not intentionally contain participant payloads or admin identities.

### Deployment and rollback

The existing workflow provisions `nav-appsec-zrooms-prod-meetings` through `.nais/nais.yml` and deploys one application replica. The bucket name must be globally available; keep the manifest name and `GCS_BUCKET` value identical. Bucket provisioning and IAM propagation must complete before startup succeeds. Configure the bucket policies manually as described above; startup does not enforce them. No Redis instance or credentials are needed.

Outbound policy permits `zoom.us` for token requests and `api.zoom.us` for admin user/meeting lookups. NAIS allows `private.googleapis.com` by default; bucket access relies on the platform's private Google API routing. Startup/readiness verify bucket connectivity, not Zoom API credentials or permissions. This has not been checked against the live cluster's DNS or network policy.

The previous deployment used in-memory state, so there is no automatic data migration: currently running meetings may need subsequent events or manual reconciliation after cutover. Rolling back to the previous binary also returns to volatile storage and does not read the bucket; the bucket data itself is not removed. Verify readiness, signed webhook persistence across a pod replacement, admin access, and delivery to two dashboard clients after deployment. A local production-image smoke test with a fake GCS endpoint passed within the existing 32 MiB limit; this is not a production capacity measurement.

## Development

### Running Tests

```bash
make test
```

The race-enabled suite runs with `go test -race ./...`. An optional Docker integration check exercises the actual production image against a fake GCS HTTP endpoint, including persistence across a container restart:

```bash
docker build -t zrooms:test .
ZROOMS_TEST_IMAGE=zrooms:test go test ./internal/repository/gcs -run TestProductionImagePersistence -count=1
```

## License

This project is licensed under the MIT License - see the [LICENSE.md](LICENSE.md) file for details.

## Contact

For any questions, issues, or feature requests, please reach out to the AppSec team:
- Internal: Either our slack channel [#appsec](https://nav-it.slack.com/archives/C06P91VN27M) or contact a [team member](https://teamkatalogen.nav.no/team/02ed767d-ce01-49b5-9350-ee4c984fd78f) directly via slack/teams/mail.
- External: [Open GitHub Issue](https://github.com/navikt/appsec-github-watcher/issues/new/choose)

## Code generated by GitHub Copilot

This project was developed with the assistance of GitHub Copilot, an AI-powered code completion tool.