package yurid

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"codeberg.org/lewdest/yuri"
	"github.com/r3labs/sse/v2"
)

// Stream names served by the /events endpoint. Every invoice update is
// published to StreamUpdates; updates that leave an invoice fully paid
// are additionally published to StreamPaid.
const (
	StreamUpdates = "updates"
	StreamPaid    = "paid"
)

const (
	// sseHistoryTTL is how long published events stay replayable for
	// reconnecting clients (via the Last-Event-ID header).
	sseHistoryTTL = 5 * time.Hour
	// sseHeartbeatInterval keeps idle connections alive through proxies
	// that drop silent streams (e.g. nginx after 60s).
	sseHeartbeatInterval = 15 * time.Second
	// sseRetryMs is the reconnect delay hint sent to clients.
	sseRetryMs = "5000"
)

type eventServer struct {
	server    *sse.Server
	done      chan struct{}
	closeOnce sync.Once
}

func NewEventServer() *eventServer {
	server := sse.New()
	server.EventTTL = sseHistoryTTL
	server.Headers["X-Accel-Buffering"] = "no"

	server.CreateStream(StreamUpdates)
	server.CreateStream(StreamPaid)

	events := &eventServer{server: server, done: make(chan struct{})}
	go events.heartbeat()

	return events
}

func (e *eventServer) Close() {
	e.closeOnce.Do(func() {
		close(e.done)
		e.server.Close()
	})
}

func (e *eventServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	stream := r.URL.Query().Get("stream")
	if stream == "" {
		stream = StreamUpdates
	}
	if stream != StreamUpdates && stream != StreamPaid {
		writeError(w, http.StatusNotFound, "unknown stream", nil)
		return
	}

	q := r.URL.Query()
	q.Set("stream", stream)
	r.URL.RawQuery = q.Encode()

	// SSE connections outlive the HTTP server's write timeout
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	e.server.ServeHTTP(w, r)
}

func (e *eventServer) PublishInvoice(inv *yuri.Invoice) {
	id, err := invoiceID(inv)
	if err != nil {
		slog.Warn("skipping SSE broadcast for invoice without yurid UUID", "err", err)
		return
	}

	data, err := json.Marshal(wrapInvoice(id, inv))
	if err != nil {
		slog.Warn("skipping SSE broadcast, failed to encode invoice", "err", err)
		return
	}

	// Each stream needs its own Event value: the event log assigns IDs by
	// mutating them. TryPublish keeps the caller (the poller hook) from
	// ever blocking behind a slow or stuck subscriber; a dropped update is
	// fine because the next one supersedes it and reconnecting clients
	// replay history
	if !e.server.TryPublish(StreamUpdates, newInvoiceEvent(data)) {
		slog.Debug("dropped SSE update, stream backlogged", "stream", StreamUpdates)
	}

	if inv.Paid() {
		if !e.server.TryPublish(StreamPaid, newInvoiceEvent(data)) {
			slog.Debug("dropped SSE update, stream backlogged", "stream", StreamPaid)
		}
	}
}

func newInvoiceEvent(data []byte) *sse.Event {
	return &sse.Event{
		Event: []byte("invoice"),
		Data:  data,
		Retry: []byte(sseRetryMs),
	}
}

func (e *eventServer) heartbeat() {
	ticker := time.NewTicker(sseHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.done:
			return
		case <-ticker.C:
			ping := &sse.Event{Comment: []byte("ping")}
			e.server.TryPublish(StreamUpdates, ping)
			e.server.TryPublish(StreamPaid, ping)
		}
	}
}
