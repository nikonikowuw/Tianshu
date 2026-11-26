package setup

import "github.com/google/uuid"

// Status describes whether Tianshu has completed initial administrator setup.
type Status struct {
	Initialized bool
}

// CreatedAdmin represents the newly created initial super administrator.
type CreatedAdmin struct {
	ID          uuid.UUID
	Username    string
	Email       string
	DisplayName string
}

// InitInput carries validated user input and request context for initial administrator creation.
type InitInput struct {
	Username    string
	Email       string
	DisplayName string
	Password    string
	ClientIP    string
	UserAgent   string
	Method      string
}
