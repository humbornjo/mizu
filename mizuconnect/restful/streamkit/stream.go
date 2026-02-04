package streamkit

import (
	"errors"
	"io"
	"iter"

	"connectrpc.com/connect"
)

// FromClientStream converts a connect.ClientStream to an iterator.
// This function serves as a convenience wrapper for receiving
// messages from client stream without checking for EOF.
func FromClientStream[Req any](stream *connect.ClientStream[Req]) iter.Seq2[*Req, error] {
	return func(yield func(*Req, error) bool) {
		for {
			if !stream.Receive() {
				if err := stream.Err(); errors.Is(err, io.EOF) {
					return
				} else {
					yield(nil, err)
					return
				}
			}
			req := stream.Msg()
			if !yield(req, nil) {
				return
			}
		}
	}
}

// FromBidiStream converts a connect.BidiStream to an iterator. This
// function serves as a convenience wrapper for receiving messages
// from bidi stream without checking for EOF.
func FromBidiStream[Req, Rsp any](stream *connect.BidiStream[Req, Rsp]) iter.Seq2[*Req, error] {
	return func(yield func(*Req, error) bool) {
		for {
			req, err := stream.Receive()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				yield(nil, err)
				return
			}
			if !yield(req, err) {
				return
			}
		}
	}
}
