package sql

import "github.com/vmihailenco/msgpack/v5"

// Codec serializes/deserializes values stored in L2 cache.
type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
}

// MsgpackCodec is the default codec.
type MsgpackCodec struct{}

func (MsgpackCodec) Marshal(v any) ([]byte, error) {
	return msgpack.Marshal(v)
}

func (MsgpackCodec) Unmarshal(data []byte, v any) error {
	return msgpack.Unmarshal(data, v)
}
