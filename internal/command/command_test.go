package command

import (
	"ararauna/internal/config"
	"ararauna/internal/parser"
	"ararauna/internal/storage"
	"ararauna/internal/toolbox"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg := config.Default()
	cfg.Storage.PartitionsNumber = 4
	cfg.WAL.Enabled = false

	tb := toolbox.New(cfg, zap.NewNop())
	s, err := storage.New(ctx, tb, nil)

	require.NoError(t, err)

	return New(tb, s)
}

func cmd(parts ...string) parser.Value {
	items := make([]parser.Value, len(parts))

	for i, p := range parts {
		items[i] = parser.Bulk([]byte(p))
	}

	return parser.Array(items)
}

func TestDispatch_Ping(t *testing.T) {
	d := newTestHandler(t)

	assert.Equal(t, parser.SimpleString("PONG"), d.Dispatch(context.Background(), cmd("PING")))
	assert.Equal(t, parser.Bulk([]byte("hello")), d.Dispatch(context.Background(), cmd("PING", "hello")))
}

func TestDispatch_SetGet(t *testing.T) {
	d := newTestHandler(t)
	ctx := context.Background()

	assert.Equal(t, parser.SimpleString("OK"), d.Dispatch(ctx, cmd("SET", "foo", "bar")))
	assert.Equal(t, parser.Bulk([]byte("bar")), d.Dispatch(ctx, cmd("GET", "foo")))
}

func TestDispatch_Get_Missing(t *testing.T) {
	d := newTestHandler(t)
	assert.Equal(t, parser.NullBulk(), d.Dispatch(context.Background(), cmd("GET", "missing")))
}

func TestDispatch_Del(t *testing.T) {
	d := newTestHandler(t)
	ctx := context.Background()

	require.Equal(t, parser.SimpleString("OK"), d.Dispatch(ctx, cmd("SET", "a", "1")))
	require.Equal(t, parser.SimpleString("OK"), d.Dispatch(ctx, cmd("SET", "b", "2")))

	assert.Equal(t, parser.Integer(2), d.Dispatch(ctx, cmd("DEL", "a", "b", "missing")))
	assert.Equal(t, parser.NullBulk(), d.Dispatch(ctx, cmd("GET", "a")))
}

func TestDispatch_Del_AllMissing(t *testing.T) {
	d := newTestHandler(t)
	assert.Equal(t, parser.Integer(0), d.Dispatch(context.Background(), cmd("DEL", "x", "y")))
}

func TestDispatch_CaseInsensitive(t *testing.T) {
	d := newTestHandler(t)
	ctx := context.Background()

	assert.Equal(t, parser.SimpleString("OK"), d.Dispatch(ctx, cmd("set", "k", "v")))
	assert.Equal(t, parser.Bulk([]byte("v")), d.Dispatch(ctx, cmd("Get", "k")))
}

func TestDispatch_UnknownCommand(t *testing.T) {
	d := newTestHandler(t)
	resp := d.Dispatch(context.Background(), cmd("FOO", "bar"))

	assert.Equal(t, parser.KindError, resp.Kind)
	assert.Contains(t, resp.Str, "unknown command")
	assert.Contains(t, resp.Str, "foo")
}

func TestDispatch_WrongArity(t *testing.T) {
	d := newTestHandler(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  parser.Value
	}{
		{"ping too many", cmd("PING", "a", "b")},
		{"get no args", cmd("GET")},
		{"get too many", cmd("GET", "a", "b")},
		{"set no args", cmd("SET")},
		{"set missing value", cmd("SET", "k")},
		{"set too many", cmd("SET", "k", "v", "EX", "10")},
		{"del no args", cmd("DEL")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := d.Dispatch(ctx, tc.req)
			assert.Equal(t, parser.KindError, resp.Kind)
			assert.Contains(t, resp.Str, "wrong number of arguments")
		})
	}
}

func TestDispatch_InvalidRequest(t *testing.T) {
	d := newTestHandler(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  parser.Value
	}{
		{"not array", parser.SimpleString("PING")},
		{"empty array", parser.Array(nil)},
		{"null array", parser.Value{Kind: parser.KindArray, Null: true}},
		{"non-bulk element", parser.Array([]parser.Value{parser.Integer(1)})},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := d.Dispatch(ctx, tc.req)
			assert.Equal(t, parser.KindError, resp.Kind)
			assert.Contains(t, resp.Str, "invalid request")
		})
	}
}

func TestDispatch_ContextCancelled(t *testing.T) {
	d := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp := d.Dispatch(ctx, cmd("SET", "k", "v"))
	assert.Equal(t, parser.KindError, resp.Kind)
	assert.Contains(t, resp.Str, "context canceled")
}
