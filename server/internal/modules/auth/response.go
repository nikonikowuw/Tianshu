package auth

import "time"

type authUserResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

type loginResponse struct {
	AccessToken string           `json:"accessToken"`
	ExpiresAt   string           `json:"expiresAt"`
	User        authUserResponse `json:"user"`
	Roles       []string         `json:"roles"`
	Permissions []string         `json:"permissions"`
}

type refreshResponse struct {
	AccessToken string `json:"accessToken"`
	ExpiresAt   string `json:"expiresAt"`
}

type meResponse struct {
	User        authUserResponse `json:"user"`
	Roles       []string         `json:"roles"`
	Permissions []string         `json:"permissions"`
}

func toAuthUserResponse(u User) authUserResponse {
	return authUserResponse{
		ID:          u.ID.String(),
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
	}
}

func toLoginResponse(res LoginResult) loginResponse {
	return loginResponse{
		AccessToken: res.AccessToken,
		ExpiresAt:   res.ExpiresAt.UTC().Format(time.RFC3339),
		User:        toAuthUserResponse(res.User),
		Roles:       nonNilStrings(res.Roles),
		Permissions: nonNilStrings(res.Permissions),
	}
}

func toRefreshResponse(res RefreshResult) refreshResponse {
	return refreshResponse{
		AccessToken: res.AccessToken,
		ExpiresAt:   res.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func toMeResponse(res MeResult) meResponse {
	return meResponse{
		User:        toAuthUserResponse(res.User),
		Roles:       nonNilStrings(res.Roles),
		Permissions: nonNilStrings(res.Permissions),
	}
}
