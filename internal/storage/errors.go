package storage

import (
	"errors"
	"fmt"
)

// Используются для простых, бинарных проверок с помощью errors.Is()
var (
	ErrWalletNotFound    = errors.New("кошелёк не найден")
	ErrInsufficientFunds = errors.New("недостаточно средств на балансе")
	ErrOpenDatabase      = errors.New("не удалось открыть базу данных")
	ErrConnectDatabase   = errors.New("не удалось подключиться к базе данных")
)

// Используются для передачи дополнительного контекста об ошибке с помощью errors.As()
// Коды ошибок для TransactionError, чтобы вызывающий код мог легко их различить.
type TxErrCode int

const (
	CodeUnknown TxErrCode = iota
	CodeSenderNotFound
	CodeRecipientNotFound
	CodeInsufficientFunds
	CodeInternalError
)

// TransactionError инкапсулирует любую ошибку, произошедшую во время выполнения SendMoney.
type TransactionError struct {
	Code        TxErrCode
	OriginalErr error
}

// для совместимости с интерфейсом error.
func (e *TransactionError) Error() string {
	switch e.Code {
	case CodeSenderNotFound:
		return "кошелёк отправителя не найден"
	case CodeRecipientNotFound:
		return "кошелёк получателя не найден"
	case CodeInsufficientFunds:
		return ErrInsufficientFunds.Error() // Используем текст из сигнальной ошибки
	case CodeInternalError:
		return fmt.Sprintf("внутренняя ошибка транзакции: %v", e.OriginalErr)
	default:
		return fmt.Sprintf("неизвестная ошибка транзакции: %v", e.OriginalErr)
	}
}

// Unwrap позволяет errors.Is и errors.As "заглянуть" в исходную ошибку.
func (e *TransactionError) Unwrap() error {
	return e.OriginalErr
}
