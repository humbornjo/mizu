package mizuoai

import (
	"net/http"

	"github.com/humbornjo/mizu"
)

type Option func(*config)

type config struct {
}

type Rx[T any] struct {
}

func (r Rx[T]) Read() *T {

	return new(T)
}

type Tx[T any] struct {
}

func (t Tx[T]) Write(data *T) error {
	return nil
}

func Get[I any, O any](s *mizu.Server, pattern string, handler func(w Tx[O], r Rx[I]), opts ...Option) {
	s.Use(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r)
		})
	}).Get(pattern, func(w http.ResponseWriter, r *http.Request) {
		handler(Tx[O]{}, Rx[I]{})
	})
}
