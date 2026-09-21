package cachex

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
)

// global is the cache shared by every wrapped function. Keys are
// namespaced by a per-wrapper id so two wrapped functions with
// identically-shaped arguments never collide.
var global sync.Map

var nextID atomic.Uint64

var (
	ctxType   = reflect.TypeFor[context.Context]()
	errorType = reflect.TypeFor[error]()
)

// Wrap memoizes a function of any signature in the package-global
// cache. The rules:
//   - a leading context.Context argument is excluded from the key;
//   - every remaining argument must be comparable — a call carrying a
//     non-comparable argument bypasses the cache entirely;
//   - a non-nil trailing error result is never cached.
//
// Wrap panics if F is not a func. Note the reflect call path and
// key encoding cost noticeably more than a direct call — wrap
// functions whose computation is expensive enough to pay for it.
//
// Usage:
//
//	var Bar = func(ctx context.Context, a string, n int) (string, error) { ... }
//
//	func init() { Bar = Wrap(Bar) }
func Wrap[F any](fn F) F {
	t := reflect.TypeFor[F]()
	if t.Kind() != reflect.Func {
		panic("cachex: Wrap requires a func, got " + t.String())
	}
	v := reflect.ValueOf(fn)
	id := nextID.Add(1)

	// MakeFunc packs variadic arguments into a final slice, which is
	// the form CallSlice expects; plain calls unpack instead.
	call := v.Call
	if t.IsVariadic() {
		call = v.CallSlice
	}

	// A leading ctx is carried through to fn but excluded from the key.
	skip := 0
	if t.NumIn() > 0 && t.In(0).Implements(ctxType) {
		skip = 1
	}
	// A trailing error gates caching.
	errIdx := -1
	if n := t.NumOut(); n > 0 && t.Out(n-1).Implements(errorType) {
		errIdx = n - 1
	}

	wrapped := reflect.MakeFunc(t, func(args []reflect.Value) []reflect.Value {
		var sb strings.Builder
		fmt.Fprintf(&sb, "%d|", id)
		keyed := args[skip:]
		for i, a := range keyed {
			if t.IsVariadic() && i == len(keyed)-1 {
				// The packed variadic tail is a slice; key on its elements instead.
				for j := range a.Len() {
					if e := a.Index(j); e.Comparable() {
						fmt.Fprintf(&sb, "%#v|", e.Interface())
					} else {
						return call(args)
					}
				}
				continue
			}
			if !a.Comparable() {
				return call(args) // unkeyable, best-effort: uncached
			}
			fmt.Fprintf(&sb, "%#v|", a.Interface())
		}
		k := sb.String()
		if hit, ok := global.Load(k); ok {
			return thaw(t, hit.([]any))
		}
		outs := call(args)
		if errIdx >= 0 && !outs[errIdx].IsNil() {
			return outs
		}
		frozen := make([]any, len(outs))
		for i, o := range outs {
			frozen[i] = o.Interface()
		}
		stored, _ := global.LoadOrStore(k, frozen)
		return thaw(t, stored.([]any))
	})
	return wrapped.Interface().(F)
}

// thaw rebuilds call results from their frozen any form, restoring
// typed nils that reflect.ValueOf alone would lose.
func thaw(t reflect.Type, frozen []any) []reflect.Value {
	outs := make([]reflect.Value, len(frozen))
	for i, f := range frozen {
		if f == nil {
			outs[i] = reflect.Zero(t.Out(i))
		} else {
			outs[i] = reflect.ValueOf(f)
		}
	}
	return outs
}

func init() {
	Foo = Wrap(Foo)
}

var Foo = func(ctx context.Context, bar string) (string, error) {
	return "foo " + bar, nil
}
