package yuri

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
)

type JsonRpcClientConfig struct {
	Host     string
	Username string
	Password string

	NonB64BasicAuth bool

	// override the default http client
	Client *http.Client
}

func NewJsonRpcClient(conf JsonRpcClientConfig) JsonRpcClient {
	client := http.DefaultClient
	if conf.Client != nil {
		client = conf.Client
	}

	return JsonRpcClient{conf: conf, httpClient: client}
}

// JsonRpcClient is a small JSON-RPC 2.0 client
// this client does not support batching.
//
// Impls https://www.simple-is-better.org/json-rpc/transport_http.html
type JsonRpcClient struct {
	conf       JsonRpcClientConfig
	httpClient *http.Client
}

const jsonRpcVersion = "2.0"

type JsonRpcRequest struct {
	// Must be exactly 2.0
	JsonRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	// NOTE: previously we only supported maps/actual objects,
	// but uhhh.. nope! fuck you. Bitcoin forced my hand
	// because they dont accept named args.
	Params any `json:"params,omitempty"`
	// if ID is omitted this request is a Notification
	Id string `json:"id,omitempty"`
}

type JsonRpcResponse struct {
	// Must be exactly 2.0
	JsonRPC string `json:"jsonrpc"`

	// mutually exclusive with Error
	Result json.RawMessage `json:"result"`
	// mutually exclusive with Result
	Error jsonRpcError `json:"error"`

	// can be nullable if there was no id in
	// the request object
	Id string `json:"id,omitempty"`
}

const (
	JsonRPCParseError          = -32700
	JsonRPCInvalidRequestError = -32600
	JsonRPCMethodNotFoundError = -32601
	JsonRPCInvalidParamsError  = -32602
	JsonRPCInternalErrorError  = -32603
	// [-32000 to -32099] Server Error
)

type jsonRpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	// Data can be nullable/empty
	Data map[string]any `json:"data,omitempty"`
}

func RPCDo(
	ctx context.Context,
	client JsonRpcClient,
	req JsonRpcRequest,
	out any,
) error {
	resp, err := client.Do(ctx, req)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(bytes.NewReader(resp.Result))
	dec.UseNumber()

	return dec.Decode(out)
}

// You generally want to use RPCDo instead for an easier life.
func (c JsonRpcClient) Do(ctx context.Context, request JsonRpcRequest) (JsonRpcResponse, error) {
	rid := request.Id
	if rid == "" {
		rid = strconv.FormatInt(int64(rand.Int()), 10)
	}

	body, err := json.Marshal(JsonRpcRequest{
		JsonRPC: jsonRpcVersion,
		Method:  request.Method,
		Params:  request.Params,
		Id:      rid,
	})
	if err != nil {
		return JsonRpcResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.conf.Host, bytes.NewBuffer(body))
	if err != nil {
		return JsonRpcResponse{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.conf.Username != "" && c.conf.Password != "" {
		// NOTE: we do not use SetBasicAuth here as it base64 encodes
		// the username:password which... is RETARDED.
		// this took 10 minutes to determine why monero_test.go was failing to communicate
		// to RPC when auth was enabled.

		if c.conf.NonB64BasicAuth {
			req.Header.Set("Authorization", fmt.Sprintf("%s:%s", c.conf.Username, c.conf.Password))
		} else {
			req.SetBasicAuth(c.conf.Username, c.conf.Password)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return JsonRpcResponse{}, err
	}

	defer resp.Body.Close()
	readBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return JsonRpcResponse{}, err
	}

	parsedResponse := JsonRpcResponse{}
	if err := json.Unmarshal(readBody, &parsedResponse); err != nil {
		return JsonRpcResponse{}, err
	}

	if parsedResponse.Id != rid {
		return JsonRpcResponse{}, fmt.Errorf("recieved jsonrpc response but id didn't match expected %s but recieved %s", rid, parsedResponse.Id)
	}

	if parsedResponse.Error.Code != 0 {
		return JsonRpcResponse{}, fmt.Errorf("jsonrpc request landed but failed: %+v", parsedResponse.Error)
	}

	return parsedResponse, nil
}
