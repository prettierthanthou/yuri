package yurid

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"codeberg.org/lewdest/yuri"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const yuridTestUrl = "http://localhost:6761"

func jsonCompare(t *testing.T, got, want []byte) {
	t.Helper()

	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("got=%#v\nwant=%#v", g, w)
	}
}

type fakeChain struct{}

func (fakeChain) Chain() yuri.Chain { return yuri.Ethereum }

func (fakeChain) CreateAddress(context.Context) (string, error) {
	return uuid.NewString(), nil
}

func (fakeChain) Decimals() int64 { return 8 }

func (fakeChain) Poll(context.Context, []yuri.Invoice) ([]yuri.Invoice, error) {
	return nil, nil
}

func (fakeChain) SupportsNFTs() bool { return false }

func newTestAPI(t *testing.T) *API {
	t.Helper()
	return newTestAPIWith(t, fakeChain{}, "")
}

func newTestAPIWith(t *testing.T, chain yuri.CryptoProvider, token string) *API {
	t.Helper()

	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{chain},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	events := NewEventServer()
	t.Cleanup(events.Close)

	return NewAPI(db, instance, []string{string(yuri.Ethereum)}, token, events)
}

func TestSample(t *testing.T) {
	api := newTestAPI(t)

	req := httptest.NewRequest("GET", yuridTestUrl+"/sample", nil)
	w := httptest.NewRecorder()

	api.handleSample(w, req)

	body, err := io.ReadAll(w.Result().Body)
	if err != nil {
		t.Fatalf("readall = %+v", err)
	}

	const expectedSampleResponse = `{"chain":"ethereum","token":{"symbol":"USDT","contract":"0xdAC17F958D2ee523a2206206994597C13D831ec7","decimals":6},"amount_fiat":{"currency":{"code":"USD","decimals":2},"minor":500},"metadata":{"some-cool":"metadata"}}`
	jsonCompare(t, body, []byte(expectedSampleResponse))
}

func TestGet_RequiresGetMethod(t *testing.T) {
	api := newTestAPI(t)

	req := httptest.NewRequest("POST", yuridTestUrl+"/get", nil)
	w := httptest.NewRecorder()

	api.handleGet(w, req)
	if w.Result().StatusCode != 405 {
		t.Fatalf("StatusCode = %d expected = 405", w.Result().StatusCode)
	}
}

func TestGet_MissingId(t *testing.T) {
	api := newTestAPI(t)

	req := httptest.NewRequest("GET", yuridTestUrl+"/get", nil)
	w := httptest.NewRecorder()

	api.handleGet(w, req)
	resp := w.Result()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("readall = %+v", err)
	}

	if resp.StatusCode != 400 {
		t.Fatalf("StatusCode = %d expected = 400", resp.StatusCode)
	}

	const expectedBody = `{"error":"missing ID"}`
	jsonCompare(t, body, []byte(expectedBody))
}

func TestGet_NoExistingRecord(t *testing.T) {
	api := newTestAPI(t)

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("failed to create v7 uuid for testing: %+v", err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf(yuridTestUrl+"/get?id=%s", id.String()), nil)
	w := httptest.NewRecorder()

	api.handleGet(w, req)
	if w.Result().StatusCode != 404 {
		t.Fatalf("StatusCode = %d expected = 404", w.Result().StatusCode)
	}

	body, err := io.ReadAll(w.Result().Body)
	if err != nil {
		t.Fatalf("readall = %+v", err)
	}

	const expectedBody = `{"detail":"sql: no rows in result set","error":"failed to fetch invoice by id"}`
	jsonCompare(t, body, []byte(expectedBody))
}

func TestNew_InvalidMethod(t *testing.T) {
	api := newTestAPI(t)

	req := httptest.NewRequest("GET", yuridTestUrl+"/new", nil)
	w := httptest.NewRecorder()

	api.handleNew(w, req)
	if w.Result().StatusCode != 405 {
		t.Fatalf("StatusCode = %d expected = 405", w.Result().StatusCode)
	}
}

func TestNew_InvalidJsonBody(t *testing.T) {
	api := newTestAPI(t)

	body, _ := json.Marshal(map[string]any{"blahhh": "hahaha"})
	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader(body))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	if resp.StatusCode != 400 {
		t.Fatalf("StatusCode = %d expected = 400", resp.StatusCode)
	}
}

func TestNew_InvalidBody(t *testing.T) {
	api := newTestAPI(t)

	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader([]byte("waqfvwae")))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	if resp.StatusCode != 400 {
		t.Fatalf("StatusCode = %d expected = 400", resp.StatusCode)
	}
}

func TestNew_InvalidFiatAmount(t *testing.T) {
	api := newTestAPI(t)

	body, _ := json.Marshal(WrappedInvoiceCreate{
		InvoiceCreate: yuri.InvoiceCreate{
			Chain:      yuri.Ethereum,
			Token:      yuri.Token{},
			AmountFiat: yuri.EUR.Of(0),
			Metadata:   map[string]any{},
		},
	})

	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader(body))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	if resp.StatusCode != 400 {
		t.Fatalf("StatusCode = %d expected = 400", resp.StatusCode)
	}
}

func TestNew_InvalidFiatAmountNegative(t *testing.T) {
	api := newTestAPI(t)

	body, _ := json.Marshal(WrappedInvoiceCreate{
		InvoiceCreate: yuri.InvoiceCreate{
			Chain:      yuri.Ethereum,
			Token:      yuri.Token{},
			AmountFiat: yuri.EUR.Of(-1),
			Metadata:   map[string]any{},
		},
	})

	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader(body))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	if resp.StatusCode != 400 {
		t.Fatalf("StatusCode = %d expected = 400", resp.StatusCode)
	}
}

func TestNew_NoMetadataRegression(t *testing.T) {
	api := newTestAPIWith(t, yuri.NewEthereum(yuri.JsonRpcClientConfig{}), "")

	body, _ := json.Marshal(WrappedInvoiceCreate{
		InvoiceCreate: yuri.InvoiceCreate{
			Chain:      yuri.Ethereum,
			Token:      yuri.Token{},
			AmountFiat: yuri.EUR.Of(5),
		},
	})

	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader(body))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 500 {
		// 500 is expected as this should fail to create the addr
		t.Fatalf("StatusCode = %d expected = 500", resp.StatusCode)
	}

	const expectedBody = `{"detail":"failed to create address for invoice: err = Post \"\": unsupported protocol scheme \"\" invoice = {Chain:ethereum Token:{Symbol: Contract: Decimals:0} AmountFiat:{Currency:{Code:EUR Decimals:2} Minor:500} Metadata:map[yurid-fiat-hist:{Currency:{Code:EUR Decimals:2} Minor:500}]}","error":"failed to create invoice"}`
	jsonCompare(t, b, []byte(expectedBody))
}

func TestAuth_RejectsMissingToken(t *testing.T) {
	api := newTestAPIWith(t, fakeChain{}, "sekrit")

	req := httptest.NewRequest("GET", yuridTestUrl+"/sample", nil)
	w := httptest.NewRecorder()

	api.Handler().ServeHTTP(w, req)
	if w.Result().StatusCode != 401 {
		t.Fatalf("StatusCode = %d expected = 401", w.Result().StatusCode)
	}

	req = httptest.NewRequest("GET", yuridTestUrl+"/sample", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w = httptest.NewRecorder()

	api.Handler().ServeHTTP(w, req)
	if w.Result().StatusCode != 401 {
		t.Fatalf("StatusCode = %d expected = 401", w.Result().StatusCode)
	}
}

func TestAuth_AcceptsToken(t *testing.T) {
	api := newTestAPIWith(t, fakeChain{}, "sekrit")

	req := httptest.NewRequest("GET", yuridTestUrl+"/sample", nil)
	req.Header.Set("Authorization", "Bearer sekrit")
	w := httptest.NewRecorder()

	api.Handler().ServeHTTP(w, req)
	if w.Result().StatusCode != 200 {
		t.Fatalf("StatusCode = %d expected = 200", w.Result().StatusCode)
	}
}

func TestNew_RejectsPastExpiresAt(t *testing.T) {
	api := newTestAPI(t)

	body, _ := json.Marshal(WrappedInvoiceCreate{
		InvoiceCreate: yuri.InvoiceCreate{
			Chain:      yuri.Ethereum,
			Token:      yuri.Token{},
			AmountFiat: yuri.EUR.Of(5),
			Metadata:   map[string]any{},
		},
		ExpiresAt: time.Now().Add(-1 * time.Hour).UnixMilli(),
	})

	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader(body))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 400 {
		t.Fatalf("StatusCode = %d expected = 400 (body = %s)", resp.StatusCode, b)
	}

	const expectedBody = `{"error":"expires_at must be in the future (unix milliseconds)"}`
	jsonCompare(t, b, []byte(expectedBody))
}

func postNew(t *testing.T, api *API, body []byte) (*http.Response, []byte) {
	t.Helper()

	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader(body))
	req.RemoteAddr = "192.0.2.4:1234"
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func getActive(t *testing.T, api *API, chain string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest("GET", yuridTestUrl+"/active?chain="+chain, nil)
	w := httptest.NewRecorder()

	api.handleActive(w, req)
	return w
}

func TestNew_NullMetadataSucceeds(t *testing.T) {
	api := newTestAPI(t)

	body := []byte(`{"chain":"ethereum","amount_fiat":{"currency":{"code":"EUR","decimals":2},"minor":500},"metadata":null}`)

	resp, b := postNew(t, api, body)
	if resp.StatusCode != 200 {
		t.Fatalf("StatusCode = %d expected = 200 (body = %s)", resp.StatusCode, b)
	}

	var created struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(b, &created); err != nil {
		t.Fatal(err)
	}

	active := getActive(t, api, string(yuri.Ethereum))
	activeBody, _ := io.ReadAll(active.Result().Body)
	if active.Result().StatusCode != 200 {
		t.Fatalf("active StatusCode = %d expected = 200 (body = %s)", active.Result().StatusCode, activeBody)
	}

	var activeInvoices map[string]json.RawMessage
	if err := json.Unmarshal(activeBody, &activeInvoices); err != nil {
		t.Fatal(err)
	}
	if _, ok := activeInvoices[created.Id]; !ok {
		t.Fatalf("invoice %s was not found in active invoices", created.Id)
	}
}

func TestActive_CanonicalizesChainCasing(t *testing.T) {
	api := newTestAPI(t)

	body := []byte(`{"chain":"ethereum","amount_fiat":{"currency":{"code":"EUR","decimals":2},"minor":500}}`)

	resp, b := postNew(t, api, body)
	if resp.StatusCode != 200 {
		t.Fatalf("StatusCode = %d expected = 200 (body = %s)", resp.StatusCode, b)
	}

	var created struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(b, &created); err != nil {
		t.Fatal(err)
	}

	active := getActive(t, api, "Ethereum")
	activeBody, _ := io.ReadAll(active.Result().Body)
	if active.Result().StatusCode != 200 {
		t.Fatalf("active StatusCode = %d expected = 200 (body = %s)", active.Result().StatusCode, activeBody)
	}

	var activeInvoices map[string]json.RawMessage
	if err := json.Unmarshal(activeBody, &activeInvoices); err != nil {
		t.Fatal(err)
	}
	if _, ok := activeInvoices[created.Id]; !ok {
		t.Fatalf("invoice %s was not found in active invoices for chain Ethereum", created.Id)
	}
}

func TestNew_IdempotencyKey(t *testing.T) {
	api := newTestAPI(t)

	body := []byte(`{"chain":"ethereum","amount_fiat":{"currency":{"code":"EUR","decimals":2},"minor":500},"id":"order-1234"}`)

	resp1, b1 := postNew(t, api, body)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first StatusCode = %d expected = 200 (body = %s)", resp1.StatusCode, b1)
	}

	var first struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(b1, &first); err != nil {
		t.Fatal(err)
	}

	resp2, b2 := postNew(t, api, body)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second StatusCode = %d expected = 200 (body = %s)", resp2.StatusCode, b2)
	}

	var second struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(b2, &second); err != nil {
		t.Fatal(err)
	}

	if second.Id != first.Id {
		t.Fatalf("second Id = %q expected = %q (same invoice)", second.Id, first.Id)
	}

	active := getActive(t, api, string(yuri.Ethereum))
	activeBody, _ := io.ReadAll(active.Result().Body)
	if active.Result().StatusCode != 200 {
		t.Fatalf("active StatusCode = %d expected = 200 (body = %s)", active.Result().StatusCode, activeBody)
	}

	var activeInvoices map[string]json.RawMessage
	if err := json.Unmarshal(activeBody, &activeInvoices); err != nil {
		t.Fatal(err)
	}
	if len(activeInvoices) != 1 {
		t.Fatalf("len(activeInvoices) = %d expected = 1", len(activeInvoices))
	}
	if _, ok := activeInvoices[first.Id]; !ok {
		t.Fatalf("invoice %s was not found in active invoices", first.Id)
	}
}

func TestNew_StripsReservedMetadataKeys(t *testing.T) {
	api := newTestAPI(t)

	body := []byte(`{"chain":"ethereum","amount_fiat":{"currency":{"code":"EUR","decimals":2},"minor":500},"metadata":{"yurid-foo":"bar","client-key":"keep"}}`)

	resp, b := postNew(t, api, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d expected = 200 (body = %s)", resp.StatusCode, b)
	}

	var created struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(b, &created); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inv, err := api.storage.GetInvoiceByID(ctx, created.Id)
	if err != nil {
		t.Fatalf("failed to fetch stored invoice: %+v", err)
	}

	if _, ok := inv.Metadata["yurid-foo"]; ok {
		t.Fatalf("stored invoice metadata contains reserved key yurid-foo: %#v", inv.Metadata)
	}
	if inv.Metadata["client-key"] != "keep" {
		t.Fatalf("stored invoice metadata missing non-reserved key: %#v", inv.Metadata)
	}
}

func TestNew_CanonicalizesChainCasing(t *testing.T) {
	api := newTestAPI(t)

	body := []byte(`{"chain":"Ethereum","amount_fiat":{"currency":{"code":"EUR","decimals":2},"minor":500}}`)

	resp, b := postNew(t, api, body)
	if resp.StatusCode != 200 {
		t.Fatalf("StatusCode = %d expected = 200 (body = %s)", resp.StatusCode, b)
	}

	var created struct {
		Id    string `json:"id"`
		Chain string `json:"chain"`
	}
	if err := json.Unmarshal(b, &created); err != nil {
		t.Fatal(err)
	}
	if created.Chain != string(yuri.Ethereum) {
		t.Fatalf("Chain = %q expected = %q", created.Chain, yuri.Ethereum)
	}

	active := getActive(t, api, string(yuri.Ethereum))
	activeBody, _ := io.ReadAll(active.Result().Body)
	if active.Result().StatusCode != 200 {
		t.Fatalf("active StatusCode = %d expected = 200 (body = %s)", active.Result().StatusCode, activeBody)
	}

	var activeInvoices map[string]json.RawMessage
	if err := json.Unmarshal(activeBody, &activeInvoices); err != nil {
		t.Fatal(err)
	}
	if _, ok := activeInvoices[created.Id]; !ok {
		t.Fatalf("invoice %s was not found in active invoices for chain %s", created.Id, yuri.Ethereum)
	}
}

func TestEvents_RequiresGetMethod(t *testing.T) {
	api := newTestAPI(t)

	req := httptest.NewRequest("POST", yuridTestUrl+"/events", nil)
	w := httptest.NewRecorder()

	api.Handler().ServeHTTP(w, req)
	if w.Result().StatusCode != 405 {
		t.Fatalf("StatusCode = %d expected = 405", w.Result().StatusCode)
	}
}

func TestEvents_UnknownStream(t *testing.T) {
	api := newTestAPI(t)

	req := httptest.NewRequest("GET", yuridTestUrl+"/events?stream=bogus", nil)
	w := httptest.NewRecorder()

	api.Handler().ServeHTTP(w, req)
	if w.Result().StatusCode != 404 {
		t.Fatalf("StatusCode = %d expected = 404", w.Result().StatusCode)
	}
}

func TestEvents_RequiresAuth(t *testing.T) {
	api := newTestAPIWith(t, fakeChain{}, "sekrit")

	req := httptest.NewRequest("GET", yuridTestUrl+"/events", nil)
	w := httptest.NewRecorder()

	api.Handler().ServeHTTP(w, req)
	if w.Result().StatusCode != 401 {
		t.Fatalf("StatusCode = %d expected = 401", w.Result().StatusCode)
	}
}

// sseFrame is a single parsed SSE event from the wire.
type sseFrame struct {
	id   string
	typ  string
	data string
}

// streamFrames parses SSE frames from a response body until it is closed.
func streamFrames(t *testing.T, resp *http.Response) <-chan sseFrame {
	t.Helper()

	frames := make(chan sseFrame, 16)
	go func() {
		defer close(frames)

		sc := bufio.NewScanner(resp.Body)
		var cur *sseFrame
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if cur != nil {
					frames <- *cur
					cur = nil
				}
				continue
			}

			if strings.HasPrefix(line, ":") {
				// comment (e.g. heartbeat ping), carries no event
				continue
			}

			if cur == nil {
				cur = &sseFrame{}
			}
			switch {
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				cur.typ = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	return frames
}

func nextFrame(t *testing.T, frames <-chan sseFrame) sseFrame {
	t.Helper()

	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("events stream closed early")
		}
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSE frame")
		return sseFrame{}
	}
}

func testInvoice(paid bool) *yuri.Invoice {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}

	amountPaid := big.NewInt(500)
	if paid {
		amountPaid = big.NewInt(1000)
	}

	return &yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0xabc",
		Token:      yuri.EthereumUSDT,
		AmountOwed: big.NewInt(1000),
		AmountPaid: amountPaid,
		Metadata: map[string]any{
			yuridInvoiceUUIDMetaId: id,
			yuridInvoiceFiatMetaID: yuri.USD.Of(5),
		},
	}
}

// newEventsClient connects an SSE client to a stream and waits for the
// connection to be established (headers flushed after subscribing).
func newEventsClient(t *testing.T, srvURL, stream string) (*http.Response, <-chan sseFrame) {
	t.Helper()

	url := srvURL + "/events"
	if stream != "" {
		url += "?stream=" + stream
	}

	// Timeout as a safety net: the stream is only read until the frames
	// the test expects have arrived.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("connect to events stream: %+v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d expected = 200", resp.StatusCode)
	}

	return resp, streamFrames(t, resp)
}

func TestEvents_StreamsInvoiceUpdates(t *testing.T) {
	api := newTestAPI(t)

	srv := httptest.NewServer(api.Handler())
	// Registered before the client-body cleanups so those run first:
	// Close() blocks until outstanding requests finish, and the SSE
	// connection stays open until the client closes it.
	t.Cleanup(srv.Close)

	_, frames := newEventsClient(t, srv.URL, "")

	inv := testInvoice(false)
	api.events.PublishInvoice(inv)

	got := nextFrame(t, frames)
	if got.typ != "invoice" || got.id != "0" {
		t.Fatalf("frame = %+v expected type=invoice id=0", got)
	}

	id := inv.Metadata[yuridInvoiceUUIDMetaId].(uuid.UUID).String()
	want, err := json.Marshal(wrapInvoice(id, inv))
	if err != nil {
		t.Fatal(err)
	}
	jsonCompare(t, []byte(got.data), want)
}

func TestEvents_PaidStreamFilters(t *testing.T) {
	api := newTestAPI(t)

	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)

	_, updates := newEventsClient(t, srv.URL, "updates")
	_, paid := newEventsClient(t, srv.URL, "paid")

	unpaid := testInvoice(false)
	paidInv := testInvoice(true)

	api.events.PublishInvoice(unpaid)
	api.events.PublishInvoice(paidInv)

	// the paid stream only sees the paid invoice
	got := nextFrame(t, paid)
	if got.typ != "invoice" || got.id != "0" {
		t.Fatalf("paid stream frame = %+v expected type=invoice id=0", got)
	}
	var data wrappedInvoice
	if err := json.Unmarshal([]byte(got.data), &data); err != nil {
		t.Fatal(err)
	}
	if data.Id != paidInv.Metadata[yuridInvoiceUUIDMetaId].(uuid.UUID).String() {
		t.Fatalf("paid stream invoice id = %s, expected the paid invoice", data.Id)
	}

	// the updates stream sees both, in order
	first := nextFrame(t, updates)
	var firstData wrappedInvoice
	if err := json.Unmarshal([]byte(first.data), &firstData); err != nil {
		t.Fatal(err)
	}
	if first.id != "0" || firstData.Id != unpaid.Metadata[yuridInvoiceUUIDMetaId].(uuid.UUID).String() {
		t.Fatalf("updates stream first frame = %+v expected the unpaid invoice", first)
	}

	second := nextFrame(t, updates)
	var secondData wrappedInvoice
	if err := json.Unmarshal([]byte(second.data), &secondData); err != nil {
		t.Fatal(err)
	}
	if second.id != "1" || secondData.Id != paidInv.Metadata[yuridInvoiceUUIDMetaId].(uuid.UUID).String() {
		t.Fatalf("updates stream second frame = %+v expected the paid invoice", second)
	}
}

func TestEvents_ReplaysFromLastEventID(t *testing.T) {
	api := newTestAPI(t)

	// publish history before any client connects
	inv1 := testInvoice(false)
	inv2 := testInvoice(true)
	inv3 := testInvoice(false)
	api.events.PublishInvoice(inv1)
	api.events.PublishInvoice(inv2)
	api.events.PublishInvoice(inv3)

	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", srv.URL+"/events?stream=updates", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "1")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("connect to events stream: %+v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	frames := streamFrames(t, resp)

	// events with id >= 1 are replayed
	first := nextFrame(t, frames)
	if first.id != "1" {
		t.Fatalf("first replayed frame id = %s expected = 1", first.id)
	}
	var firstData wrappedInvoice
	if err := json.Unmarshal([]byte(first.data), &firstData); err != nil {
		t.Fatal(err)
	}
	if firstData.Id != inv2.Metadata[yuridInvoiceUUIDMetaId].(uuid.UUID).String() {
		t.Fatalf("first replayed frame = %+v expected invoice 2", first)
	}

	second := nextFrame(t, frames)
	if second.id != "2" {
		t.Fatalf("second replayed frame id = %s expected = 2", second.id)
	}
	var secondData wrappedInvoice
	if err := json.Unmarshal([]byte(second.data), &secondData); err != nil {
		t.Fatal(err)
	}
	if secondData.Id != inv3.Metadata[yuridInvoiceUUIDMetaId].(uuid.UUID).String() {
		t.Fatalf("second replayed frame = %+v expected invoice 3", second)
	}
}
