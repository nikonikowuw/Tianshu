package auth

type loginRequest struct {
	Identifier string `json:"identifier" binding:"required,min=1,max=255"`
	Password   string `json:"password" binding:"required,min=1,max=128"`
}
