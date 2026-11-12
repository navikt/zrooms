package stats

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/navikt/zrooms/internal/repository"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
)

const (
	gcsPrefix     = "stats/v1/"
	maxObjectSize = 4 << 20
)

// GCSBackend stores one object per meeting and month in the meeting bucket.
type GCSBackend struct {
	bucket *storage.BucketHandle
}

func NewGCSBackend(client *storage.Client, bucket string) *GCSBackend {
	return &GCSBackend{bucket: client.Bucket(bucket)}
}

func gcsObjectName(meetingID, month string) string {
	hash := sha256.Sum256([]byte(meetingID))
	return gcsPrefix + hex.EncodeToString(hash[:]) + "/" + month + ".json"
}

func (b *GCSBackend) readRaw(ctx context.Context, name string) ([]byte, int64, error) {
	reader, err := b.bucket.Object(name).NewReader(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil, 0, repository.ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read statistics object: %w", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxObjectSize+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read statistics data: %w", err)
	}
	if len(data) > maxObjectSize {
		return nil, 0, errors.New("statistics object exceeds 4 MiB limit")
	}
	return data, reader.Attrs.Generation, nil
}

func (b *GCSBackend) writeRaw(ctx context.Context, name string, data []byte, version int64) error {
	if len(data) > maxObjectSize {
		return errors.New("statistics object exceeds 4 MiB limit")
	}
	condition := storage.Conditions{GenerationMatch: version}
	if version == 0 {
		condition = storage.Conditions{DoesNotExist: true}
	}
	writer := b.bucket.Object(name).If(condition).NewWriter(ctx)
	writer.ContentType = "application/json"
	writer.CacheControl = "no-store"
	writer.ChunkSize = 0
	if _, err := writer.Write(data); err != nil {
		_ = writer.CloseWithError(err)
		return fmt.Errorf("write statistics object: %w", err)
	}
	if err := writer.Close(); err != nil {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == 412 {
			return repository.ErrConflict
		}
		return fmt.Errorf("persist statistics object: %w", err)
	}
	return nil
}

func decodeMonth(data []byte) (*Month, error) {
	var month Month
	if err := json.Unmarshal(data, &month); err != nil {
		return nil, fmt.Errorf("decode statistics object: %w", err)
	}
	if month.SchemaVersion != 1 || month.MeetingID == "" || month.Month == "" {
		return nil, errors.New("invalid statistics object schema")
	}
	return &month, nil
}

func (b *GCSBackend) Read(ctx context.Context, meetingID, month string) (*Month, int64, error) {
	data, version, err := b.readRaw(ctx, gcsObjectName(meetingID, month))
	if err != nil {
		return nil, 0, err
	}
	decoded, err := decodeMonth(data)
	return decoded, version, err
}

func (b *GCSBackend) Write(ctx context.Context, month *Month, version int64) error {
	data, err := json.Marshal(month)
	if err != nil {
		return fmt.Errorf("encode statistics object: %w", err)
	}
	return b.writeRaw(ctx, gcsObjectName(month.MeetingID, month.Month), data, version)
}

func monthOf(name string) string {
	return strings.TrimSuffix(path.Base(name), ".json")
}

func (b *GCSBackend) ListMonth(ctx context.Context, month string) ([]*Month, error) {
	objects := b.bucket.Objects(ctx, &storage.Query{Prefix: gcsPrefix})
	var months []*Month
	for {
		attrs, err := objects.Next()
		if errors.Is(err, iterator.Done) {
			return months, nil
		}
		if err != nil {
			return nil, fmt.Errorf("list statistics objects: %w", err)
		}
		if monthOf(attrs.Name) != month {
			continue
		}
		data, _, err := b.readRaw(ctx, attrs.Name)
		if errors.Is(err, repository.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		decoded, err := decodeMonth(data)
		if err != nil {
			return nil, err
		}
		months = append(months, decoded)
	}
}

// Refresh rewrites retained objects created before the cutoff, restarting their lifecycle age.
// Objects older than the retention period are left for the bucket lifecycle rule to delete.
func (b *GCSBackend) Refresh(ctx context.Context, oldestMonth string, createdBefore time.Time) error {
	objects := b.bucket.Objects(ctx, &storage.Query{Prefix: gcsPrefix})
	for {
		attrs, err := objects.Next()
		if errors.Is(err, iterator.Done) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("list statistics objects: %w", err)
		}
		if monthOf(attrs.Name) < oldestMonth || !attrs.Created.Before(createdBefore) {
			continue
		}
		data, version, err := b.readRaw(ctx, attrs.Name)
		if errors.Is(err, repository.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		// A conflict means a concurrent write already created a fresh generation.
		if err := b.writeRaw(ctx, attrs.Name, data, version); err != nil && !errors.Is(err, repository.ErrConflict) {
			return err
		}
	}
}
