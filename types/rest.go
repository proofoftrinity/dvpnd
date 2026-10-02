// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package types

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewError(code int, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

type Response struct {
	Success bool        `json:"success"`
	Error   interface{} `json:"error,omitempty"`
	Result  interface{} `json:"result,omitempty"`
}

func NewResponse(err interface{}, res interface{}) *Response {
	return &Response{
		Success: err == nil,
		Error:   err,
		Result:  res,
	}
}

func NewResponseError(code int, v interface{}) *Response {
	message := "unknown error"
	if m, ok := v.(string); ok {
		message = m
	} else if m, ok := v.(error); ok {
		message = m.Error()
	}

	err := NewError(code, message)
	return NewResponse(err, nil)
}

func NewResponseResult(v interface{}) *Response {
	return NewResponse(nil, v)
}

// InternalErrorMessage is what a client is told when the node itself failed
// (a 5xx). The detail (an RPC endpoint's error, the proxy's API) goes to the
// node's log: it is the operator's business, not the client's.
const InternalErrorMessage = "the node could not complete the request; try again later"

// CodeInternal is the error code of a request that failed inside the node
// for no reason the client gave.
const CodeInternal = 13
