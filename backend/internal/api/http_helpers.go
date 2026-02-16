package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"personaworlds/backend/internal/auth"
)

func decodeJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func decodeJSONAllowEmpty(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(dst)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func writeBadRequest(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, message)
}

func writeUnauthorized(w http.ResponseWriter, message string) {
	writeError(w, http.StatusUnauthorized, message)
}

func writeForbidden(w http.ResponseWriter, message string) {
	writeError(w, http.StatusForbidden, message)
}

func writeNotFound(w http.ResponseWriter, message string) {
	writeError(w, http.StatusNotFound, message)
}

func writeConflict(w http.ResponseWriter, message string) {
	writeError(w, http.StatusConflict, message)
}

func writeTooManyRequests(w http.ResponseWriter, message string) {
	writeError(w, http.StatusTooManyRequests, message)
}

func writeBadGateway(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadGateway, message)
}

func writeInternalError(w http.ResponseWriter, message string) {
	writeError(w, http.StatusInternalServerError, message)
}

func (s *Server) requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w, "missing user")
		return "", false
	}
	return userID, true
}
