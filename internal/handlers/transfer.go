package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"wallet-service/internal/api"
	"wallet-service/internal/dto"
	"wallet-service/internal/service"
)

type TransferHandler struct {
	transfers *service.TransferService
}

func NewTransferHandler(transfers *service.TransferService) *TransferHandler {
	return &TransferHandler{transfers: transfers}
}

func (h *TransferHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req dto.TransferRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	senderID := api.UserID(r.Context())

	result, err := h.transfers.Execute(r.Context(), senderID, req.ToUser, req.AmountPaise, req.IdempotencyKey)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		// A retry: return the ORIGINAL outcome, marked so clients can tell.
		w.Header().Set("Idempotent-Replay", "true")
		status = http.StatusOK
	}
	writeJSON(w, status, dto.TransferResponse{
		TransferID: result.TransferID.String(),
		NewBalance: result.NewBalance,
	})
}

func (h *TransferHandler) Get(w http.ResponseWriter, r *http.Request) {
	transferID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	callerID := api.UserID(r.Context())

	tr, err := h.transfers.GetByID(r.Context(), callerID, transferID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.TransferDetails{
		TransferID:  tr.ID.String(),
		SenderID:    tr.SenderID.String(),
		RecipientID: tr.RecipientID.String(),
		AmountPaise: tr.AmountPaise,
		CreatedAt:   tr.CreatedAt,
	})
}
