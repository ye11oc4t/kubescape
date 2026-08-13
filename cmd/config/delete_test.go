package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kubescape/kubescape/v3/core/cautils"
	"github.com/kubescape/kubescape/v3/core/cautils/getter"
	"github.com/kubescape/kubescape/v3/core/core"
	"github.com/kubescape/kubescape/v3/core/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDeleteCmd(t *testing.T) {
	// Create a mock Kubescape interface
	mockKubescape := &mocks.MockIKubescape{}

	// Call the GetConfigCmd function
	configCmd := getDeleteCmd(mockKubescape)

	// Verify the command name and short description
	assert.Equal(t, "delete", configCmd.Use)
	assert.Equal(t, "Delete cached configurations", configCmd.Short)
	assert.Equal(t, "", configCmd.Long)
}

func TestDeleteCmdPropagatesCacheRemovalFailure(t *testing.T) {
	originalStore := getter.DefaultLocalStore
	getter.DefaultLocalStore = t.TempDir()
	t.Cleanup(func() { getter.DefaultLocalStore = originalStore })

	configPath := cautils.ConfigFileFullPath()
	// A non-empty directory at the cache-file path makes os.Remove fail
	// reliably without depending on platform-specific permission handling.
	require.NoError(t, os.MkdirAll(configPath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(configPath, "keep"), []byte("data"), 0o600))

	cmd := getDeleteCmd(core.NewKubescape(context.Background()))
	err := cmd.Execute()

	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	assert.Equal(t, "remove", pathErr.Op)
	assert.Equal(t, configPath, pathErr.Path)
	_, statErr := os.Stat(configPath)
	require.NoError(t, statErr, "failed deletion must leave the cache path in place")
}

func TestDeleteCmdMissingCacheIsIdempotent(t *testing.T) {
	originalStore := getter.DefaultLocalStore
	getter.DefaultLocalStore = t.TempDir()
	t.Cleanup(func() { getter.DefaultLocalStore = originalStore })

	cmd := getDeleteCmd(core.NewKubescape(context.Background()))
	require.NoError(t, cmd.Execute())
}
