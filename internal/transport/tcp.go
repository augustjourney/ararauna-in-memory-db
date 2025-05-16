package transport

import (
	"ararauna/internal/command"
	"ararauna/internal/parser"
	"ararauna/internal/toolbox"
	"ararauna/pkg/concurrency"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

type Server struct {
	tb       *toolbox.Toolbox
	handler  *command.Handler
	mu       sync.RWMutex
	listener net.Listener
	sem      concurrency.Semaphore
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
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	s.tb.Logger.Info("listening", zap.Stringer("addr", ln.Addr()))

	if max := s.tb.Cfg.Server.MaxConnections; max > 0 {
		s.sem = concurrency.NewSemaphore(max)
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close()
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

		if !s.sem.TryAcquire() {
			s.tb.Metrics.IncConnRejected()
			s.rejectConn(conn)
			continue
		}

		s.wg.Add(1)
		go s.handleConn(ctx, conn)
	}
	s.wg.Wait()
	return nil
}

func (s *Server) Addr() net.Addr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

func (s *Server) rejectConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
	_, _ = conn.Write([]byte("-ERR max number of clients reached\r\n"))
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer s.sem.Release()
	defer s.wg.Done()
	defer func() { _ = conn.Close() }()
	s.tb.Metrics.IncConnActive()
	defer s.tb.Metrics.DecConnActive()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
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
