package handlers

import (
	"net/http"

	"wallet-service/internal/dto"
	"wallet-service/internal/service"
)

type AuthHandler struct {
	auth *service.AuthService
}

func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

func (h *AuthHandler) Signup(w http.ResponseWriter, r *http.Request) {
	var req dto.SignupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, token, err := h.auth.Signup(r.Context(), req.Username, req.Password)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.AuthResponse{Token: token, UserID: user.ID.String()})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.SignupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, token, err := h.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.AuthResponse{Token: token, UserID: user.ID.String()})
}
