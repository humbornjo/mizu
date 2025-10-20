package bidikit

import (
	"errors"
	"io"
	"iter"

	"connectrpc.com/connect"
)

func NewIterator[Req, Rsp any](s *connect.BidiStream[Req, Rsp]) iter.Seq2[*Req, error] {
	return func(yield func(*Req, error) bool) {
		for {
			req, err := s.Receive()
			if errors.Is(err, io.EOF) {
				return
			}
			if !yield(req, err) {
				return
			}
		}
	}
}
