package obs

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
)

// Ring keeps the most recent log lines in memory so the service can expose
// its own public read-only log view at GET /logs without an external log
// platform. Full history still goes to stdout for the host's log drain.
type Ring struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func NewRing(max int) *Ring {
	return &Ring{max: max}
}

// Write implements io.Writer; slog's JSONHandler emits one line per record.
func (r *Ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := 0
	for i, b := range p {
		if b == '\n' {
			if i > start {
				r.push(string(p[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(p) {
		r.push(string(p[start:]))
	}
	return len(p), nil
}

func (r *Ring) push(line string) {
	r.lines = append(r.lines, line)
	if len(r.lines) > r.max {
		r.lines = r.lines[len(r.lines)-r.max:]
	}
}

func (r *Ring) Snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

// ServeHTTP renders the buffered lines as NDJSON, oldest first.
func (r *Ring) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	lines := r.Snapshot()
	if v := req.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < len(lines) {
			lines = lines[len(lines)-n:]
		}
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	for _, l := range lines {
		io.WriteString(w, l)
		io.WriteString(w, "\n")
	}
}

// NewLogger returns a JSON logger that writes to stdout and the ring buffer.
func NewLogger(ring *Ring) *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.MultiWriter(os.Stdout, ring), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

type ctxKey struct{}

// WithLogger stores a request-scoped logger (carrying request_id etc.) in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// Log returns the request-scoped logger, falling back to the default logger.
func Log(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
