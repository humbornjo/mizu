package cachex

import (
	"container/list"
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

const (
	_DEFAULT_MAX_ENTRIES = 1024
	_SWEEP_SAMPLE        = 32
)

// TTL is a concurrency-safe in-memory cache whose entries expire a fixed
// TTL after being set. Expired entries are evicted on access, sampled
// opportunistically on Get and Set, and cleared in full by Sweep. Total
// entries are bounded by a maximum capacity using LRU eviction.
//
// The zero value is a ready-to-use cache whose entries never expire.
type TTL[K comparable, V any] struct {
	ttl        time.Duration
	maxEntries int
	mu         sync.Mutex
	m          map[K]*list.Element
	lru        *list.List
}

type entry[K comparable, V any] struct {
	key K
	val V
	exp time.Time // zero when the cache has no TTL
}

// NewTTL returns a Cache whose entries expire ttl after being set. A
// non-positive ttl disables expiry, matching the package-global cache used
// by Wrap.
func NewTTL[K comparable, V any](ttl time.Duration) *TTL[K, V] {
	return &TTL[K, V]{
		ttl: ttl,
		m:   make(map[K]*list.Element),
		lru: list.New(),
	}
}

// Get returns the value stored under k, or false when k is absent or its
// entry has expired.
func (c *TTL[K, V]) Get(k K) (v V, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		return v, false
	}
	now := time.Now()
	c.evictExpiredLocked(now, _SWEEP_SAMPLE)
	elem, ok := c.m[k]
	if !ok {
		return v, false
	}
	e := elem.Value.(*entry[K, V])
	if !e.exp.IsZero() && now.After(e.exp) {
		c.removeLocked(elem)
		return v, false
	}
	c.lru.MoveToFront(elem)
	return e.val, true
}

// Set stores v under k. The entry expires after the cache TTL, or never
// when the cache has a non-positive TTL. The first ttl, when given,
// overrides both as the entry's exact deadline; extra values are ignored.
func (c *TTL[K, V]) Set(k K, v V, ttl ...time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[K]*list.Element)
		c.lru = list.New()
	}
	now := time.Now()
	var exp time.Time
	switch {
	case len(ttl) > 0:
		exp = ttl[0]
	case c.ttl > 0:
		exp = now.Add(c.ttl)
	}
	c.evictExpiredLocked(now, _SWEEP_SAMPLE)

	if !exp.IsZero() && now.After(exp) {
		if elem, ok := c.m[k]; ok {
			c.removeLocked(elem)
		}
		return
	}
	if elem, ok := c.m[k]; ok {
		e := elem.Value.(*entry[K, V])
		e.val = v
		e.exp = exp
		c.lru.MoveToFront(elem)
		return
	}
	maxEntries := c.maxEntries
	if maxEntries <= 0 {
		maxEntries = _DEFAULT_MAX_ENTRIES
	}
	if len(c.m) >= maxEntries {
		c.evictExpiredLocked(now, 0)
		for len(c.m) >= maxEntries && c.lru.Len() > 0 {
			c.removeLocked(c.lru.Back())
		}
	}
	e := &entry[K, V]{key: k, val: v, exp: exp}
	c.m[k] = c.lru.PushFront(e)
}

// Delete removes the entry under k, reporting whether it was present.
func (c *TTL[K, V]) Delete(k K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		return false
	}
	elem, ok := c.m[k]
	if !ok {
		return false
	}
	c.removeLocked(elem)
	return true
}

// Sweep evicts every expired entry, including those never read past their
// deadline.
func (c *TTL[K, V]) Sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		return
	}
	c.evictExpiredLocked(time.Now(), 0)
}

func (c *TTL[K, V]) removeLocked(elem *list.Element) {
	e := elem.Value.(*entry[K, V])
	delete(c.m, e.key)
	c.lru.Remove(elem)
}

func (c *TTL[K, V]) evictExpiredLocked(now time.Time, limit int) {
	checked := 0
	for _, elem := range c.m {
		e := elem.Value.(*entry[K, V])
		if !e.exp.IsZero() && now.After(e.exp) {
			c.removeLocked(elem)
		}
		checked++
		if limit > 0 && checked >= limit {
			return
		}
	}
}
