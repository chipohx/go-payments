/*
storage предоставляет собой слой для взаимодействия с базой данных PostgreSQL.

Основной тип в этом пакете - Storage, который содержит в себе пул подключений к базе данных
и предоставляет методы для работы с ней.

Функции и методы:
  - New: Создает новый экземпляр Storage и устанавливает соединение с базой данных.
  - Init: Инициализирует базу данных, создавая необходимые таблицы (`wallets`, `transactions`).
    Если кошельки отсутствуют, создает 10 кошельков по умолчанию с начальным балансом.
  - GetWalletBalance: Возвращает информацию о кошельке (адрес и баланс) по его адресу.
  - GetLastTransactions: Получает N последних транзакций из базы данных.
  - SendMoney: Осуществляет перевод средств с одного кошелька на другой.
    Эта операция выполняется в рамках одной транзакции для обеспечения атомарности.
    Она включает в себя проверку баланса отправителя, обновление балансов обоих
    кошельков и запись информации о транзакции.
  - GetWallets: Получает N кошельков с балансом
*/
package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"go-payments/internal/models"
	"log"
	"os"
	"strconv"
	"time"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type Storage struct {
	db *sql.DB
}

//Создает новый экземпляр Storage и устанавливает соединение с базой данных.
func New() (*Storage, error) {

	_ = godotenv.Load()

	portStr := os.Getenv("POSTGRES_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port value: %s: %w", portStr, err)
	}
	host := os.Getenv("POSTGRES_HOST")
	user := os.Getenv("POSTGRES_USER")
	password := os.Getenv("POSTGRES_PASSWORD")
	dbname := os.Getenv("POSTGRES_DB")

	if host == "" || user == "" || dbname == "" || portStr == "" {
		return nil, fmt.Errorf("missing required environment variables")
	}

	connectLine := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	db, err := sql.Open("postgres", connectLine)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpenDatabase, err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnectDatabase, err)
	}

	return &Storage{db: db}, nil
}

//Инициализирует базу данных, создавая необходимые таблицы (`wallets`, `transactions`).
//Если кошельки отсутствуют, создает 10 кошельков по умолчанию с начальным балансом.
func (s *Storage) Init(ctx context.Context) error {
	queryWallets := `
    CREATE TABLE IF NOT EXISTS wallets (
        address TEXT PRIMARY KEY,
        balance DECIMAL(20, 8) NOT NULL DEFAULT 0
    );`

	if _, err := s.db.ExecContext(ctx, queryWallets); err != nil {
		return fmt.Errorf("не удалось создать таблицу wallets: %w", err)
	}

	queryTransaction := `
    CREATE TABLE IF NOT EXISTS transactions (
        id SERIAL PRIMARY KEY,
        from_address TEXT NOT NULL REFERENCES wallets(address),
        to_address TEXT NOT NULL REFERENCES wallets(address),
        amount DECIMAL(20, 8) NOT NULL,
        timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        status TEXT NOT NULL,
        CHECK (from_address <> to_address)
    );`

	if _, err := s.db.ExecContext(ctx, queryTransaction); err != nil {
		return fmt.Errorf("не удалось создать таблицу transactions: %w", err)
	}

	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM wallets;").Scan(&count)
	if err != nil {
		return fmt.Errorf("не удалось прочитать кошельки: %w", err)
	}

	if count == 0 {
		log.Println("кошельки не найдены, создаём 10 новых кошельков")
		for i := 0; i < 10; i++ {
			bytes := make([]byte, 32)
			if _, err := rand.Read(bytes); err != nil {
				return fmt.Errorf("не удалось сгенерировать адрес кошелька: %w", err)
			}
			address := hex.EncodeToString(bytes)

			_, err := s.db.ExecContext(ctx, "INSERT INTO wallets (address, balance) VALUES ($1, $2)", address, 100.0)
			if err != nil {
				return fmt.Errorf("не удалось создать кошелёк: %w", err)
			}
			log.Printf("создан кошелёк: %s с балансом 100.0\n", address)
		}
	}
	return nil
}

//Получает баланс кошелька с адрессом address
func (s *Storage) GetWalletBalance(ctx context.Context, address string) (*models.Wallet, error) {
	var wallet models.Wallet
	query := "SELECT address, balance FROM wallets WHERE address = $1"
	err := s.db.QueryRowContext(ctx, query, address).Scan(&wallet.Address, &wallet.Balance)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("ошибка получения баланса кошелька %s: %w", address, err)
	}
	return &wallet, nil
}

//Получает N адрессов с балансом
func (s *Storage) GetWallets(ctx context.Context, n int) ([]models.Wallet, error) {
	query := "SELECT address, balance FROM wallets LIMIT $1"
	rows, err := s.db.QueryContext(ctx, query, n)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить транзакции: %w", err)
	}
	defer rows.Close()

	var wallets []models.Wallet
	for rows.Next() {
		var w models.Wallet
		if err := rows.Scan(&w.Address, &w.Balance); err != nil {
			return nil, fmt.Errorf("ошибка сканирования строки wallets: %w", err)
		}
		wallets = append(wallets, w)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации по wallets: %w", err)
	}
	return wallets, nil
}

//GetLastTransactions: Получает N последних транзакций из базы данных.
func (s *Storage) GetLastTransactions(ctx context.Context, n int) ([]models.Transaction, error) {
	query := "SELECT id, from_address, to_address, amount, timestamp, status FROM transactions ORDER BY timestamp DESC LIMIT $1"
	rows, err := s.db.QueryContext(ctx, query, n)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить транзакции: %w", err)
	}
	defer rows.Close()

	var transactions []models.Transaction
	for rows.Next() {
		var t models.Transaction
		if err := rows.Scan(&t.ID, &t.From, &t.To, &t.Amount, &t.Timestamp, &t.Status); err != nil {
			return nil, fmt.Errorf("ошибка сканирования строки транзакции: %w", err)
		}
		transactions = append(transactions, t)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации по транзакциям: %w", err)
	}

	return transactions, nil
}

// Записывает транзакцию в таблицу transactions в случае ошибки.
func (s *Storage) logTransaction(ctx context.Context, from, to string, amount float64, status models.TransactionStatus) {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO transactions (from_address, to_address, amount, timestamp, status) VALUES ($1, $2, $3, $4, $5)",
		from, to, amount, time.Now(), status)
	if err != nil {
		log.Printf("ошибка: не удалось записать лог транзакции: %v", err)
	}
}

// Записывает транзакцию в таблицу transactions при успешном выполнении
func logTransactionInTx(ctx context.Context, tx *sql.Tx, from, to string, amount float64, status models.TransactionStatus) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO transactions (from_address, to_address, amount, timestamp, status) VALUES ($1, $2, $3, $4, $5)",
		from, to, amount, time.Now(), status)
	if err != nil {
		return fmt.Errorf("не удалось записать лог транзакции внутри tx: %w", err)
	}
	return nil
}

func (s *Storage) SendMoney(ctx context.Context, from string, to string, amount float64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logTransaction(ctx, from, to, amount, models.StatusUnknownError)
		return fmt.Errorf("не удалось начать транзакцию: %w", err)
	}

	// Проверка отправителя
	var senderBalance float64
	err = tx.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE address = $1", from).Scan(&senderBalance)
	if err != nil {
		tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			s.logTransaction(ctx, from, to, amount, models.StatusFailedSenderNotFound)
			return &TransactionError{Code: CodeSenderNotFound, OriginalErr: ErrWalletNotFound}
		}
		s.logTransaction(ctx, from, to, amount, models.StatusUnknownError)
		return &TransactionError{Code: CodeInternalError, OriginalErr: fmt.Errorf("ошибка получения баланса отправителя: %w", err)}
	}

	// Проверка баланса
	if senderBalance < amount {
		tx.Rollback()
		s.logTransaction(ctx, from, to, amount, models.StatusFailedInsufficientFunds)
		return &TransactionError{Code: CodeInsufficientFunds, OriginalErr: ErrInsufficientFunds}
	}

	// Проверка получателя
	var recipientExists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM wallets WHERE address = $1", to).Scan(&recipientExists)
	if err != nil {
		tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			s.logTransaction(ctx, from, to, amount, models.StatusFailedRecipientNotFound)
			return &TransactionError{Code: CodeRecipientNotFound, OriginalErr: ErrWalletNotFound}
		}
		s.logTransaction(ctx, from, to, amount, models.StatusUnknownError)
		return &TransactionError{Code: CodeInternalError, OriginalErr: fmt.Errorf("ошибка проверки кошелька получателя: %w", err)}
	}

	// Обновление балансов
	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance - $1 WHERE address = $2", amount, from)
	if err != nil {
		tx.Rollback()
		s.logTransaction(ctx, from, to, amount, models.StatusUnknownError)
		return &TransactionError{Code: CodeInternalError, OriginalErr: fmt.Errorf("ошибка списания средств: %w", err)}
	}

	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance + $1 WHERE address = $2", amount, to)
	if err != nil {
		tx.Rollback()
		s.logTransaction(ctx, from, to, amount, models.StatusUnknownError)
		return &TransactionError{Code: CodeInternalError, OriginalErr: fmt.Errorf("ошибка начисления средств: %w", err)}
	}

	// Запись успешной транзакции
	err = logTransactionInTx(ctx, tx, from, to, amount, models.StatusSuccess)
	if err != nil {
		tx.Rollback()
		return &TransactionError{Code: CodeInternalError, OriginalErr: err}
	}

	return tx.Commit()
}
