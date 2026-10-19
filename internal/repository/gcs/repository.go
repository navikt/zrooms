package gcs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	"github.com/navikt/zrooms/internal/repository"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
)

const prefix = "meetings/v1/"

type Backend struct {
	bucket *storage.BucketHandle
}

func NewBackend(client *storage.Client, bucket string) *Backend {
	return &Backend{bucket: client.Bucket(bucket)}
}

func objectName(id string) string {
	hash := sha256.Sum256([]byte(id))
	return prefix + hex.EncodeToString(hash[:]) + ".json"
}

func (b *Backend) Read(ctx context.Context, id string) (*repository.State, int64, error) {
	return b.readObject(ctx, b.bucket.Object(objectName(id)))
}

func (b *Backend) readObject(ctx context.Context, object *storage.ObjectHandle) (*repository.State, int64, error) {
	reader, err := object.NewReader(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil, 0, repository.ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read meeting object: %w", err)
	}
	defer reader.Close()
	var state repository.State
	data, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read meeting data: %w", err)
	}
	if len(data) > 4<<20 {
		return nil, 0, fmt.Errorf("meeting object exceeds 4 MiB limit")
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, 0, fmt.Errorf("decode meeting object: %w", err)
	}
	if state.SchemaVersion != 1 || state.Meeting.ID == "" || state.UpdatedAt.IsZero() {
		return nil, 0, fmt.Errorf("invalid meeting object schema")
	}
	return &state, reader.Attrs.Generation, nil
}

func (b *Backend) Write(ctx context.Context, id string, state *repository.State, version int64) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode meeting object: %w", err)
	}
	if len(data) > 4<<20 {
		return fmt.Errorf("meeting object exceeds 4 MiB limit")
	}
	condition := storage.Conditions{GenerationMatch: version}
	if version == 0 {
		condition = storage.Conditions{DoesNotExist: true}
	}
	writer := b.bucket.Object(objectName(id)).If(condition).NewWriter(ctx)
	writer.ContentType = "application/json"
	writer.CacheControl = "no-store"
	// Lifecycle age restarts for each accepted event because every write creates a generation.
	writer.ChunkSize = 0
	if _, err := writer.Write(data); err != nil {
		_ = writer.CloseWithError(err)
		return fmt.Errorf("encode meeting object: %w", err)
	}
	if err := writer.Close(); err != nil {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == 412 {
			return repository.ErrConflict
		}
		return fmt.Errorf("persist meeting object: %w", err)
	}
	return nil
}

func (b *Backend) List(ctx context.Context) ([]*repository.State, error) {
	objects := b.bucket.Objects(ctx, &storage.Query{Prefix: prefix})
	var states []*repository.State
	for {
		attrs, err := objects.Next()
		if errors.Is(err, iterator.Done) {
			return states, nil
		}
		if err != nil {
			return nil, fmt.Errorf("list meeting objects: %w", err)
		}
		state, _, err := b.readObject(ctx, b.bucket.Object(attrs.Name))
		// Lifecycle deletion may happen between listing and reading.
		if errors.Is(err, repository.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
}

func (b *Backend) Check(ctx context.Context) error {
	// Verify listing and, when nonempty, reading without writing a probe object.
	objects := b.bucket.Objects(ctx, &storage.Query{Prefix: prefix})
	object, err := objects.Next()
	if errors.Is(err, iterator.Done) {
		return nil
	}
	if err != nil {
		return err
	}
	_, _, err = b.readObject(ctx, b.bucket.Object(object.Name))
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	return err
}
