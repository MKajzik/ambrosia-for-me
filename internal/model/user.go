package model

import "time"

type User struct {
	ID           int64     `json:"id" readOnly:"true"`
	Username     string    `json:"username" binding:"required,min=3,max=50"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at" readOnly:"true"`
	UpdatedAt    time.Time `json:"updated_at" readOnly:"true"`
}
