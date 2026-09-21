package cachex

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrap_WrappersDoNotCollide(t *testing.T) {
	f1 := Wrap(func(ctx context.Context, s string) (string, error) { return "f1 " + s, nil })
	f2 := Wrap(func(ctx context.Context, s string) (string, error) { return "f2 " + s, nil })

	r1, err := f1(t.Context(), "x")
	require.NoError(t, err)
	r2, err := f2(t.Context(), "x")
	require.NoError(t, err)
	assert.Equal(t, "f1 x", r1)
	assert.Equal(t, "f2 x", r2, "wrappers sharing arg shape must not share entries")
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
	})

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

func TestWrap_NoCtxNoError(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(n int) int {
		calls.Add(1)
		return n * 2
	})

	assert.Equal(t, 4, fn(2))
	assert.Equal(t, 4, fn(2))
	assert.EqualValues(t, 1, calls.Load())
}

func TestWrap_Variadic(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, parts ...string) (int, error) {
		calls.Add(1)
		return len(parts), nil
	})

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

func TestWrap_NonComparableBypasses(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, xs []int) (int, error) {
		calls.Add(1)
		return len(xs), nil
	})

	for range 2 {
		n, err := fn(t.Context(), []int{1, 2})
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	}
	assert.EqualValues(t, 2, calls.Load(), "unkeyable args must pass through uncached")
}

func TestWrap_SkipsErrors(t *testing.T) {
	var calls atomic.Int64
	boom := errors.New("boom")
	fn := Wrap(func(ctx context.Context, s string, n int) (string, error) {
		calls.Add(1)
		return "", boom
	})

	for range 2 {
		_, err := fn(t.Context(), "a", 1)
		require.ErrorIs(t, err, boom)
	}
	assert.EqualValues(t, 2, calls.Load())
}

func TestWrap_NilResultsRoundTrip(t *testing.T) {
	fn := Wrap(func(ctx context.Context, s string) (*int, error) {
		return nil, nil
	})

	for range 2 {
		p, err := fn(t.Context(), "a")
		require.NoError(t, err)
		assert.Nil(t, p, "typed nil must survive the freeze/thaw cycle")
	}
}

func TestWrap_PanicsOnNonFunc(t *testing.T) {
	assert.Panics(t, func() { Wrap(42) })
}

func TestWrap_In1Out1(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(s string) string {
		calls.Add(1)
		return "out:" + s
	})

	assert.Equal(t, "out:a", fn("a"))
	assert.Equal(t, "out:a", fn("a"))
	assert.EqualValues(t, 1, calls.Load(), "same arg must hit the cache")
}

func TestWrap_In3Out3(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(a string, b int, c bool) (string, int, bool) {
		calls.Add(1)
		return a + "!", b * 2, !c
	})

	for range 2 {
		s, n, ok := fn("x", 21, false)
		assert.Equal(t, "x!", s)
		assert.Equal(t, 42, n)
		assert.True(t, ok)
	}
	assert.EqualValues(t, 1, calls.Load(), "same 3 args must hit the cache")

	fn("x", 22, false)
	assert.EqualValues(t, 2, calls.Load(), "one differing arg must recompute")
}

func TestWrap_In5Out1(t *testing.T) {
	var calls atomic.Int64
	fn := Wrap(func(ctx context.Context, a, b, c, d string) (int, error) {
		calls.Add(1)
		return len(a + b + c + d), nil
	})

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
	})

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
