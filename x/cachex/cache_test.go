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

func TestCachex_TTLEvictsExpiredWithoutSweep(t *testing.T) {
	tests := []struct {
		name      string
		action    func(c *TTL[string, int])
		wantCount int
		wantKeys  []string
	}{
		{
			name: "unrelated get purges expired entries",
			action: func(c *TTL[string, int]) {
				_, ok := c.Get("unrelated")
				assert.False(t, ok)
			},
			wantCount: 0,
		},
		{
			name: "set purges expired entries",
			action: func(c *TTL[string, int]) {
				c.Set("fresh", 99, time.Now().Add(time.Minute))
			},
			wantCount: 1,
			wantKeys:  []string{"fresh"},
		},
		{
			name: "already expired set does not retain entry",
			action: func(c *TTL[string, int]) {
				c.Set("dead", 1, time.Now().Add(-time.Second))
			},
			wantCount: 0,
		},
		{
			name: "immortal entry survives opportunistic eviction",
			action: func(c *TTL[string, int]) {
				c.Set("immortal", 42, time.Time{})
				_, ok := c.Get("unrelated")
				assert.False(t, ok)
			},
			wantCount: 1,
			wantKeys:  []string{"immortal"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewTTL[string, int](15 * time.Millisecond)
			for i := range 16 {
				c.Set(fmt.Sprintf("k-%d", i), i)
			}
			time.Sleep(40 * time.Millisecond)

			tc.action(c)

			assert.Len(t, c.m, tc.wantCount)
			assert.Equal(t, tc.wantCount, c.lru.Len())
			for _, k := range tc.wantKeys {
				assert.Contains(t, c.m, k)
			}
		})
	}
}

func TestCachex_TTLBoundedMaxEntries(t *testing.T) {
	t.Run("lru eviction on custom cap", func(t *testing.T) {
		c := &TTL[string, int]{maxEntries: 3}
		c.Set("a", 1)
		c.Set("b", 2)
		c.Set("c", 3)

		// Touch "a" so "b" becomes least recently used.
		v, ok := c.Get("a")
		require.True(t, ok)
		assert.Equal(t, 1, v)

		c.Set("d", 4)

		assert.Len(t, c.m, 3)
		assert.Equal(t, 3, c.lru.Len())
		assert.NotContains(t, c.m, "b", "least recently used entry must be evicted")
		assert.Contains(t, c.m, "a")
		assert.Contains(t, c.m, "c")
		assert.Contains(t, c.m, "d")
	})

	t.Run("expired entries reclaimed before evicting live entries", func(t *testing.T) {
		c := &TTL[string, int]{ttl: time.Minute, maxEntries: 3}
		past := time.Now().Add(-time.Second)
		c.Set("live-1", 1)
		c.Set("live-2", 2)
		c.Set("exp", 3, time.Now().Add(15*time.Millisecond))
		time.Sleep(40 * time.Millisecond)

		// Insert a new key while "exp" is expired; live entries must survive.
		c.Set("live-3", 4)
		c.Set("dead", 5, past)

		assert.Len(t, c.m, 3)
		assert.NotContains(t, c.m, "exp")
		assert.Contains(t, c.m, "live-1")
		assert.Contains(t, c.m, "live-2")
		assert.Contains(t, c.m, "live-3")
	})

	tests := []struct {
		name  string
		cache func() *TTL[int, int]
	}{
		{
			name:  "default cap on NewTTL",
			cache: func() *TTL[int, int] { return NewTTL[int, int](0) },
		},
		{
			name:  "default cap on zero-value TTL",
			cache: func() *TTL[int, int] { return &TTL[int, int]{} },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.cache()
			for i := range _DEFAULT_MAX_ENTRIES + 64 {
				c.Set(i, i)
			}
			assert.Len(t, c.m, _DEFAULT_MAX_ENTRIES)
			assert.Equal(t, _DEFAULT_MAX_ENTRIES, c.lru.Len())
		})
	}
}
