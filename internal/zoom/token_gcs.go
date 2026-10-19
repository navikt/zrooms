package zoom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	"github.com/navikt/zrooms/internal/repository"
	"google.golang.org/api/googleapi"
)

type gcsTokenBackend struct {
	bucket *storage.BucketHandle
}

// NewGCSTokenBackend uses a separate, private prefix in the existing bucket.
func NewGCSTokenBackend(client *storage.Client, bucket string) TokenBackend {
	return &gcsTokenBackend{bucket: client.Bucket(bucket)}
}

func (b *gcsTokenBackend) object(id string) *storage.ObjectHandle {
	hash := sha256.Sum256([]byte(id))
	return b.bucket.Object("oauth/v1/" + hex.EncodeToString(hash[:]) + ".enc")
}

func (b *gcsTokenBackend) Read(ctx context.Context, id string) ([]byte, int64, error) {
	reader, err := b.object(id).NewReader(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil, 0, repository.ErrNotFound
	}
	if err != nil {
		return nil, 0, errors.New("read OAuth object failed")
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return nil, 0, errors.New("invalid OAuth object data")
	}
	return data, reader.Attrs.Generation, nil
}

func (b *gcsTokenBackend) Write(ctx context.Context, id string, data []byte, version int64) error {
	if len(data) > 64<<10 {
		return errors.New("OAuth object exceeds size limit")
	}
	condition := storage.Conditions{GenerationMatch: version}
	if version == 0 {
		condition = storage.Conditions{DoesNotExist: true}
	}
	writer := b.object(id).If(condition).NewWriter(ctx)
	writer.ContentType = "application/octet-stream"
	writer.CacheControl = "no-store"
	writer.ChunkSize = 0
	if _, err := writer.Write(data); err != nil {
		_ = writer.CloseWithError(err)
		return errors.New("write OAuth object failed")
	}
	if err := writer.Close(); err != nil {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == 412 {
			return repository.ErrConflict
		}
		return fmt.Errorf("persist OAuth object failed")
	}
	return nil
}
