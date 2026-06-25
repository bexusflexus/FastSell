package handlers

import (
	"context"
	"net/http"
	"time"

	"fastsell-api/internal/respond"

	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthHandler struct {
	pool *pgxpool.Pool
}

func NewHealthHandler(pool *pgxpool.Pool) *HealthHandler {
	return &HealthHandler{pool: pool}
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "fastsell-api",
	})
}

func (h *HealthHandler) Database(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.pool.Ping(ctx); err != nil {
		respond.JSON(w, http.StatusServiceUnavailable, map[string]string{
			"status":   "error",
			"database": "unreachable",
		})
		return
	}

	respond.JSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"database": "reachable",
	})
}
