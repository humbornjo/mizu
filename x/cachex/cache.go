package cachex

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

// global is the cache shared by every wrapped function. Keys are
// namespaced by a per-wrapper id so two wrapped functions whose key funcs
// return the same key never collide.
var global TTL[string, []any]

var nextID atomic.Uint64

// _EXPIRE_NEVER is the ridiculous deadline standing in for "never expire"
// when keyf returns a non-positive TTL.
var _EXPIRE_NEVER = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

var (
	_TYPE_CONTEXT = reflect.TypeFor[context.Context]()
	_TYPE_ERROR   = reflect.TypeFor[error]()
)

// Wrap memoizes a function in the package-global cache, keying every
// call with keyf. The rules:
//   - the first argument must be a context.Context; it rides along to
//     fn and keyf but is not itself key material;
//   - keyf maps the remaining arguments (a variadic tail arrives as
//     its packed slice) to the entry's key and TTL — a non-positive
//     TTL never expires;
//   - a non-nil trailing error result is never cached.
//
// Wrap panics if F is not a func, has no leading context.Context, or keyf
// is nil. Note the reflect call path and keyf cost noticeably more than a
// direct call — wrap functions whose computation is expensive enough to
// pay for it.
//
// Usage:
//
//	var Bar = func(ctx context.Context, a string, n int) (string, error) { ... }
//
//	func init() {
//		Bar = Wrap(Bar, func(_ context.Context, args ...any) (string, time.Duration) {
//			return fmt.Sprintf("%s:%d", args[0], args[1]), time.Minute
//		})
//	}
func Wrap[F any](fn F, keyf func(context.Context, ...any) (string, time.Duration)) F {
	t := reflect.TypeFor[F]()
	if t.Kind() != reflect.Func {
		panic("cachex: Wrap requires a func, got " + t.String())
	}
	if t.NumIn() == 0 || !t.In(0).Implements(_TYPE_CONTEXT) {
		panic("cachex: Wrap requires a leading context.Context, got " + t.String())
	}
	if keyf == nil {
		panic("cachex: Wrap requires a key func")
	}
	v := reflect.ValueOf(fn)
	id := nextID.Add(1)

	// MakeFunc packs variadic arguments into a final slice, which is the form CallSlice expects; plain calls unpack
	// instead.
	call := v.Call
	if t.IsVariadic() {
		call = v.CallSlice
	}

	// A trailing error gates caching.
	errIdx := -1
	if n := t.NumOut(); n > 0 && t.Out(n-1).Implements(_TYPE_ERROR) {
		errIdx = n - 1
	}

	// thaw rebuilds call results from their frozen any form, restoring typed nils that reflect.ValueOf alone would lose.
	thaw := func(frozen []any) []reflect.Value {
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

	wrapped := reflect.MakeFunc(t, func(args []reflect.Value) []reflect.Value {
		ctx, ok := args[0].Interface().(context.Context)
		if !ok || ctx == nil {
			ctx = context.Background() // a nil ctx still keys
		}
		rest := make([]any, len(args)-1)
		for i, a := range args[1:] {
			rest[i] = a.Interface()
		}
		key, dur := keyf(ctx, rest...)
		k := fmt.Sprintf("%d|%s", id, key)
		if hit, ok := global.Get(k); ok {
			return thaw(hit)
		}
		outs := call(args)
		if errIdx >= 0 && !outs[errIdx].IsNil() {
			return outs
		}
		frozen := make([]any, len(outs))
		for i, o := range outs {
			frozen[i] = o.Interface()
		}
		exp := time.Now().Add(dur)
		if dur <= 0 {
			exp = _EXPIRE_NEVER
		}
		global.Set(k, frozen, exp)
		return outs
	})
	return wrapped.Interface().(F)
}

// TTL is a concurrency-safe in-memory cache whose entries expire a fixed
// TTL after being set. Expiry is lazy: an entry is evicted when read past
// its deadline rather than by a background sweep, so unread expired
// entries linger until touched or Swept.
//
// The zero value is a ready-to-use cache whose entries never expire.
type TTL[K comparable, V any] struct {
	ttl time.Duration
	mu  sync.Mutex
	m   map[K]entry[V]
}

type entry[V any] struct {
	val V
	exp time.Time // zero when the cache has no TTL
}

// NewTTL returns a Cache whose entries expire ttl after being set. A
// non-positive ttl disables expiry, matching the package-global cache used
// by Wrap.
func NewTTL[K comparable, V any](ttl time.Duration) *TTL[K, V] {
	return &TTL[K, V]{ttl: ttl, m: make(map[K]entry[V])}
}

// Get returns the value stored under k, or false when k is absent or its
// entry has expired.
func (c *TTL[K, V]) Get(k K) (v V, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok {
		return v, false
	}
	if !e.exp.IsZero() && time.Now().After(e.exp) {
		delete(c.m, k)
		return v, false
	}
	return e.val, true
}

// Set stores v under k. The entry expires after the cache TTL, or never
// when the cache has a non-positive TTL. The first ttl, when given,
// overrides both as the entry's exact deadline; extra values are ignored.
func (c *TTL[K, V]) Set(k K, v V, ttl ...time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[K]entry[V])
	}
	e := entry[V]{val: v}
	switch {
	case len(ttl) > 0:
		e.exp = ttl[0]
	case c.ttl > 0:
		e.exp = time.Now().Add(c.ttl)
	}
	c.m[k] = e
}

// Delete removes the entry under k, reporting whether it was present.
func (c *TTL[K, V]) Delete(k K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.m[k]
	delete(c.m, k)
	return ok
}

// Sweep evicts every expired entry, including those never read past their
// deadline.
func (c *TTL[K, V]) Sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.m {
		if !e.exp.IsZero() && now.After(e.exp) {
			delete(c.m, k)
		}
	}
}
