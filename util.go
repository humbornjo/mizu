package mizu

import (
	"unsafe"

	"github.com/humbornjo/mizu/internal"
)

// R is an implementation of the Rust Option type. Here it is
// named R instead of O(ption) to avoid name collisions with the
// widely adopted With Option paradigm.
type R[T any] = internal.R

// None represents an option with no value.
var None = internal.None

type Some[T any] func(*T) R[T]

// Rption is used to create an rust style option. It is called
// Rption to avoid name collisions with the widely adopted With
// Option paradigm. It converts a pointer to a value of type T
// into an option like the one in Rust. If the provided pointer
// is nil, it returns None. Otherwise, it creates a closure that
// captures the value pointed to by x. When this closure is
// unwrapped (via Match), it copies the encapsulated value into
// the provided target pointer.
//
// Example:
//
//	x := 42
//	opt := Rption(&x)
func Rption[T any](x *T) R[T] {
	if x == nil {
		return None
	}
	f := func(t *T) R[T] {
		*t = *x
		return internal.Some
	}
	// nolint: gosec
	return R[T](unsafe.Pointer(&f))
}

// Match inspects the provided option. If the option is None, it
// returns None and a nil extraction function. Otherwise, it
// returns a sentinel "some" value and a function that, when
// called with a pointer to T, invokes the stored closure to copy
// the encapsulated value into that pointer. This provides a
// mechanism to safely extract the stored value.
//
// Example usage:
//
//	val := new(T)
//	switch o, Some := Match\[T\](opt); o {
//	case None:
//	    // do things when None
//	case Some(val):
//	    // do things with retrieved val
//	}
func Match[T any](o R[T]) (R[T], Some[T]) {
	if o == None {
		return None, nil
	}
	return internal.Some, (*(*Some[T])(o))
}
