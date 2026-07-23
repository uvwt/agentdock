package jsonrpc

import "encoding/json"

const Version = "2.0"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string
	ID      any
	Result  any
	Error   *Error
}

func (r Response) MarshalJSON() ([]byte, error) {
	if r.Error != nil {
		return json.Marshal(struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Error   *Error `json:"error"`
		}{JSONRPC: r.JSONRPC, ID: r.ID, Error: r.Error})
	}
	return json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Result  any    `json:"result"`
	}{JSONRPC: r.JSONRPC, ID: r.ID, Result: r.Result})
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func Success(id any, result any) Response {
	return Response{JSONRPC: Version, ID: id, Result: result}
}

func Failure(id any, code int, message string, data any) Response {
	return Response{JSONRPC: Version, ID: id, Error: &Error{Code: code, Message: message, Data: data}}
}
