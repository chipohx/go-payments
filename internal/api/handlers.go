package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"go-payments/internal/models"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type Storage interface {
	GetWalletBalance(ctx context.Context, address string) (*models.Wallet, error)
	GetLastTransactions(ctx context.Context, n int) ([]models.Transaction, error)
	SendMoney(ctx context.Context, from string, to string, amount float64) error
}

type API struct {
	db Storage
}

func New(db Storage) *API {
	return &API{db: db}
}

func (a *API) RegisterRoutes(r *chi.Mux) {
	r.Post("/api/send", a.Send)
	r.Get("/api/transactions", a.GetLast)
	r.Get("/api/wallet/{address}/balance", a.GetBalance)
}

func (a *API) Send(w http.ResponseWriter, r *http.Request) {
	var req models.SendRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "неверный формат запроса", http.StatusBadRequest)
		return
	}

	defer r.Body.Close()

	if req.Amount < 0 {
		http.Error(w, "сумма перевода должна быть положительная", http.StatusBadRequest)
		return
	}

	if req.From == req.To {
		http.Error(w, "Нельзя отправить деньги самому себе", http.StatusBadRequest)
		return
	}

	err := a.db.SendMoney(r.Context(), req.From, req.To, req.Amount)
	if err != nil {
		log.Printf("Ошибка при переводе средств")
		if errors.Is(err, sql.ErrNoRows) ||
			err.Error() == "кошелёк отправителя не найден" ||
			err.Error() == "кошелёк получателя не найден" {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if err.Error() == "недостаточно средств на счёте" {
			http.Error(w, err.Error(), http.StatusPaymentRequired)
			return
		}

		http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (a *API) GetLast(w http.ResponseWriter, r *http.Request) {
	countStr := r.URL.Query().Get("count")
	if countStr == "" {
		countStr = "10"
	}

	count, err := strconv.Atoi(countStr)
	if err != nil || count < 0 {
		http.Error(w, "n должен быть положительным числом", http.StatusBadRequest)
		return
	}

	transactions, err := a.db.GetLastTransactions(r.Context(), count)
	if err != nil {
		http.Error(w, "внутренняя ошибка сервера -gg", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transactions)
}

func (a *API) GetBalance(w http.ResponseWriter, r *http.Request) {

	address := chi.URLParam(r, "address")

	wallet, err := a.db.GetWalletBalance(r.Context(), address)
	if err != nil {
		if err.Error() == "кошелёк не найден" {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		log.Printf("ошибка прлучения баланса: %v", err)
		http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(wallet)
}
