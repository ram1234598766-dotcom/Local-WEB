package dht

import (
	"bytes"
	"errors"
)

func marshalGob(v interface{}) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func unmarshalGob(data []byte, v interface{}) error {
	return errors.New("not implemented")
}

type networkReader struct {
	buf *bytes.Buffer
}

func (r *networkReader) Read(p []byte) (int, error) {
	return r.buf.Read(p)
}

type networkWriter struct {
	buf *bytes.Buffer
}

func (w *networkWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}
