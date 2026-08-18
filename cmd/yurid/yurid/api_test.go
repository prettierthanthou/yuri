package yurid

import (
	"bytes"
	"codeberg.org/lewdest/yuri"
	"context"
	_ "database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"io"
	"log"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

const yuridTestAddr = ":6761"
const yuridTestUrl = "http://localhost:6761"

func jsonCompare(t *testing.T, a, b []byte) bool {
	t.Helper()

	var got, want any

	if err := json.Unmarshal(a, &got); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v\nwant=%#v", got, want)
		return false
	}

	return true
}

func TestSample(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
	req := httptest.NewRequest("GET", yuridTestUrl+"/sample", nil)
	w := httptest.NewRecorder()

	api.handleSample(w, req)

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("readall = %+v", err)
	}

	const expectedSampleResponse = `{"chain":"ethereum","token":{"symbol":"USDT","contract":"0xdAC17F958D2ee523a2206206994597C13D831ec7","decimals":6},"amount_fiat":{"currency":{"code":"USD","decimals":2},"minor":500},"metadata":{"some-cool":"metadata"}}`
	jsonCompare(t, body, []byte(expectedSampleResponse))
}

func TestGet_RequiresGetMethod(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	log.SetOutput(io.Discard)
	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
	req := httptest.NewRequest("POST", yuridTestUrl+"/get", nil)
	w := httptest.NewRecorder()

	api.handleGet(w, req)
	if w.Result().StatusCode != 405 {
		t.Fatalf("StatusCode = %d expected = 405", w.Result().StatusCode)
	}
}

func TestGet_MissingId(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	log.SetOutput(io.Discard)
	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
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
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("failed to create v7 uuid for testing: %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
	req := httptest.NewRequest("GET", fmt.Sprintf(yuridTestUrl+"/get?id=%s", id.String()), nil)
	w := httptest.NewRecorder()

	api.handleGet(w, req)
	if w.Result().StatusCode != 404 {
		t.Fatalf("StatusCode = %d expected = 404", w.Result().StatusCode)
	}

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("readall = %+v", err)
	}

	const expectedBody = `{"detail":"sql: no rows in result set","error":"failed to fetch invoice by id"}`
	jsonCompare(t, body, []byte(expectedBody))
}

func TestNew_InvalidMethod(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
	req := httptest.NewRequest("GET", yuridTestUrl+"/new", nil)
	w := httptest.NewRecorder()

	api.handleNew(w, req)
	if w.Result().StatusCode != 405 {
		t.Fatalf("StatusCode = %d expected = 405", w.Result().StatusCode)
	}
}

func TestNew_InvalidJsonBody(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
	body, _ := json.Marshal(map[string]any{"blahhh": "hahaha"})
	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader(body))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	if resp.StatusCode != 400 {
		t.Fatalf("StatusCode = %d expected = 400", w.Result().StatusCode)
	}
}

func TestNew_InvalidBody(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader([]byte("waqfvwae")))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	if resp.StatusCode != 400 {
		t.Fatalf("StatusCode = %d expected = 400", w.Result().StatusCode)
	}
}

func TestNew_InvalidFiatAmount(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
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
		t.Fatalf("StatusCode = %d expected = 400", w.Result().StatusCode)
	}
}

func TestNew_InvalidFiatAmountNegative(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
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
		t.Fatalf("StatusCode = %d expected = 400", w.Result().StatusCode)
	}
}

func TestNew_NoMetadataRegression(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
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
	fmt.Printf("body = %s", b)
	if resp.StatusCode != 500 {
		// 500 is expected as this should fail to create the addr
		t.Fatalf("StatusCode = %d expected = 500", w.Result().StatusCode)
	}

	const expectedBody = `{"detail":"failed to create address for invoice: err = Post \"\": unsupported protocol scheme \"\" invoice = {Chain:ethereum Token:{Symbol: Contract: Decimals:0} AmountFiat:{Currency:{Code:EUR Decimals:2} Minor:500} Metadata:map[yurid-fiat-hist:{Currency:{Code:EUR Decimals:2} Minor:500}]}","error":"failed to create invoice"}`
	jsonCompare(t, b, []byte(expectedBody))
}

func TestAuth_RejectsMissingToken(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "sekrit")
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
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "sekrit")
	req := httptest.NewRequest("GET", yuridTestUrl+"/sample", nil)
	req.Header.Set("Authorization", "Bearer sekrit")
	w := httptest.NewRecorder()

	api.Handler().ServeHTTP(w, req)
	if w.Result().StatusCode != 200 {
		t.Fatalf("StatusCode = %d expected = 200", w.Result().StatusCode)
	}
}

func TestNew_RejectsPastExpiresAt(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{yuri.NewEthereum(yuri.JsonRpcClientConfig{})},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	api := NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
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

// fakeChain always succeeds at creating addresses, unlike real providers.
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

	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("database = %+v", err)
	}

	instance, err := yuri.New(yuri.Options{
		Pricing:         []yuri.PriceProvider{yuri.NewStaticPriceProvider(1)},
		Chains:          []yuri.CryptoProvider{fakeChain{}},
		PriceAggregator: yuri.MedianPriceAggregator{},
		Storage:         db,
	})
	if err != nil {
		t.Fatalf("instance = %+v", err)
	}

	return NewAPI(yuridTestAddr, db, instance, []string{string(yuri.Ethereum)}, "")
}

func TestNew_NullMetadataSucceeds(t *testing.T) {
	api := newTestAPI(t)

	body := []byte(`{"chain":"ethereum","amount_fiat":{"currency":{"code":"EUR","decimals":2},"minor":500},"metadata":null}`)
	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader(body))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	b, _ := io.ReadAll(resp.Body)
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

func getActive(t *testing.T, api *API, chain string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest("GET", yuridTestUrl+"/active?chain="+chain, nil)
	w := httptest.NewRecorder()

	api.handleActive(w, req)
	return w
}

func TestActive_CanonicalizesChainCasing(t *testing.T) {
	api := newTestAPI(t)

	body := []byte(`{"chain":"ethereum","amount_fiat":{"currency":{"code":"EUR","decimals":2},"minor":500}}`)
	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader(body))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	b, _ := io.ReadAll(resp.Body)
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
	req := httptest.NewRequest("POST", yuridTestUrl+"/new", bytes.NewReader(body))
	w := httptest.NewRecorder()

	api.handleNew(w, req)

	resp := w.Result()
	b, _ := io.ReadAll(resp.Body)
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
