// ORM models
package models

import "time"

type TransactionStatus string

const (
	StatusSuccess                 TransactionStatus = "success"
	StatusFailedInsufficientFunds TransactionStatus = "failed_insufficient_funds"
	StatusFailedRecipientNotFound TransactionStatus = "failed_recipient_not_found"
	StatusFailedSenderNotFound    TransactionStatus = "failed_sender_not_found"
	StatusUnknownError            TransactionStatus = "unknown_error"
)

type Wallet struct {
	Address string  `json:"address"`
	Balance float64 `json:"balance"`
}

type Transaction struct {
	ID        int               `json:"id"`
	From      string            `json:"from`
	To        string            `json:"to"`
	Amount    float64           `json:"amount"`
	Timestamp time.Time         `json:"timestamp"`
	Status    TransactionStatus `json: status`
}

type SendRequest struct {
	From   string  `json:"from`
	To     string  `json:"to"`
	Amount float64 `json:"amount"`
}
