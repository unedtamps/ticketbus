package http

import (
	"encoding/json"
	"net/http"
)

// Response is a standard JSON response envelope.
type Response[T any] struct {
	Data    T      `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// OK writes a 200 OK response.
func OK(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, Response[interface{}]{Data: data})
}

// Created writes a 201 Created response.
func Created(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusCreated, Response[interface{}]{Data: data})
}

// NoContent writes a 204 No Content response.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Error writes an error response with the given status code.
func Error(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, Response[interface{}]{Error: message})
}

// BadRequest writes a 400 Bad Request response.
func BadRequest(w http.ResponseWriter, message string) {
	Error(w, http.StatusBadRequest, message)
}

// Unauthorized writes a 401 Unauthorized response.
func Unauthorized(w http.ResponseWriter, message string) {
	Error(w, http.StatusUnauthorized, message)
}

// Forbidden writes a 403 Forbidden response.
func Forbidden(w http.ResponseWriter, message string) {
	Error(w, http.StatusForbidden, message)
}

// NotFound writes a 404 Not Found response.
func NotFound(w http.ResponseWriter, message string) {
	Error(w, http.StatusNotFound, message)
}

// InternalServerError writes a 500 Internal Server Error response.
func InternalServerError(w http.ResponseWriter, message string) {
	Error(w, http.StatusInternalServerError, message)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// DecodeJSON decodes a JSON request body into the given target.
func DecodeJSON(r *http.Request, target interface{}) error {
	if r.Body == nil {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(target)
}
