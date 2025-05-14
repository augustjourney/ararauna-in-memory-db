package metrics

import (
	"ararauna/internal/config"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func newTestRecorder(t *testing.T, pushURL string) *Recorder {
	t.Helper()
	cfg := config.Default()
	cfg.Metrics = config.Metrics{
		Enabled:      true,
		Provider:     "victoriametrics",
		PushURL:      pushURL,
		PushInterval: 10 * time.Second,
	}
	rec, err := New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func readGzip(r *http.Request) (string, error) {
	gr, err := gzip.NewReader(r.Body)
	if err != nil {
		return "", err
	}
	defer gr.Close()
	b, err := io.ReadAll(gr)
	return string(b), err
}

func TestClose_PushesMetrics(t *testing.T) {
	bodyCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := readGzip(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bodyCh <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	rec := newTestRecorder(t, srv.URL)
	rec.IncSet(false)
	rec.ObserveCommand("GET", time.Millisecond, false)

	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case body := <-bodyCh:
		for _, want := range []string{
			"ararauna_set_total",
			"ararauna_command_duration_seconds",
			"go_goroutines",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("expected %q in push body, got:\n%s", want, body)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for push body")
	}
}
