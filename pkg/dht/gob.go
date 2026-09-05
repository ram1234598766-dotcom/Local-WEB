package dht

import (
	"bytes"
	"encoding/gob"
)

func marshalGob(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func unmarshalGob(data []byte, v interface{}) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(v)
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
