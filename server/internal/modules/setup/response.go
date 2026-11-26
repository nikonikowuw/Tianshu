package setup

type statusResponse struct {
	Initialized bool `json:"initialized"`
}

type adminUserResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

type initResponse struct {
	User adminUserResponse `json:"user"`
}

func toStatusResponse(st Status) statusResponse {
	return statusResponse(st)
}

func toInitResponse(admin CreatedAdmin) initResponse {
	return initResponse{
		User: adminUserResponse{
			ID:          admin.ID.String(),
			Username:    admin.Username,
			Email:       admin.Email,
			DisplayName: admin.DisplayName,
		},
	}
}
