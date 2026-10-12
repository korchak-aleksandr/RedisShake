package main

import (
	"os"
	"path/filepath"
	"testing"

	"RedisShake/internal/log"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

type scanCountRecorder struct{ counts []int }

func (r *scanCountRecorder) SetScanCount(count int) { r.counts = append(r.counts, count) }

func readTestConfig(t *testing.T, content string) *viper.Viper {
	v := viper.New()
	v.SetConfigFile(filepath.Join(t.TempDir(), "shake.toml"))
	require.NoError(t, os.WriteFile(v.ConfigFileUsed(), []byte(content), 0o644))
	require.NoError(t, v.ReadInConfig())
	return v
}

func TestApplyConfigChangeSetsScanCount(t *testing.T) {
	log.Init("info", "shake.log", t.TempDir(), false, 0, 0, 0, false)
	loaded := readTestConfig(t, "[scan_reader]\naddress = \"a:6379\"\ncount = 100\n")
	r := &scanCountRecorder{}

	applyConfigChange(loaded, readTestConfig(t, "[scan_reader]\naddress = \"a:6379\"\ncount = 30\n"), r)
	// Other changes are not applied, but COUNT still is.
	applyConfigChange(loaded, readTestConfig(t, "[scan_reader]\naddress = \"b:6379\"\ncount = 40\n"), r)
	// Without count in the file, COUNT falls back to the default, as on startup.
	applyConfigChange(loaded, readTestConfig(t, "[scan_reader]\naddress = \"a:6379\"\n"), r)

	require.Equal(t, []int{30, 40, 1}, r.counts)
}
