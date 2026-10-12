package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"RedisShake/internal/log"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logDir, err := os.MkdirTemp("", "redis-shake-config-test")
	if err != nil {
		panic(err)
	}
	log.Init("info", "shake.log", logDir, false, 0, 0, 0, false)
	code := m.Run()
	os.RemoveAll(logDir)
	os.Exit(code)
}

// configMapDir lays out a directory the way kubelet mounts a ConfigMap: the file is a symlink
// through ..data, and an update atomically repoints ..data to a new directory.
type configMapDir struct {
	t       *testing.T
	dir     string
	version int
}

func newConfigMapDir(t *testing.T, content string) *configMapDir {
	d := &configMapDir{t: t, dir: t.TempDir()}
	d.update(content)
	require.NoError(t, os.Symlink(filepath.Join("..data", "shake.toml"), d.path()))
	return d
}

func (d *configMapDir) path() string {
	return filepath.Join(d.dir, "shake.toml")
}

func (d *configMapDir) update(content string) {
	d.version++
	versionDir := filepath.Join(d.dir, fmt.Sprintf("..v%d", d.version))
	require.NoError(d.t, os.Mkdir(versionDir, 0o755))
	require.NoError(d.t, os.WriteFile(filepath.Join(versionDir, "shake.toml"), []byte(content), 0o644))
	tmp := filepath.Join(d.dir, "..data_tmp")
	require.NoError(d.t, os.Symlink(filepath.Base(versionDir), tmp))
	require.NoError(d.t, os.Rename(tmp, filepath.Join(d.dir, "..data")))
	if d.version > 1 {
		require.NoError(d.t, os.RemoveAll(filepath.Join(d.dir, fmt.Sprintf("..v%d", d.version-1))))
	}
}

func watch(t *testing.T, path string) <-chan *viper.Viper {
	changes := make(chan *viper.Viper, 10)
	WatchConfig(path, func(v *viper.Viper) { changes <- v })
	return changes
}

func TestWatchConfigReloadsConfigMapUpdate(t *testing.T) {
	t.Setenv("SHAKE_TEST_PASSWORD", "secret")
	d := newConfigMapDir(t, "[scan_reader]\ncount = 1\npassword = \"${SHAKE_TEST_PASSWORD}\"\n")
	changes := watch(t, d.path())

	d.update("[scan_reader]\ncount = 500\npassword = \"${SHAKE_TEST_PASSWORD}\"\n")
	select {
	case v := <-changes:
		require.Equal(t, 500, v.GetInt("scan_reader.count"))
		require.Equal(t, "secret", v.GetString("scan_reader.password"))
	case <-time.After(5 * time.Second):
		t.Fatal("config change was not detected")
	}
}

func TestWatchConfigSkipsInvalidFile(t *testing.T) {
	d := newConfigMapDir(t, "[scan_reader]\ncount = 1\n")
	changes := watch(t, d.path())

	d.update("[scan_reader\ncount = 500\n")
	d.update("[scan_reader]\ncount = 7\n")
	select {
	case v := <-changes:
		require.Equal(t, 7, v.GetInt("scan_reader.count"), "the invalid file must not be delivered")
	case <-time.After(5 * time.Second):
		t.Fatal("config change was not detected")
	}
}

func TestChangedKeys(t *testing.T) {
	read := func(content string) *viper.Viper {
		path := filepath.Join(t.TempDir(), "shake.toml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		v, err := readConfig(path)
		require.NoError(t, err)
		return v
	}
	old := read("[scan_reader]\ncount = 1\naddress = \"a:6379\"\n[advanced]\nlog_interval = 5\n")
	changed := read("[scan_reader]\ncount = 2\naddress = \"b:6379\"\n[advanced]\nlog_interval = 5\nstatus_port = 8080\n")
	require.Equal(t, []string{"advanced.status_port", "scan_reader.address", "scan_reader.count"}, ChangedKeys(old, changed))
}
