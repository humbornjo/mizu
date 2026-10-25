package cachex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testKey reproduces Wrap's former default keying: every argument encoded,
// never expiring.
func testKey(_ context.Context, args ...any) (string, time.Duration) {
	var sb strings.Builder
	for _, a := range args {
		fmt.Fprintf(&sb, "%#v|", a)
	}
	return sb.String(), 0
}

func TestWrap_WrappersDoNotCollide(t *testing.T) {
	f1 := Wrap(func(ctx context.Context, s string) (string, error) { return "f1 " + s, nil }, testKey)
	f2 := Wrap(func(ctx context.Context, s string) (string, error) { return "f2 " + s, nil }, testKey)

	r1, err := f1(t.Context(), "x")
	require.NoError(t, err)
	r2, err := f2(t.Context(), "x")
	require.NoError(t, err)
	assert.Equal(t, "f1 x", r1)
	assert.Equal(t, "f2 x", r2, "wrappers sharing arg shape must not share entries")
}

func init() {
	Foo = Wrap(Foo, testKey)
}

var Foo = func(ctx context.Context, bar string) (string, error) {
	return "foo " + bar, nil
}

func TestFoo_Wrapped(t *testing.T) {
	r, err := Foo(t.Context(), "bar")
	require.NoError(t, err)
	assert.Equal(t, "foo bar", r)
}

func TestWrap_MultiArg(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, s string, n int) (string, error) {
		calls.Add(1)
		return fmt.Sprintf("%s-%d", s, n), nil
	}, testKey)

	for range 2 {
		r, err := fn(t.Context(), "a", 1)
		require.NoError(t, err)
		assert.Equal(t, "a-1", r)
	}
	assert.EqualValues(t, 1, calls.Load())

	r, err := fn(t.Context(), "a", 2)
	require.NoError(t, err)
	assert.Equal(t, "a-2", r)
	assert.EqualValues(t, 2, calls.Load(), "any arg change must recompute")
}

func TestWrap_PanicsOnMissingCtx(t *testing.T) {
	assert.Panics(t, func() {
		Wrap(func(n int) int {
			return n * 2
		}, testKey)
	})
}

func TestWrap_PanicsOnNilKeyf(t *testing.T) {
	assert.Panics(t, func() {
		Wrap(func(ctx context.Context) error { return nil }, nil)
	})
}

func TestWrap_Variadic(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, parts ...string) (int, error) {
		calls.Add(1)
		return len(parts), nil
	}, testKey)

	for range 2 {
		n, err := fn(t.Context(), "a", "b")
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	}
	assert.EqualValues(t, 1, calls.Load())

	n, err := fn(t.Context(), "a", "b", "c")
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.EqualValues(t, 2, calls.Load())
}

func TestWrap_KeyfKeysNonComparable(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, xs []int) (int, error) {
		calls.Add(1)
		return len(xs), nil
	}, func(_ context.Context, args ...any) (string, time.Duration) {
		return fmt.Sprint(len(args[0].([]int))), 0
	})

	for range 2 {
		n, err := fn(t.Context(), []int{1, 2})
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	}
	assert.EqualValues(t, 1, calls.Load(), "keyf decides the key — non-comparable args no longer bypass")

	n, err := fn(t.Context(), []int{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.EqualValues(t, 2, calls.Load(), "a different key must recompute")
}

func TestWrap_SkipsErrors(t *testing.T) {
	var calls atomic.Int64
	boom := errors.New("boom")
	fn := Wrap(func(ctx context.Context, s string, n int) (string, error) {
		calls.Add(1)
		return "", boom
	}, testKey)

	for range 2 {
		_, err := fn(t.Context(), "a", 1)
		require.ErrorIs(t, err, boom)
	}
	assert.EqualValues(t, 2, calls.Load())
}

func TestWrap_NilResultsRoundTrip(t *testing.T) {
	fn := Wrap(func(ctx context.Context, s string) (*int, error) {
		return nil, nil
	}, testKey)

	for range 2 {
		p, err := fn(t.Context(), "a")
		require.NoError(t, err)
		assert.Nil(t, p, "typed nil must survive the freeze/thaw cycle")
	}
}

func TestWrap_PanicsOnNonFunc(t *testing.T) {
	assert.Panics(t, func() { Wrap(42, testKey) })
}

func TestWrap_In1Out1(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, s string) string {
		calls.Add(1)
		return "out:" + s
	}, testKey)

	assert.Equal(t, "out:a", fn(t.Context(), "a"))
	assert.Equal(t, "out:a", fn(t.Context(), "a"))
	assert.EqualValues(t, 1, calls.Load(), "same arg must hit the cache")
}

func TestWrap_In3Out3(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, a string, b int, c bool) (string, int, bool) {
		calls.Add(1)
		return a + "!", b * 2, !c
	}, testKey)

	for range 2 {
		s, n, ok := fn(t.Context(), "x", 21, false)
		assert.Equal(t, "x!", s)
		assert.Equal(t, 42, n)
		assert.True(t, ok)
	}
	assert.EqualValues(t, 1, calls.Load(), "same 3 args must hit the cache")

	fn(t.Context(), "x", 22, false)
	assert.EqualValues(t, 2, calls.Load(), "one differing arg must recompute")
}

func TestWrap_In5Out1(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, a, b, c, d string) (int, error) {
		calls.Add(1)
		return len(a + b + c + d), nil
	}, testKey)

	for range 2 {
		n, err := fn(t.Context(), "a", "b", "c", "d")
		require.NoError(t, err)
		assert.Equal(t, 4, n)
	}
	assert.EqualValues(t, 1, calls.Load(), "same 5 args (ctx excluded) must hit the cache")
}

func TestWrap_In5Out3(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, a, b string, x, y int) (string, int, error) {
		calls.Add(1)
		return a + b, x + y, nil
	}, testKey)

	for range 2 {
		s, n, err := fn(t.Context(), "a", "b", 1, 2)
		require.NoError(t, err)
		assert.Equal(t, "ab", s)
		assert.Equal(t, 3, n)
	}
	assert.EqualValues(t, 1, calls.Load(), "same 5 args (ctx excluded) must hit the cache")

	_, n, err := fn(t.Context(), "a", "b", 1, 3)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.EqualValues(t, 2, calls.Load(), "last arg differing must recompute")
}

func TestWrap_KeyfTTL(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, s string) (string, error) {
		calls.Add(1)
		return "out:" + s, nil
	}, func(_ context.Context, args ...any) (string, time.Duration) {
		return args[0].(string), 20 * time.Millisecond
	})

	for range 2 {
		r, err := fn(t.Context(), "a")
		require.NoError(t, err)
		assert.Equal(t, "out:a", r)
	}
	assert.EqualValues(t, 1, calls.Load())

	time.Sleep(60 * time.Millisecond)
	fn(t.Context(), "a")
	assert.EqualValues(t, 2, calls.Load(), "entry must expire after keyf's TTL")
}

func TestCache_SetGet(t *testing.T) {
	c := NewTTL[string, int](time.Minute)
	c.Set("a", 1)

	v, ok := c.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v)

	_, ok = c.Get("missing")
	assert.False(t, ok)
}

func TestCache_ExpiresAfterTTL(t *testing.T) {
	c := NewTTL[string, int](20 * time.Millisecond)
	c.Set("a", 1)

	v, ok := c.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v, "entry must be live within the TTL")

	time.Sleep(60 * time.Millisecond)
	_, ok = c.Get("a")
	assert.False(t, ok, "entry must expire after the TTL")

	c.Set("a", 2)
	v, ok = c.Get("a")
	require.True(t, ok)
	assert.Equal(t, 2, v, "re-set must refresh the entry")
}

func TestCache_NonPositiveTTLNeverExpires(t *testing.T) {
	c := NewTTL[string, int](0)
	c.Set("a", 1)

	time.Sleep(30 * time.Millisecond)
	v, ok := c.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v)
}

func TestCache_SetTTLOverride(t *testing.T) {
	c := NewTTL[string, int](time.Minute)
	c.Set("a", 1, time.Now().Add(20*time.Millisecond))

	v, ok := c.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v, "override deadline must be live before it passes")

	time.Sleep(60 * time.Millisecond)
	_, ok = c.Get("a")
	assert.False(t, ok, "override deadline must replace the cache TTL")
}

func TestCache_ZeroValue(t *testing.T) {
	var c TTL[string, int]
	c.Set("a", 1)

	v, ok := c.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v)
}

func TestCache_Delete(t *testing.T) {
	c := NewTTL[string, int](time.Minute)
	assert.False(t, c.Delete("missing"))

	c.Set("a", 1)
	assert.True(t, c.Delete("a"))

	_, ok := c.Get("a")
	assert.False(t, ok)
}

func TestCache_Concurrent(t *testing.T) {
	c := NewTTL[int, int](time.Minute)
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Set(i%8, i)
			c.Get(i % 8)
			c.Delete(i % 16)
		}()
	}
	wg.Wait()
}

func TestCache_Sweep(t *testing.T) {
	c := NewTTL[string, int](20 * time.Millisecond)
	c.Set("expired", 1)
	c.Set("immortal", 2, time.Time{})

	time.Sleep(60 * time.Millisecond)
	c.Sweep()

	assert.NotContains(t, c.m, "expired", "sweep must evict unread expired entries")
	assert.Contains(t, c.m, "immortal", "sweep must spare deadline-free entries")
}
