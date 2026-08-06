package mizu_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/stretchr/testify/require"
)

func TestMizu_WriteError(t *testing.T) {
	recorder := httptest.NewRecorder()
	err := mizu.WriteError(recorder, http.StatusBadRequest, errors.New("invalid request"), "name")
	require.NoError(t, err)

	response := recorder.Result()
	defer response.Body.Close()
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Equal(t, "application/json", response.Header.Get("Content-Type"))

	var body mizu.Error
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Equal(t, mizu.Error{
		Code:    http.StatusBadRequest,
		Message: "invalid request",
		Details: []any{"name"},
	}, body)
}
