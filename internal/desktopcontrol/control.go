package desktopcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	maxMessageBytes       = 1 << 20
	controlRequestTimeout = 30 * time.Second
)

// Request 是桌面端与后台核心之间的本地控制请求。
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type responseError struct {
	Message string `json:"message"`
}

type response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *responseError  `json:"error,omitempty"`
}

// Handler 只处理已验证的本地请求。传输层负责限制消息大小与本机权限。
type Handler func(context.Context, Request) (any, error)

func Serve(ctx context.Context, runtimeRoot string, handler Handler) error {
	if runtimeRoot == "" {
		return errors.New("desktop control runtime root is required")
	}
	if handler == nil {
		return errors.New("desktop control handler is required")
	}
	return servePlatform(ctx, runtimeRoot, func(requestCtx context.Context, requestData []byte) []byte {
		return handleMessage(requestCtx, requestData, handler)
	})
}

func Call(ctx context.Context, runtimeRoot, method string, params, result any) error {
	if runtimeRoot == "" {
		return errors.New("desktop control runtime root is required")
	}
	if method == "" {
		return errors.New("desktop control method is required")
	}
	request := Request{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Method: method}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("encode desktop control params: %w", err)
		}
		request.Params = data
	}
	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode desktop control request: %w", err)
	}
	responseData, err := callPlatform(ctx, runtimeRoot, data)
	if err != nil {
		return err
	}
	var reply response
	if err := json.Unmarshal(responseData, &reply); err != nil {
		return fmt.Errorf("decode desktop control response: %w", err)
	}
	if reply.ID != request.ID {
		return errors.New("desktop control response id mismatch")
	}
	if reply.Error != nil {
		return errors.New(reply.Error.Message)
	}
	if result == nil || len(reply.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(reply.Result, result); err != nil {
		return fmt.Errorf("decode desktop control result: %w", err)
	}
	return nil
}

func handleMessage(ctx context.Context, data []byte, handler Handler) []byte {
	var request Request
	if len(data) > maxMessageBytes {
		return encodeResponse(response{Error: &responseError{Message: "本地控制请求过大"}})
	}
	if err := json.Unmarshal(data, &request); err != nil {
		return encodeResponse(response{Error: &responseError{Message: "本地控制请求格式无效"}})
	}
	if request.ID == "" || request.Method == "" {
		return encodeResponse(response{ID: request.ID, Error: &responseError{Message: "本地控制请求缺少 id 或 method"}})
	}
	value, err := handler(ctx, request)
	if err != nil {
		return encodeResponse(response{ID: request.ID, Error: &responseError{Message: err.Error()}})
	}
	var result json.RawMessage
	if value != nil {
		encoded, encodeErr := json.Marshal(value)
		if encodeErr != nil {
			return encodeResponse(response{ID: request.ID, Error: &responseError{Message: "本地控制响应编码失败"}})
		}
		result = encoded
	}
	return encodeResponse(response{ID: request.ID, Result: result})
}

func encodeResponse(reply response) []byte {
	data, err := json.Marshal(reply)
	if err != nil {
		return []byte(`{"error":{"message":"本地控制响应编码失败"}}`)
	}
	return data
}
