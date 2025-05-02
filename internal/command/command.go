package command

import (
	"ararauna/internal/errs"
	"ararauna/internal/parser"
	"ararauna/internal/storage"
	"ararauna/internal/toolbox"
	"context"
	"errors"
	"fmt"
	"strings"
)

type Handler struct {
	tb      *toolbox.Toolbox
	storage storage.Storage
}

func New(tb *toolbox.Toolbox, s storage.Storage) *Handler {
	return &Handler{tb: tb, storage: s}
}

func (d *Handler) Dispatch(ctx context.Context, req parser.Value) parser.Value {
	if req.Kind != parser.KindArray || req.Null || len(req.Array) == 0 {
		return parser.Error("ERR invalid request")
	}
	for _, item := range req.Array {
		if item.Kind != parser.KindBulkString || item.Null {
			return parser.Error("ERR invalid request")
		}
	}

	name := strings.ToUpper(string(req.Array[0].Bulk))
	args := req.Array[1:]

	switch name {
	case "PING":
		return d.ping(args)
	case "GET":
		return d.get(ctx, args)
	case "SET":
		return d.set(ctx, args)
	case "DEL":
		return d.del(ctx, args)
	default:
		return parser.Error(fmt.Sprintf("ERR unknown command '%s'", strings.ToLower(name)))
	}
}

func (d *Handler) ping(args []parser.Value) parser.Value {
	switch len(args) {
	case 0:
		return parser.SimpleString("PONG")
	case 1:
		return parser.Bulk(args[0].Bulk)
	default:
		return parser.Error("ERR wrong number of arguments for 'ping'")
	}
}

func (d *Handler) get(ctx context.Context, args []parser.Value) parser.Value {
	if len(args) != 1 {
		return parser.Error("ERR wrong number of arguments for 'get'")
	}
	val, err := d.storage.Get(ctx, string(args[0].Bulk))
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return parser.NullBulk()
		}
		return parser.Error(fmt.Sprintf("ERR %v", err))
	}
	return parser.Bulk(val.Data)
}

func (d *Handler) set(ctx context.Context, args []parser.Value) parser.Value {
	if len(args) != 2 {
		return parser.Error("ERR wrong number of arguments for 'set'")
	}
	if err := d.storage.Set(ctx, string(args[0].Bulk), args[1].Bulk, nil); err != nil {
		return parser.Error(fmt.Sprintf("ERR %v", err))
	}
	return parser.SimpleString("OK")
}

func (d *Handler) del(ctx context.Context, args []parser.Value) parser.Value {
	if len(args) == 0 {
		return parser.Error("ERR wrong number of arguments for 'del'")
	}
	var count int64
	for _, arg := range args {
		if _, err := d.storage.Del(ctx, string(arg.Bulk)); err != nil {
			if errors.Is(err, errs.ErrNotFound) {
				continue
			}
			return parser.Error(fmt.Sprintf("ERR %v", err))
		}
		count++
	}
	return parser.Integer(count)
}
