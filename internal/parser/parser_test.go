package parser

import (
	"ararauna/internal/errs"
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecode_SimpleString(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("+OK\r\n"))
	v, err := Decode(r)
	require.NoError(t, err)
	assert.Equal(t, SimpleString("OK"), v)
}

func TestDecode_Error(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("-ERR bad thing\r\n"))
	v, err := Decode(r)
	require.NoError(t, err)
	assert.Equal(t, Error("ERR bad thing"), v)
}

func TestDecode_Integer(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(":42\r\n"))
	v, err := Decode(r)
	require.NoError(t, err)
	assert.Equal(t, Integer(42), v)
}

func TestDecode_BulkString(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("$5\r\nhello\r\n"))
	v, err := Decode(r)
	require.NoError(t, err)
	assert.Equal(t, Bulk([]byte("hello")), v)
}

func TestDecode_BulkString_Empty(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("$0\r\n\r\n"))
	v, err := Decode(r)
	require.NoError(t, err)
	assert.Equal(t, KindBulkString, v.Kind)
	assert.Empty(t, v.Bulk)
	assert.False(t, v.Null)
}

func TestDecode_BulkString_Null(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("$-1\r\n"))
	v, err := Decode(r)
	require.NoError(t, err)
	assert.Equal(t, NullBulk(), v)
}

func TestDecode_BulkString_Binary(t *testing.T) {
	payload := []byte{0x00, 0x01, '\r', '\n', 0xff}
	buf := bytes.NewBuffer(nil)
	buf.WriteString("$5\r\n")
	buf.Write(payload)
	buf.WriteString("\r\n")

	v, err := Decode(bufio.NewReader(buf))
	require.NoError(t, err)
	assert.Equal(t, payload, v.Bulk)
}

func TestDecode_Array(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"))
	v, err := Decode(r)
	require.NoError(t, err)

	require.Equal(t, KindArray, v.Kind)
	require.Len(t, v.Array, 3)
	assert.Equal(t, []byte("SET"), v.Array[0].Bulk)
	assert.Equal(t, []byte("foo"), v.Array[1].Bulk)
	assert.Equal(t, []byte("bar"), v.Array[2].Bulk)
}

func TestDecode_Array_Null(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("*-1\r\n"))
	v, err := Decode(r)
	require.NoError(t, err)
	assert.Equal(t, KindArray, v.Kind)
	assert.True(t, v.Null)
}

func TestDecode_UnknownKind(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("?wat\r\n"))
	_, err := Decode(r)
	assert.ErrorIs(t, err, errs.ErrUnknownRESPKind)
}

func TestDecode_EOF(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(""))
	_, err := Decode(r)
	assert.ErrorIs(t, err, io.EOF)
}

func TestDecode_MalformedLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("+OK\n"))
	_, err := Decode(r)
	assert.ErrorIs(t, err, errs.ErrMalformedRESP)
}

func TestDecode_MalformedBulkTrailer(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("$3\r\nfooXX"))
	_, err := Decode(r)
	assert.ErrorIs(t, err, errs.ErrMalformedRESP)
}

func TestEncode(t *testing.T) {
	tests := []struct {
		name string
		v    Value
		want string
	}{
		{"simple string", SimpleString("OK"), "+OK\r\n"},
		{"error", Error("ERR bad"), "-ERR bad\r\n"},
		{"integer", Integer(123), ":123\r\n"},
		{"bulk", Bulk([]byte("hello")), "$5\r\nhello\r\n"},
		{"bulk empty", Bulk([]byte{}), "$0\r\n\r\n"},
		{"null bulk", NullBulk(), "$-1\r\n"},
		{"array", Array([]Value{Bulk([]byte("foo")), Integer(7)}), "*2\r\n$3\r\nfoo\r\n:7\r\n"},
		{"null array", Value{Kind: KindArray, Null: true}, "*-1\r\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, Encode(tc.v, &buf))
			assert.Equal(t, tc.want, buf.String())
		})
	}
}

func TestEncode_UnknownKind(t *testing.T) {
	v := Value{Kind: Kind('?')}
	err := Encode(v, io.Discard)
	assert.ErrorIs(t, err, errs.ErrUnknownRESPKind)
}

func TestRoundtrip(t *testing.T) {
	cmd := Array([]Value{Bulk([]byte("SET")), Bulk([]byte("k")), Bulk([]byte("v"))})

	var buf bytes.Buffer
	require.NoError(t, Encode(cmd, &buf))

	got, err := Decode(bufio.NewReader(&buf))
	require.NoError(t, err)
	assert.Equal(t, cmd, got)
}

func TestDecode_Pipeline(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("+OK\r\n:42\r\n$3\r\nfoo\r\n"))

	v1, err := Decode(r)
	require.NoError(t, err)
	assert.Equal(t, SimpleString("OK"), v1)

	v2, err := Decode(r)
	require.NoError(t, err)
	assert.Equal(t, Integer(42), v2)

	v3, err := Decode(r)
	require.NoError(t, err)
	assert.Equal(t, Bulk([]byte("foo")), v3)

	_, err = Decode(r)
	assert.True(t, errors.Is(err, io.EOF))
}
