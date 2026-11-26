package setup

import "github.com/google/uuid"

// Protection mode constants matching config.SetupConfig.Mode.
const (
	ModeToken = "token"
	ModeLocal = "local"
	ModeOpen  = "open"
)

// Status describes whether Tianshu has completed initial administrator setup.
type Status struct {
	Initialized   bool
	Mode          string
	RequiresToken bool
}

// CreatedAdmin represents the newly created initial super administrator.
type CreatedAdmin struct {
	ID          uuid.UUID
	Username    string
	Email       string
	DisplayName string
}

// InitInput carries validated user input and client context for setup initialization.
type InitInput struct {
	SetupToken  string
	Username    string
	Email       string
	DisplayName string
	Password    string
	ClientIP    string
}
