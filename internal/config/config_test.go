package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartupRequiresWebhookSecretAndDurableStorageOnNAIS(t *testing.T) {
	t.Setenv("ZOOM_WEBHOOK_SECRET_TOKEN", "")
	t.Setenv("NAIS_CLUSTER_NAME", "")
	t.Setenv("GCS_BUCKET", "")
	require.ErrorContains(t, Validate(), "ZOOM_WEBHOOK_SECRET_TOKEN")
	t.Setenv("ZOOM_WEBHOOK_SECRET_TOKEN", "test-secret")
	require.NoError(t, Validate())
	t.Setenv("NAIS_CLUSTER_NAME", "test-cluster")
	require.ErrorContains(t, Validate(), "GCS_BUCKET")
	t.Setenv("GCS_BUCKET", "test-bucket")
	require.NoError(t, Validate())
}
