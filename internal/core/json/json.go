package json

import (
	"encoding/json"
	"errors"
	"net/http"
)

var (
	ErrInvalidRequest = errors.New("Invalid Request.")
)

type ErrorResponse struct {
	Error string `json:"error"`
}

func Write(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func WriteError(w http.ResponseWriter, err string, status int) {
	Write(w, status, ErrorResponse{Error: err})
}
