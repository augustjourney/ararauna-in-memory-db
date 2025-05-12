package transport

import (
	"ararauna/internal/command"
	"ararauna/internal/config"
	"ararauna/internal/parser"
	"ararauna/internal/storage"
	"ararauna/internal/toolbox"
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func startTestServer(t *testing.T) (*Server, net.Conn) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg := config.Default()
	cfg.Server.Port = 0
	cfg.Storage.PartitionsNumber = 4
	cfg.WAL.Enabled = false
	tb := toolbox.New(cfg, zap.NewNop())
	store, err := storage.New(ctx, tb, nil)
	require.NoError(t, err)
	h := command.New(tb, store)
	srv := New(tb, h)

	started := make(chan struct{})
	go func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		srv.listener = ln
		close(started)

		go func() {
			<-ctx.Done()
			ln.Close()
		}()

		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			srv.wg.Add(1)
			go srv.handleConn(ctx, conn)
		}
	}()

	<-started
	conn, err := net.Dial("tcp", srv.listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return srv, conn
}

func send(t *testing.T, w *bufio.Writer, parts ...string) {
	t.Helper()
	items := make([]parser.Value, len(parts))
	for i, p := range parts {
		items[i] = parser.Bulk([]byte(p))
	}
	arr := parser.Array(items)
	require.NoError(t, parser.Encode(arr, w))
	require.NoError(t, w.Flush())
}

func recv(t *testing.T, r *bufio.Reader) parser.Value {
	t.Helper()
	v, err := parser.Decode(r)
	require.NoError(t, err)
	return v
}

func TestServer_PingGetSetDel(t *testing.T) {
	_, conn := startTestServer(t)
	require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))

	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)

	send(t, w, "PING")
	assert.Equal(t, parser.SimpleString("PONG"), recv(t, r))

	send(t, w, "SET", "foo", "bar")
	assert.Equal(t, parser.SimpleString("OK"), recv(t, r))

	send(t, w, "GET", "foo")
	assert.Equal(t, parser.Bulk([]byte("bar")), recv(t, r))

	send(t, w, "DEL", "foo")
	assert.Equal(t, parser.Integer(1), recv(t, r))

	send(t, w, "GET", "foo")
	assert.Equal(t, parser.NullBulk(), recv(t, r))
}

func TestServer_UnknownCommand(t *testing.T) {
	_, conn := startTestServer(t)
	require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))

	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)

	send(t, w, "BOGUS")
	resp := recv(t, r)
	assert.Equal(t, parser.KindError, resp.Kind)
	assert.Contains(t, strings.ToLower(resp.Str), "unknown command")
}
