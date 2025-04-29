package parser

import (
	"ararauna/internal/errs"
	"bufio"
	"fmt"
	"io"
	"strconv"
)

type Kind byte

const (
	KindSimpleString Kind = '+'
	KindError        Kind = '-'
	KindInteger      Kind = ':'
	KindBulkString   Kind = '$'
	KindArray        Kind = '*'
)

type Value struct {
	Kind  Kind
	Str   string
	Int   int64
	Bulk  []byte
	Array []Value
	Null  bool
}

func SimpleString(s string) Value {
	return Value{Kind: KindSimpleString, Str: s}
}

func Error(err string) Value {
	return Value{Kind: KindError, Str: err}
}

func Integer(n int64) Value {
	return Value{Kind: KindInteger, Int: n}
}

func Bulk(b []byte) Value {
	return Value{Kind: KindBulkString, Bulk: b}
}

func NullBulk() Value {
	return Value{Kind: KindBulkString, Null: true}
}

func Array(items []Value) Value {
	return Value{Kind: KindArray, Array: items}
}

func Decode(r *bufio.Reader) (Value, error) {
	kindByte, err := r.ReadByte()
	if err != nil {
		return Value{}, err
	}
	switch Kind(kindByte) {
	case KindSimpleString:
		line, err := readLine(r)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindSimpleString, Str: string(line)}, nil
	case KindError:
		line, err := readLine(r)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindError, Str: string(line)}, nil
	case KindInteger:
		n, err := readInt(r)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindInteger, Int: n}, nil
	case KindBulkString:
		return decodeBulk(r)
	case KindArray:
		return decodeArray(r)
	default:
		return Value{}, fmt.Errorf("%w: %q", errs.ErrUnknownRESPKind, kindByte)
	}
}

func decodeBulk(r *bufio.Reader) (Value, error) {
	n, err := readInt(r)
	if err != nil {
		return Value{}, err
	}
	if n < 0 {
		return Value{Kind: KindBulkString, Null: true}, nil
	}
	buf := make([]byte, n+2)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Value{}, err
	}
	if buf[n] != '\r' || buf[n+1] != '\n' {
		return Value{}, errs.ErrMalformedRESP
	}
	return Value{Kind: KindBulkString, Bulk: buf[:n]}, nil
}

func decodeArray(r *bufio.Reader) (Value, error) {
	n, err := readInt(r)
	if err != nil {
		return Value{}, err
	}
	if n < 0 {
		return Value{Kind: KindArray, Null: true}, nil
	}
	items := make([]Value, n)
	for i := range items {
		v, err := Decode(r)
		if err != nil {
			return Value{}, err
		}
		items[i] = v
	}
	return Value{Kind: KindArray, Array: items}, nil
}

func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, errs.ErrMalformedRESP
	}
	return line[:len(line)-2], nil
}

func readInt(r *bufio.Reader) (int64, error) {
	line, err := readLine(r)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(line), 10, 64)
}

func Encode(v Value, w io.Writer) error {
	switch v.Kind {
	case KindSimpleString:
		_, err := fmt.Fprintf(w, "+%s\r\n", v.Str)
		return err
	case KindError:
		_, err := fmt.Fprintf(w, "-%s\r\n", v.Str)
		return err
	case KindInteger:
		_, err := fmt.Fprintf(w, ":%d\r\n", v.Int)
		return err
	case KindBulkString:
		if v.Null {
			_, err := io.WriteString(w, "$-1\r\n")
			return err
		}
		if _, err := fmt.Fprintf(w, "$%d\r\n", len(v.Bulk)); err != nil {
			return err
		}
		if _, err := w.Write(v.Bulk); err != nil {
			return err
		}
		_, err := io.WriteString(w, "\r\n")
		return err
	case KindArray:
		if v.Null {
			_, err := io.WriteString(w, "*-1\r\n")
			return err
		}
		if _, err := fmt.Fprintf(w, "*%d\r\n", len(v.Array)); err != nil {
			return err
		}
		for _, item := range v.Array {
			if err := Encode(item, w); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: %v", errs.ErrUnknownRESPKind, v.Kind)
	}
}
