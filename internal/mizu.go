package internal

import (
	"net/http"
	"unsafe"
)

// --------------------------------------------------------------
// Option Implementation
//
// This implementation provides a minimal "option" type using
// unsafe.Pointer. It encapsulates a pointer to a value of any
// type T and allows matching and extracting that value via a
// closure.
//
// The option type is defined as an alias for unsafe.Pointer. A
// nil option represents the absence of a value (i.e. None). When
// a non-nil pointer is provided, Option[T] creates a closure
// that, when invoked, assigns the stored value to a given target
// pointer. The Match function checks whether the option is None
// or contains a value and returns an extraction function.
//
// Note: This implementation uses unsafe operations. Caution is
// advised when integrating it into production code.
// --------------------------------------------------------------

var (
	// some_v is an auxiliary variable used to init the `Some`.
	some_v int = 1

	// None represents an option with no value.
	None R = R(nil)

	// `Some` is a sentinel value representing the presence of a
	// value.
	//
	// nolint: gosec
	Some R = R(*(**unsafe.Pointer)(unsafe.Pointer(&some_v)))
)

// R is used to represent an optional value.
type R unsafe.Pointer

// Package mizu -------------------------------------------------

type Mux interface {
	Get(pattern string, handler http.HandlerFunc)
	Post(pattern string, handler http.HandlerFunc)
	Put(pattern string, handler http.HandlerFunc)
	Delete(pattern string, handler http.HandlerFunc)
	Patch(pattern string, handler http.HandlerFunc)
	Head(pattern string, handler http.HandlerFunc)
	Trace(pattern string, handler http.HandlerFunc)
	Options(pattern string, handler http.HandlerFunc)
	Connect(pattern string, handler http.HandlerFunc)
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handlerFunc http.HandlerFunc)
	Use(middleware func(http.Handler) http.Handler) Mux
}
