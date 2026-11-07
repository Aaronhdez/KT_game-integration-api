package application

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const maxRequestBodyBytes = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(destination)
	if err != nil {
		return fmt.Errorf("%w: invalid JSON body: %v", domain.ErrInvalidInput, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: invalid JSON body: unexpected data after the object", domain.ErrInvalidInput)
	}
	return nil
}
