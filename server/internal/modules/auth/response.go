package auth

import (
	"time"

	"github.com/google/uuid"
)

type authUserResponse struct {
	ID           string  `json:"id"`
	Username     string  `json:"username"`
	Email        string  `json:"email"`
	DisplayName  string  `json:"displayName"`
	AvatarFileID *string `json:"avatarFileId"`
	AvatarURL    *string `json:"avatarUrl"`
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
	var avatarFileID *string
	if u.AvatarFileID != nil && *u.AvatarFileID != uuid.Nil {
		s := u.AvatarFileID.String()
		avatarFileID = &s
	}
	return authUserResponse{
		ID:           u.ID.String(),
		Username:     u.Username,
		Email:        u.Email,
		DisplayName:  u.DisplayName,
		AvatarFileID: avatarFileID,
		AvatarURL:    u.AvatarURL,
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
