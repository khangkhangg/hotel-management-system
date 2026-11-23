package yieldrules

import (
	"encoding/json"
	"io"
	"net/http"
)

// jsonDecoder reads a small request body and refuses fields it does not know.
func jsonDecoder(r *http.Request) *json.Decoder {
	d := json.NewDecoder(io.LimitReader(r.Body, 8<<10))
	d.DisallowUnknownFields()
	return d
}
