package yurid

import (
	"bytes"
	"codeberg.org/lewdest/yuri"
	_ "database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"io"
	"log"
	_ "modernc.org/sqlite"
	"net/http/httptest"
	"reflect"
	"testing"
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
