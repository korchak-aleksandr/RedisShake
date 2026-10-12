package client

import (
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCountingConnCountsReceivedBytes(t *testing.T) {
	server, conn := net.Pipe()
	defer server.Close()
	c := &countingConn{Conn: conn}
	defer c.Close()

	go func() {
		_, _ = server.Write([]byte("hello"))
		_, _ = server.Write([]byte(" world"))
	}()
	buf := make([]byte, 11)
	_, err := io.ReadFull(c, buf)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(buf))
	require.EqualValues(t, 11, c.bytesRead.Load())

	// Bytes sent are not counted.
	go func() { _, _ = io.ReadAll(server) }()
	_, err = c.Write([]byte("ping"))
	require.NoError(t, err)
	require.EqualValues(t, 11, c.bytesRead.Load())
}
