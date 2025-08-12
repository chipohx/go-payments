// ORM models
package models

import "time"

type Wallet struct {
	Address string  `json:"address"`
	Balance float64 `json:"balance"`
}

type Transaction struct {
	ID        int       `json:"id"`
	From      string    `json:"from`
	To        string    `json:"to"`
	Amount    float64   `json:"amount"`
	Timestamp time.Time `json:"timestamp"`
}

type SendRequest struct {
	From   string  `json:"from`
	To     string  `json:"to"`
	Amount float64 `json:"amount"`
}
