/*
api предоставляет HTTP-интерфейс для взаимодействия с платёжной системой.
Он определяет маршруты и обработчики для выполнения основных операций, таких как
перевод средств, получение истории транзакций и проверка баланса кошелька.

Key components:
  - Storage (интерфейс): Абстракция, определяющая контракт для работы с хранилищем данных.
    Это позволяет отделить логику API от конкретной реализации базы данных,
    облегчая тестирование и замену хранилища.
  - API: Основная структура, содержащая зависимость от хранилища (Storage)
    и реализующая методы-обработчики HTTP-запросов.
  - New: Конструктор для создания нового экземпляра API.
  - RegisterRoutes: Метод для регистрации всех маршрутов API с использованием роутера chi.

Handlers:
  - Send: Обрабатывает POST-запросы на `/api/send` для перевода средств между кошельками.
    Принимает JSON-тело с адресами отправителя и получателя и суммой перевода.
    Выполняет валидацию и возвращает соответствующие HTTP-статусы.
  - GetLast: Обрабатывает GET-запросы на `/api/transactions` для получения списка
    последних транзакций. Поддерживает необязательный query-параметр `count` для
    указания количества запрашиваемых транзакций.
  - GetBalance: Обрабатывает GET-запросы на `/api/wallet/{address}/balance` для
    получения текущего баланса кошелька по его адресу.
*/
package api

import (
	"context"
	"encoding/json"
	"errors"
	"go-payments/internal/models"
	"go-payments/internal/storage"
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

	if req.Amount <= 0 {
		http.Error(w, "сумма перевода должна быть положительной", http.StatusBadRequest)
		return
	}
	if req.From == req.To {
		http.Error(w, "нельзя отправить деньги самому себе", http.StatusBadRequest)
		return
	}

	err := a.db.SendMoney(r.Context(), req.From, req.To, req.Amount)
	if err != nil {
		log.Printf("ошибка при переводе средств от %s к %s на сумму %.2f: %v", req.From, req.To, req.Amount, err)

		var txErr *storage.TransactionError
		if errors.As(err, &txErr) {
			switch txErr.Code {
			case storage.CodeSenderNotFound, storage.CodeRecipientNotFound:
				http.Error(w, txErr.Error(), http.StatusNotFound)
				return
			case storage.CodeInsufficientFunds:
				http.Error(w, txErr.Error(), http.StatusPaymentRequired) // 402 Payment Required - очень подходящий статус
				return
			default:
				http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
				return
			}
		}

		http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (a *API) GetLast(w http.ResponseWriter, r *http.Request) {
	countStr := r.URL.Query().Get("count")
	if countStr == "" {
		countStr = "10"
	}

	count, err := strconv.Atoi(countStr)
	if err != nil || count <= 0 {
		http.Error(w, "параметр 'count' должен быть положительным числом", http.StatusBadRequest)
		return
	}

	transactions, err := a.db.GetLastTransactions(r.Context(), count)
	if err != nil {
		log.Printf("ошибка получения последних транзакций: %v", err)
		http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transactions)
}

func (a *API) GetBalance(w http.ResponseWriter, r *http.Request) {
	address := chi.URLParam(r, "address")

	wallet, err := a.db.GetWalletBalance(r.Context(), address)
	if err != nil {
		if errors.Is(err, storage.ErrWalletNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		log.Printf("ошибка получения баланса для кошелька %s: %v", address, err)
		http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(wallet)
}
