package mizu_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/stretchr/testify/require"
)

type errorWire struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details []struct {
		Type  string `json:"type"`
		Value any    `json:"value"`
	} `json:"details,omitempty"`
}

func TestMizu_ResponseError(t *testing.T) {
	recorder := httptest.NewRecorder()
	merr := mizu.NewError(http.StatusBadRequest, errors.New("invalid request"),
		mizu.NewErrorDetail("field_violation", map[string]any{"field": "name"}))
	require.NoError(t, mizu.ResponseError(recorder, merr))

	response := recorder.Result()
	defer response.Body.Close()
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Equal(t, "application/json", response.Header.Get("Content-Type"))

	var body errorWire
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Equal(t, http.StatusBadRequest, body.Code)
	require.Equal(t, "invalid request", body.Message)
	require.Len(t, body.Details, 1)
	require.Equal(t, "field_violation", body.Details[0].Type)
	require.Equal(t, map[string]any{"field": "name"}, body.Details[0].Value)
}

func TestMizu_ResponseError_PlainErrorDefaultsTo500(t *testing.T) {
	recorder := httptest.NewRecorder()
	require.NoError(t, mizu.ResponseError(recorder, errors.New("boom")))

	response := recorder.Result()
	defer response.Body.Close()
	require.Equal(t, http.StatusInternalServerError, response.StatusCode)

	var body errorWire
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Equal(t, errorWire{Code: http.StatusInternalServerError, Message: "boom"}, body)
}

func TestMizu_ResponseError_WrappedError(t *testing.T) {
	recorder := httptest.NewRecorder()
	merr := mizu.NewError(http.StatusNotFound, errors.New("user missing"))
	require.NoError(t, mizu.ResponseError(recorder, fmt.Errorf("handler: %w", merr)))
	require.Equal(t, http.StatusNotFound, recorder.Result().StatusCode)
}

func TestMizu_ErrorAccessors(t *testing.T) {
	cause := errors.New("invalid request")
	merr := mizu.NewError(http.StatusBadRequest, cause,
		mizu.NewErrorDetail("retry", map[string]any{"after": "1s"}))

	require.Equal(t, "400: invalid request", merr.Error())
	require.ErrorIs(t, merr, cause)
	require.Equal(t, http.StatusBadRequest, merr.Code())
	require.Len(t, merr.Details(), 1)
	require.Equal(t, "retry", merr.Details()[0].Type())
	require.Equal(t, map[string]any{"after": "1s"}, merr.Details()[0].Value())
}
