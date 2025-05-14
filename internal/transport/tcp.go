package transport

import (
	"ararauna/internal/command"
	"ararauna/internal/parser"
	"ararauna/internal/toolbox"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"go.uber.org/zap"
)

type Server struct {
	tb       *toolbox.Toolbox
	handler  *command.Handler
	listener net.Listener
	wg       sync.WaitGroup
}

func New(tb *toolbox.Toolbox, h *command.Handler) *Server {
	return &Server{
		tb:      tb,
		handler: h,
	}
}

func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.tb.Cfg.Server.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = ln
	s.tb.Logger.Info("listening", zap.Stringer("addr", ln.Addr()))

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			s.tb.Logger.Error("accept failed", zap.Error(err))
			continue
		}
		s.tb.Metrics.IncConnAccepted()
		s.wg.Add(1)
		go s.handleConn(ctx, conn)
	}
	s.wg.Wait()
	return nil
}

func (s *Server) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	s.tb.Metrics.IncConnActive()
	defer s.tb.Metrics.DecConnActive()

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		req, err := parser.Decode(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				s.tb.Logger.Warn("decode failed",
					zap.Stringer("remote", conn.RemoteAddr()),
					zap.Error(err),
				)
			}
			return
		}

		resp := s.handler.Dispatch(ctx, req)
		if err := parser.Encode(resp, writer); err != nil {
			s.tb.Logger.Error("encode failed",
				zap.Stringer("remote", conn.RemoteAddr()),
				zap.Error(err),
			)
			return
		}
		if err := writer.Flush(); err != nil {
			return
		}
	}
}
