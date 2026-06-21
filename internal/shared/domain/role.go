package domain

// Role represents a user role in the system.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleEO       Role = "eo"
	RoleCustomer Role = "customer"
)
