package models

import "time"

type Task struct {
	ID            int           `json:"id"`
	UserID        int           `json:"user_id"`
	Arg1          float64       `json:"arg1"`
	Arg2          float64       `json:"arg2"`
	Operation     string        `json:"operation"`
	OperationTime time.Duration `json:"operation_time"`
	Status        string        `json:"status"`
}

type Expression struct {
	ID         int     `json:"id"`
	UserID     int     `json:"user_id"`
	Expression string  `json:"expression"`
	Status     string  `json:"status"`
	Result     float64 `json:"result"`
}

type User struct {
	ID           int    `json:"id"`
	Login        string `json:"login"`
	PasswordHash string `json:"-"`
}

type AuthRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}
