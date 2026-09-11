package handlers

import (
	"net/http"

	"wallet-service/internal/api"
	"wallet-service/internal/dto"
	"wallet-service/internal/service"
)

type WalletHandler struct {
	wallets *service.WalletService
}

func NewWalletHandler(wallets *service.WalletService) *WalletHandler {
	return &WalletHandler{wallets: wallets}
}

// CreateAccount is get-or-create: calling it twice returns the same wallet.
func (h *WalletHandler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	userID := api.UserID(r.Context())
	balance, created, err := h.wallets.GetOrCreate(r.Context(), userID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, dto.AccountResponse{UserID: userID.String(), BalancePaise: balance})
}

func (h *WalletHandler) GetMyAccount(w http.ResponseWriter, r *http.Request) {
	userID := api.UserID(r.Context())
	balance, err := h.wallets.GetBalance(r.Context(), userID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.BalanceResponse{BalancePaise: balance})
}
