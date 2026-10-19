package gcs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
)

type storedObject struct {
	data       []byte
	generation int64
}

type fakeBucket struct {
	mu         sync.Mutex
	objects    map[string]storedObject
	generation int64
	fail       bool
}

func (b *fakeBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fail {
		http.Error(w, "bucket permission denied", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodPost {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		parts := multipart.NewReader(r.Body, params["boundary"])
		part, err := parts.NextPart()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var metadata struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(part).Decode(&metadata); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		part, err = parts.NextPart()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(part)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		version, err := strconv.ParseInt(r.URL.Query().Get("ifGenerationMatch"), 10, 64)
		if err != nil {
			http.Error(w, "conditional write required", http.StatusBadRequest)
			return
		}
		if b.objects[metadata.Name].generation != version {
			http.Error(w, `{"error":{"code":412,"message":"precondition failed"}}`, http.StatusPreconditionFailed)
			return
		}
		b.generation++
		b.objects[metadata.Name] = storedObject{data: data, generation: b.generation}
		var checksum [4]byte
		binary.BigEndian.PutUint32(checksum[:], crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)))
		_ = json.NewEncoder(w).Encode(map[string]string{
			"name": metadata.Name, "generation": strconv.FormatInt(b.generation, 10),
			"crc32c": base64.StdEncoding.EncodeToString(checksum[:]),
		})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/o") {
		items := make([]map[string]string, 0, len(b.objects))
		for name, object := range b.objects {
			items = append(items, map[string]string{
				"name": name, "generation": strconv.FormatInt(object.generation, 10),
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
		return
	}
	_, name, found := strings.Cut(r.URL.Path, "/o/")
	object, exists := b.objects[name]
	if !found || !exists {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Goog-Generation", strconv.FormatInt(object.generation, 10))
	w.Header().Set("X-Goog-Metageneration", "1")
	w.Header().Set("Content-Length", strconv.Itoa(len(object.data)))
	_, _ = w.Write(object.data)
}

func testBackend(t *testing.T) (*Backend, *fakeBucket, *storage.Client) {
	t.Helper()
	bucket := &fakeBucket{objects: make(map[string]storedObject)}
	server := httptest.NewServer(bucket)
	t.Cleanup(server.Close)
	client, err := storage.NewClient(context.Background(),
		option.WithEndpoint(server.URL), option.WithoutAuthentication(), storage.WithJSONReads())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return NewBackend(client, "test-bucket"), bucket, client
}

func TestGCSRoundTripAndConcurrentGenerations(t *testing.T) {
	backend, bucket, client := testBackend(t)
	ctx := context.Background()
	require.NoError(t, backend.Check(ctx))
	store := repository.NewStore(backend)
	base := time.Now().Add(-time.Minute).UnixMilli()
	_, err := store.ApplyEvent(ctx, models.MeetingEvent{
		Type: "meeting.created", EventTS: base,
		Meeting: models.Meeting{ID: "123", Topic: "Meeting", OperatorEmail: "creator@example.test", Host: models.Participant{ID: "host-123"}},
	})
	require.NoError(t, err)
	_, err = store.ApplyEvent(ctx, models.MeetingEvent{
		Type: "meeting.started", EventTS: base + 1,
		Meeting: models.Meeting{ID: "123", UUID: "instance", StartTime: time.UnixMilli(base)},
	})
	require.NoError(t, err)
	_, err = store.ApplyEvent(ctx, models.MeetingEvent{
		Type: "meeting.participant_joined", EventTS: base + 2, ParticipantID: "guest",
		Meeting: models.Meeting{ID: "123", UUID: "instance"},
	})
	require.NoError(t, err)

	// A fresh repository has no process-local state to recover from.
	restarted := repository.NewStore(NewBackend(client, "test-bucket"))
	meeting, err := restarted.GetMeeting(ctx, "123")
	require.NoError(t, err)
	require.Equal(t, "Meeting", meeting.Topic)
	require.Equal(t, "host-123", meeting.Host.ID)
	require.Equal(t, "creator@example.test", meeting.CreatorEmail)
	require.Equal(t, 1, meeting.ParticipantCount)
	all, err := restarted.ListAllMeetings(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)

	first, generation, err := backend.Read(ctx, "123")
	require.NoError(t, err)
	second, sameGeneration, err := backend.Read(ctx, "123")
	require.NoError(t, err)
	require.Equal(t, generation, sameGeneration)
	first.Meeting.Topic = "First writer"
	require.NoError(t, backend.Write(ctx, "123", first, generation))
	second.Meeting.Topic = "Stale writer"
	require.ErrorIs(t, backend.Write(ctx, "123", second, sameGeneration), repository.ErrConflict)
	require.NoError(t, restarted.DeleteMeeting(ctx, "123"))
	_, err = restarted.GetMeeting(ctx, "123")
	require.ErrorIs(t, err, repository.ErrNotFound)
	bucket.mu.Lock()
	deleted := string(bucket.objects[objectName("123")].data)
	bucket.mu.Unlock()
	require.NotContains(t, deleted, "creator@example.test")
	require.NotContains(t, deleted, "host-123")
	require.NotContains(t, deleted, "guest")
}

func TestGCSExpiryAndFailures(t *testing.T) {
	backend, bucket, _ := testBackend(t)
	ctx := context.Background()
	state := &repository.State{
		SchemaVersion: 1, Meeting: models.Meeting{ID: "123", Topic: "Expired"},
		UpdatedAt: time.Now().Add(-repository.Retention - time.Minute),
	}
	require.NoError(t, backend.Write(ctx, "123", state, 0))
	store := repository.NewStore(backend)
	all, err := store.ListAllMeetings(ctx)
	require.NoError(t, err)
	require.Empty(t, all)
	_, err = store.GetMeeting(ctx, "123")
	require.ErrorIs(t, err, repository.ErrNotFound)
	_, err = store.ApplyEvent(ctx, models.MeetingEvent{
		Type: "meeting.created", EventTS: time.Now().UnixMilli(), Meeting: models.Meeting{ID: "123", Topic: "New"},
	})
	require.NoError(t, err)
	bucket.mu.Lock()
	bucket.fail = true
	bucket.mu.Unlock()
	require.Error(t, backend.Check(ctx))
	_, err = store.ListAllMeetings(ctx)
	require.Error(t, err)
	_, err = store.ApplyEvent(ctx, models.MeetingEvent{
		Type: "meeting.updated", EventTS: time.Now().UnixMilli(), Meeting: models.Meeting{ID: "123"},
	})
	require.Error(t, err)
}

func TestCorruptObjectIsNotAnEmptyDashboard(t *testing.T) {
	backend, bucket, _ := testBackend(t)
	bucket.mu.Lock()
	bucket.objects[objectName("123")] = storedObject{data: []byte(`{"schema_version":99}`), generation: 1}
	bucket.mu.Unlock()
	_, err := repository.NewStore(backend).ListAllMeetings(context.Background())
	require.ErrorContains(t, err, "schema")
}

func TestProductionImagePersistence(t *testing.T) {
	image := os.Getenv("ZROOMS_TEST_IMAGE")
	if image == "" {
		t.Skip("set ZROOMS_TEST_IMAGE to test a locally built production image")
	}
	bucket := &fakeBucket{objects: make(map[string]storedObject)}
	server := httptest.NewUnstartedServer(bucket)
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server.Listener = listener
	server.Start()
	defer server.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		require.NoError(t, err, string(output))
		return strings.TrimSpace(string(output))
	}
	container := docker("run", "--detach", "--memory=32m", "--publish", "127.0.0.1::8080",
		"--add-host", "host.docker.internal:host-gateway",
		"--env", "ZOOM_WEBHOOK_SECRET_TOKEN=container-test-secret", "--env", "GCS_BUCKET=test-bucket",
		"--env", "NAIS_CLUSTER_NAME=container-test",
		"--env", "STORAGE_EMULATOR_HOST=http://host.docker.internal:"+strconv.Itoa(port), image)
	defer func() {
		if t.Failed() {
			t.Log("Container state:", docker("inspect", "--format", "{{json .State}}", container))
			t.Log("Container logs:", docker("logs", container))
		}
		docker("rm", "--force", container)
	}()
	address := "http://" + docker("port", container, "8080/tcp")
	client := &http.Client{Timeout: 3 * time.Second}
	waitReady := func() {
		t.Helper()
		require.Eventually(t, func() bool {
			response, err := client.Get(address + "/health/ready")
			if err != nil {
				return false
			}
			defer response.Body.Close()
			return response.StatusCode == http.StatusOK
		}, 10*time.Second, 100*time.Millisecond)
	}
	waitReady()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	body := `{"event":"meeting.started","event_ts":` + strconv.FormatInt(time.Now().UnixMilli(), 10) +
		`,"payload":{"object":{"id":"123","uuid":"container-instance","topic":"Survives restart"}}}`
	mac := hmac.New(sha256.New, []byte("container-test-secret"))
	_, _ = io.WriteString(mac, "v0:"+timestamp+":"+body)
	request, err := http.NewRequest(http.MethodPost, address+"/webhook", strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("x-zm-request-timestamp", timestamp)
	request.Header.Set("x-zm-signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	response, err := client.Do(request)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	docker("restart", "--time", "10", container)
	address = "http://" + docker("port", container, "8080/tcp")
	waitReady()
	response, err = client.Get(address + "/")
	require.NoError(t, err)
	page, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Contains(t, string(page), "Survives restart")
	t.Log("Container memory:", docker("stats", "--no-stream", "--format", "{{.MemUsage}}", container))
}
