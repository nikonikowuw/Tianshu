package setup

type initRequest struct {
	SetupToken  string `json:"setupToken"`
	Username    string `json:"username" binding:"required,min=3,max=64"`
	Email       string `json:"email" binding:"required,email,max=255"`
	DisplayName string `json:"displayName" binding:"required,min=1,max=100"`
	Password    string `json:"password" binding:"required,min=12,max=128"`
}
