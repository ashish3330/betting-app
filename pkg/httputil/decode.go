package httputil

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

var ErrEmptyBody = errors.New("empty request body")

// DecodeJSON reads a JSON body with a size limit and decodes it into v.
// Returns ErrEmptyBody if the body is empty.
// Returns a generic error for any decode failure (caller should respond with 400).
func DecodeJSON(r *http.Request, v interface{}, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20 // 1 MB default
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if err == io.EOF {
			return ErrEmptyBody
		}
		return err
	}
	return nil
}
