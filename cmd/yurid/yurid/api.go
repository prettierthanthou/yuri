package yurid

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"codeberg.org/lewdest/yuri"
	"github.com/google/uuid"
)

type API struct {
	storage          Database
	instance         *yuri.Instance
	mux              *http.ServeMux
	activeChainNames []string
	apiToken         string
}

func NewAPI(database Database, instance *yuri.Instance, activeChainNames []string, apiToken string) *API {
	api := &API{
		storage:          database,
		instance:         instance,
		mux:              http.NewServeMux(),
		activeChainNames: activeChainNames,
		apiToken:         apiToken,
	}

	api.routes()
	return api
}

func (a *API) Handler() http.Handler {
	return a.mux
}

func (a *API) routes() {
	a.mux.HandleFunc("/sample", a.requireAuth(a.handleSample))
	a.mux.HandleFunc("/active", a.requireAuth(a.handleActive))
	a.mux.HandleFunc("/get", a.requireAuth(a.handleGet))
	a.mux.HandleFunc("/new", a.requireAuth(a.handleNew))
}

// canonicalChain resolves name (case-insensitively) to the canonical name
// of a registered chain.
func (a *API) canonicalChain(name string) (yuri.Chain, bool) {
	for _, active := range a.activeChainNames {
		if strings.EqualFold(active, name) {
			return yuri.Chain(active), true
		}
	}
	return "", false
}

// requireAuth enforces the configured bearer token, if any.
func (a *API) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.apiToken != "" {
			header := r.Header.Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(a.apiToken)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized", nil)
				return
			}
		}

		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string, err error) {
	resp := map[string]any{
		"error": msg,
	}
	if err != nil {
		resp["detail"] = err.Error()
	}

	slog.Error("occurred during a request", "err", resp)
	writeJSON(w, status, resp)
}

func decodeJSON(r *http.Request, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize+1))
	if err != nil {
		return err
	}

	if len(body) > maxRequestBodySize {
		return errors.New("request body too large")
	}

	return json.Unmarshal(body, dst)
}

const maxRequestBodySize = 1 << 20 // 1 MiB

const yuridInvoiceIdempotencyMetaID = "yurid-idempotency"

type wrappedInvoice struct {
	Id   string `json:"id"`
	Paid bool   `json:"paid"`
	Fiat any    `json:"fiat"`
	yuri.Invoice
}

func (a *API) wrapInvoice(id string, inv *yuri.Invoice) wrappedInvoice {
	cloned := inv.Clone()
	fiat := cloned.Metadata[yuridInvoiceFiatMetaID]
	delete(cloned.Metadata, yuridInvoiceUUIDMetaId)
	delete(cloned.Metadata, yuridInvoiceFiatMetaID)
	delete(cloned.Metadata, yuridInvoiceExpireyMetaID)

	return wrappedInvoice{
		Id:      id,
		Paid:    inv.Paid(),
		Fiat:    fiat,
		Invoice: cloned,
	}
}

// invoiceID extracts the yurid invoice UUID from the invoice metadata.
func invoiceID(inv *yuri.Invoice) (string, error) {
	raw, ok := inv.Metadata[yuridInvoiceUUIDMetaId]
	if !ok {
		return "", errors.New("invoice metadata is missing the yurid invoice UUID")
	}

	switch v := raw.(type) {
	case uuid.UUID:
		return v.String(), nil
	case string:
		id, err := uuid.Parse(v)
		if err != nil {
			return "", fmt.Errorf("invalid yurid invoice UUID %q: %w", v, err)
		}
		return id.String(), nil
	default:
		return "", fmt.Errorf("unexpected type %T for yurid invoice UUID", raw)
	}
}

func (a *API) handleSample(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	writeJSON(w, http.StatusOK, yuri.InvoiceCreate{
		Chain:      yuri.Ethereum,
		Token:      yuri.EthereumUSDT,
		AmountFiat: yuri.USD.Of(5),
		Metadata: map[string]any{
			"some-cool": "metadata",
		},
	})
}

func (a *API) handleActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	chainStr := r.URL.Query().Get("chain")
	if chainStr == "" {
		writeError(w, http.StatusBadRequest, "missing chain query param", nil)
		return
	}

	chain, ok := a.canonicalChain(chainStr)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("chain %s is not registered", chainStr), nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	activeInvoices, err := a.storage.GetActiveInvoices(ctx, chain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch invoices", err)
		return
	}

	wrapped := make(map[string]wrappedInvoice, len(activeInvoices))
	for _, inv := range activeInvoices {
		id, err := invoiceID(&inv)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get invoice ID from metadata", err)
			return
		}

		wrapped[id] = a.wrapInvoice(id, &inv)
	}

	writeJSON(w, http.StatusOK, wrapped)
}

type WrappedInvoiceCreate struct {
	yuri.InvoiceCreate
	// Id is an optional client-supplied idempotency key. A repeated /new
	// with the same Id returns the existing invoice.
	Id string `json:"id"`
	// ExpiresAt is the expiry time in unix milliseconds. If no time is provided
	// it will default to 30m.
	ExpiresAt int64 `json:"expires_at"`
}

// respondWithInvoice writes the wrapped form of a stored invoice.
func (a *API) respondWithInvoice(w http.ResponseWriter, inv *yuri.Invoice) {
	id, err := invoiceID(inv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get invoice ID from metadata", err)
		return
	}

	writeJSON(w, http.StatusOK, a.wrapInvoice(id, inv))
}

func (a *API) handleNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	var req WrappedInvoiceCreate
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body", err)
		return
	}

	chain, ok := a.canonicalChain(string(req.Chain))
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("chain %s is not registered", req.Chain), nil)
		return
	}
	req.Chain = chain

	if req.AmountFiat.Minor <= 0 {
		writeError(w, http.StatusBadRequest, "amount cannot be less than or equal to zero", nil)
		return
	}

	if req.ExpiresAt != 0 && req.ExpiresAt <= time.Now().UnixMilli() {
		writeError(w, http.StatusBadRequest, "expires_at must be in the future (unix milliseconds)", nil)
		return
	}

	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}

	for key := range req.Metadata {
		if strings.HasPrefix(key, "yurid-") {
			delete(req.Metadata, key)
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if req.Id != "" {
		activeInvoices, err := a.storage.GetActiveInvoices(ctx, req.Chain)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to fetch invoices", err)
			return
		}

		for i := range activeInvoices {
			if seenId, ok := activeInvoices[i].Metadata[yuridInvoiceIdempotencyMetaID]; ok && seenId == req.Id {
				a.respondWithInvoice(w, &activeInvoices[i])
				return
			}
		}

		req.Metadata[yuridInvoiceIdempotencyMetaID] = req.Id
	}

	req.Metadata[yuridInvoiceFiatMetaID] = req.AmountFiat
	if req.ExpiresAt != 0 {
		req.Metadata[yuridInvoiceExpireyMetaID] = time.UnixMilli(req.ExpiresAt)
	}

	inv, err := a.instance.NewInvoice(ctx, req.InvoiceCreate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create invoice", err)
		return
	}

	a.respondWithInvoice(w, &inv)
}

func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing ID", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	inv, err := a.storage.GetInvoiceByID(ctx, id)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			code = http.StatusNotFound
		}

		writeError(w, code, "failed to fetch invoice by id", err)
		return
	}

	writeJSON(w, http.StatusOK, a.wrapInvoice(id, inv))
}
