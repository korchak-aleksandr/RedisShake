package reader

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"RedisShake/internal/client"

	"github.com/stretchr/testify/require"
)

// A COUNT changed while the scan is running applies to the next SCAN calls.
func TestScanReaderSetScanCountDuringScan(t *testing.T) {
	const keys = 100000
	src := startRedis(t, "--notify-keyspace-events", "EA")
	args := []interface{}{"MSET"}
	for i := 0; i < keys; i++ {
		args = append(args, "key:"+strconv.Itoa(i), "1")
	}
	redisDo(t, src, args...)

	monitor := client.NewRedisClient(context.Background(), src, "", "", false, client.TlsConfig{}, false)
	t.Cleanup(monitor.Close)
	monitor.Send("MONITOR")
	_, err := monitor.Receive()
	require.NoError(t, err)

	kr := startKsnReader(t, src)
	kr.reader.SetScanCount(1)
	timeout := time.After(10 * time.Second)
	for received := 0; received < 100; {
		select {
		case <-kr.entries:
			received++
		case <-timeout:
			t.Fatal("scan did not start")
		}
	}
	kr.reader.SetScanCount(1000)
	require.EqualValues(t, 1000, kr.reader.scanCount.Load())

	found := make(chan struct{})
	go func() {
		for {
			reply, err := monitor.Receive()
			if err != nil {
				return
			}
			if line, ok := reply.(string); ok && strings.Contains(line, `"scan"`) && strings.HasSuffix(line, `"count" "1000"`) {
				close(found)
				return
			}
		}
	}()
	select {
	case <-found:
	case <-time.After(10 * time.Second):
		t.Fatal("SCAN with the new COUNT was not sent")
	}
	require.Less(t, kr.received.Load(), int64(keys), "scan finished before COUNT was changed")
}

// Bytes received from the source are reported for each connection separately.
func TestScanReaderReportsInBytesPerConnection(t *testing.T) {
	src := startRedis(t, "--notify-keyspace-events", "EA")
	redisDo(t, src, "SET", "before", "1")
	kr := startKsnReader(t, src)
	waitEntry(t, kr.entries, "RESTORE", "before")
	r := kr.reader // the scan is over: it returned the only key in one SCAN

	scanBefore := bytesRead(&r.scanClient)
	subscribeBefore := bytesRead(&r.subscribeClient)
	dumpBefore := bytesRead(&r.dumpClientStat)
	require.Positive(t, scanBefore)

	const n, valueSize = 10, 100000
	value := make([]byte, valueSize) // random, so that DUMP does not compress it
	for i := 0; i < n; i++ {
		_, err := rand.Read(value)
		require.NoError(t, err)
		redisDo(t, src, "SET", "after:"+strconv.Itoa(i), string(value))
	}
	waitEntry(t, kr.entries, "RESTORE", "after:"+strconv.Itoa(n-1))

	require.Equal(t, scanBefore, bytesRead(&r.scanClient), "the scan is over")
	require.Greater(t, bytesRead(&r.subscribeClient), subscribeBefore)
	require.GreaterOrEqual(t, bytesRead(&r.dumpClientStat)-dumpBefore, int64(n*valueSize))
}

// The status reports SCAN COUNT and the bytes received on each connection.
func TestScanReaderStatusFields(t *testing.T) {
	opts := &ScanReaderOptions{Address: "127.0.0.1:0", Count: 10}
	r := NewScanStandaloneReader(context.Background(), opts).(*scanStandaloneReader)
	r.SetScanCount(50)

	var status struct {
		ScanCount int64            `json:"scan_count"`
		InBytes   map[string]int64 `json:"in_bytes"`
	}
	raw, err := json.Marshal(r.Status())
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &status))
	require.EqualValues(t, 50, status.ScanCount)
	require.Equal(t, map[string]int64{"subscribe": 0, "scan": 0, "dump": 0}, status.InBytes)
	require.Contains(t, r.StatusString(), "scan_count=[50], in_bytes=[sub 0 B, scan 0 B, dump 0 B]")
}
