package domain

import "errors"

var (
	ErrUserExists         = errors.New("user with this email already exists")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserNotFound       = errors.New("user not found")
	ErrTokenNotFound      = errors.New("refresh token not found")
	ErrTokenExpired       = errors.New("refresh token expired")
	ErrInternalServer     = errors.New("internal server error")
)
