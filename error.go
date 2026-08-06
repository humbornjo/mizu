package mizu

import (
	"encoding/json"
	"net/http"
)

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details []any  `json:"details,omitempty"`
}

func WriteError(tx http.ResponseWriter, statusCode int, err error, details ...any) error {
	tx.Header().Set("Content-Type", "application/json")
	tx.WriteHeader(statusCode)

	canonical := &Error{
		Code:    statusCode,
		Message: err.Error(),
		Details: details,
	}
	return json.NewEncoder(tx).Encode(canonical)
}
