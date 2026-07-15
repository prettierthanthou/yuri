package yurid

import (
	"context"
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
	addr             string
	storage          *Database
	instance         *yuri.Instance
	mux              *http.ServeMux
	activeChainNames []string
}

func NewAPI(addr string, database *Database, instance *yuri.Instance, activeChainNames []string) *API {
	api := &API{
		addr:             addr,
		storage:          database,
		instance:         instance,
		mux:              http.NewServeMux(),
		activeChainNames: activeChainNames,
	}

	api.routes()
	return api
}

func (a *API) ListenAndServe() error {
	srv := &http.Server{
		Addr:              a.addr,
		Handler:           a.mux,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return srv.ListenAndServe()
}

func (a *API) routes() {
	a.mux.HandleFunc("/sample", a.handleSample)
	a.mux.HandleFunc("/active", a.handleActive)
	a.mux.HandleFunc("/get", a.handleGet)
	a.mux.HandleFunc("/new", a.handleNew)
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

	slog.Error("occuried during a request", "err", resp)
	writeJSON(w, status, resp)
}

func decodeJSON(r *http.Request, dst any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

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
	wrapped := wrappedInvoice{
		Id:      id,
		Paid:    inv.Paid(),
		Fiat:    fiat,
		Invoice: cloned,
	}

	return wrapped
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

	foundChain := false
	for _, activeChain := range a.activeChainNames {
		if strings.EqualFold(activeChain, chainStr) {
			foundChain = true
			break
		}
	}

	if !foundChain {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("chain %s is not registered", chainStr), nil)
		return
	}

	chain := yuri.Chain(chainStr)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	activeInvoices, err := a.storage.GetActiveInvoices(ctx, chain)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, []yuri.Invoice{})
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch invoices", err)
		return
	}

	wrapped := make(map[string]wrappedInvoice, len(activeInvoices))
	for _, inv := range activeInvoices {
		rawId, ok := inv.Metadata[yuridInvoiceUUIDMetaId]
		if !ok {
			writeError(w, http.StatusInternalServerError, "failed to get invoice ID from metadata", nil)
			return
		}

		id, err := uuid.Parse(rawId.(string))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to cast internal invoice uuid from metadata to UUID", err)
			return
		}

		wrapped[id.String()] = a.wrapInvoice(id.String(), &inv)
	}

	writeJSON(w, http.StatusOK, wrapped)
}

type WrappedInvoiceCreate struct {
	yuri.InvoiceCreate
	ExpiresAt int64
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

	if req.AmountFiat.Minor == 0 {
		writeError(w, http.StatusBadRequest, "amount cannot be zero", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}

	req.Metadata[yuridInvoiceFiatMetaID] = req.AmountFiat
	if req.ExpiresAt != 0 {
		req.InvoiceCreate.Metadata[yuridInvoiceExpireyMetaID] = time.UnixMilli(req.ExpiresAt)
	}
	inv, err := a.instance.NewInvoice(ctx, req.InvoiceCreate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create invoice", err)
		return
	}

	rawId, ok := inv.Metadata[yuridInvoiceUUIDMetaId]
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to get invoice ID from metadata", nil)
		return
	}

	id, ok := rawId.(uuid.UUID)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to cast internal invoice uuid from metadata to UUID", nil)
		return
	}

	writeJSON(w, http.StatusOK, a.wrapInvoice(id.String(), &inv))
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

	inv, err := a.storage.GetInvoiceByID(r.Context(), id)
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
