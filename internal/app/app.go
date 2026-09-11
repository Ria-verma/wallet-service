// Package app assembles the full HTTP application from its parts, so the
// server binary and the integration tests run the exact same stack.
package app

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-service/internal/api"
	"wallet-service/internal/config"
	"wallet-service/internal/handlers"
	"wallet-service/internal/obs"
	"wallet-service/internal/repository"
	"wallet-service/internal/service"
)

func NewHandler(cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger, metrics *obs.Metrics, ring *obs.Ring) http.Handler {
	store := repository.New(pool)
	authSvc := service.NewAuthService(store, cfg.JWTSecret, cfg.TokenTTL)
	walletSvc := service.NewWalletService(store, cfg.InitialBalancePaise, metrics)
	transferSvc := service.NewTransferService(store, cfg.InitialBalancePaise, metrics)

	authH := handlers.NewAuthHandler(authSvc)
	walletH := handlers.NewWalletHandler(walletSvc)
	transferH := handlers.NewTransferHandler(transferSvc)
	healthH := handlers.NewHealthHandler(store)

	return api.NewRouter(api.Routes{
		Signup:         authH.Signup,
		Login:          authH.Login,
		CreateAccount:  walletH.CreateAccount,
		GetMyAccount:   walletH.GetMyAccount,
		CreateTransfer: transferH.Create,
		GetTransfer:    transferH.Get,
		Healthz:        healthH.Healthz,
		Readyz:         healthH.Readyz,
		Metrics:        metrics.Handler(),
		Logs:           ring,
	}, authSvc, metrics, logger)
}
