package internal

import (
	"net/http"
)

// Package mizu -------------------------------------------------

type Mux interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handlerFunc http.HandlerFunc)

	Handler() http.Handler
	Middleware() func(http.Handler) http.Handler
	Use(middleware func(http.Handler) http.Handler) Mux

	Group(prefix string) Mux
	Get(pattern string, handler http.HandlerFunc)
	Post(pattern string, handler http.HandlerFunc)
	Put(pattern string, handler http.HandlerFunc)
	Delete(pattern string, handler http.HandlerFunc)
	Patch(pattern string, handler http.HandlerFunc)
	Head(pattern string, handler http.HandlerFunc)
	Trace(pattern string, handler http.HandlerFunc)
	Options(pattern string, handler http.HandlerFunc)
	Connect(pattern string, handler http.HandlerFunc)
}
