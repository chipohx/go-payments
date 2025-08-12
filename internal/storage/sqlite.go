/*
storage предоставляет собой слой для взаимодействия с базой данных SQLite.

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
*/
package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"go-payments/internal/models"
	"log"
	"time"
)

type Storage struct {
	db *sql.DB
}

func New(storagePath string) (*Storage, error) {
	db, err := sql.Open("sqlite3", storagePath)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть базу данных: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("не удалось подключиться к базе данных: %w", err)
	}

	return &Storage{db: db}, nil
}

func (s *Storage) Init(ctx context.Context) error {
	queryWallets := `
	CREATE TABLE IF NOT EXISTS wallets (
	address TEXT PRIMARY KEY,
	balance REAL NOT NULL
	);`

	if _, err := s.db.ExecContext(ctx, queryWallets); err != nil {
		return fmt.Errorf("не удалось создать таблицу wallets: %w", err)
	}

	queryTransaction := `
	CREATE TABLE IF NOT EXISTS transactions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	from_address TEXT NOT NULL, 
	to_address TEXT NOT NULL,
	amount REAL NOT NULL,
	timestamp DATETIME NOT NULL
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

			_, err := s.db.ExecContext(ctx, "INSERT INTO wallets (address, balance) VALUES (?, ?)", address, 100.0)
			if err != nil {
				return fmt.Errorf("не удалось создать кошелёк: %w", err)
			}
			log.Printf("создан кошелёк: %s с балансом 100.0\n", address)
		}
	}
	return nil
}

func (s *Storage) GetWalletBalance(ctx context.Context, address string) (*models.Wallet, error) {
	var wallet models.Wallet
	query := "SELECT address, balance FROM wallets WHERE address = ?"
	err := s.db.QueryRowContext(ctx, query, address).Scan(&wallet.Address, &wallet.Balance)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("кошелёк не найден: %w", err)
		}
		return nil, fmt.Errorf("ошибка получения баланса: %w", err)
	}
	return &wallet, nil
}

func (s *Storage) GetLastTransactions(ctx context.Context, n int) ([]models.Transaction, error) {
	query := "SELECT id, from_address, to_address, amount, timestamp FROM transactions ORDER BY timestamp DESC LIMIT ?"
	rows, err := s.db.QueryContext(ctx, query, n)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить транзакции: %w", err)
	}
	defer rows.Close()

	var transactions []models.Transaction
	for rows.Next() {
		var t models.Transaction
		if err := rows.Scan(&t.ID, &t.From, &t.To, &t.Amount, &t.Timestamp); err != nil {
			return nil, fmt.Errorf("ошибка сканирования транзакции")
		}
		transactions = append(transactions, t)
	}

	return transactions, nil
}

func (s *Storage) SendMoney(ctx context.Context, from string, to string, amount float64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("не удалось начать транзакцию: %w", err)
	}

	defer tx.Rollback()

	//логика обработки отправителя
	var senderBalance float64
	err = tx.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE address = ?", from).Scan(&senderBalance)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("не удалось найти кошелёк: %w", err)
		}
		return fmt.Errorf("ошибка получения баланса отправителя: %w", err)
	}

	if senderBalance < amount {
		return fmt.Errorf("недостаточно денег на балансе: %w")
	}

	//логика обработки получателя
	var recipientExists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM wallets WHERE address = ?", to).Scan(&recipientExists)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("кошелёк получателя не найден: %w", err)
		}
		return fmt.Errorf("ошибка проверки кошелька получателя: %w", err)
	}

	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance - ? WHERE address = ?", amount, from)
	if err != nil {
		return fmt.Errorf("ошибка списания средств: %w", err)
	}

	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance + ? WHERE address = ?", amount, to)
	if err != nil {
		return fmt.Errorf("ошибка начисления средств: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"INSERT INTO transactions (from_address, to_address, amount, timestamp) VALUES (?, ?, ?, ?)",
		from, to, amount, time.Now())
	if err != nil {
		return fmt.Errorf("ошибка сохранения транзакции: %w", err)
	}

	return tx.Commit()
}
