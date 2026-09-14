package mizu

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// An Error captures three key pieces of information: an HTTP status
// code, an underlying Go error, and an optional collection of
// arbitrary JSON values called "details". Servers send the code, the
// underlying error's Error() output, and the details over the wire to
// clients. Remember that the underlying error's message will be sent
// to clients — take care not to leak sensitive information from
// public APIs!
//
// Handlers and middlewares should return errors that can be cast to
// an [*Error] (using the standard library's [errors.As]). If the
// returned error can't be cast to an [*Error], [ResponseError] will
// use [http.StatusInternalServerError] and the returned error's
// message.
type Error struct {
	code    int
	err     error
	details []*ErrorDetail
}

// An ErrorDetail is a self-describing JSON value attached to an
// [*Error]. Error details are sent over the network to clients,
// which can then work with strongly-typed data rather than trying
// to parse a complex error message. For example, you might use
// details to send a localized error message or retry parameters to
// the client.
type ErrorDetail struct {
	typ   string
	value any
}

// NewErrorDetail builds an [ErrorDetail] of the given type. The
// type identifies the payload shape to clients; the value is the
// payload itself and must be JSON-marshalable.
func NewErrorDetail(typ string, value any) *ErrorDetail {
	return &ErrorDetail{typ: typ, value: value}
}

// Type returns the detail's type identifier.
func (d *ErrorDetail) Type() string {
	return d.typ
}

// Value returns the detail's payload.
func (d *ErrorDetail) Value() any {
	return d.value
}

// MarshalJSON emits the wire shape: type and value.
func (d *ErrorDetail) MarshalJSON() ([]byte, error) {
	wire := struct {
		Type  string `json:"type"`
		Value any    `json:"value"`
	}{Type: d.typ, Value: d.value}
	return json.Marshal(wire)
}

// NewError annotates err with an HTTP status code and optional
// details.
func NewError(code int, err error, details ...*ErrorDetail) *Error {
	return &Error{code: code, err: err, details: details}
}

func (e *Error) Error() string {
	message := ""
	if e.err != nil {
		message = e.err.Error()
	}
	return fmt.Sprintf("%d: %s", e.code, message)
}

// Unwrap exposes the underlying error to errors.Is and errors.As.
func (e *Error) Unwrap() error {
	return e.err
}

// Code returns the HTTP status code.
func (e *Error) Code() int {
	return e.code
}

// Details returns the attached details.
func (e *Error) Details() []*ErrorDetail {
	return e.details
}

// MarshalJSON emits the wire shape: code, message, and any details.
func (e *Error) MarshalJSON() ([]byte, error) {
	wire := struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Details []*ErrorDetail `json:"details,omitempty"`
	}{Code: e.code, Details: e.details}
	if e.err != nil {
		wire.Message = e.err.Error()
	}
	return json.Marshal(wire)
}

// ResponseError writes err as a JSON error response. An [*Error]
// found with errors.As supplies the status code and details; any
// other error is written as 500 Internal Server Error with the
// error's message.
func ResponseError(tx http.ResponseWriter, err error) error {
	mErr, ok := errors.AsType[*Error](err)
	if !ok {
		mErr = NewError(http.StatusInternalServerError, err)
	}
	tx.Header().Set("Content-Type", "application/json")
	tx.WriteHeader(mErr.code)
	return json.NewEncoder(tx).Encode(mErr)
}
