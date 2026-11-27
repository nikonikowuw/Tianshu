package files

import (
	"time"

	"tianshu/internal/shared/pagination"
)

type uploaderDTO struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type fileDTO struct {
	ID            string       `json:"id"`
	OriginalName  string       `json:"originalName"`
	MIMEType      string       `json:"mimeType"`
	Category      string       `json:"category"`
	SizeBytes     int64        `json:"sizeBytes"`
	SHA256        string       `json:"sha256"`
	StorageDriver string       `json:"storageDriver"`
	IsImage       bool         `json:"isImage"`
	Uploader      *uploaderDTO `json:"uploader"`
	CreatedAt     string       `json:"createdAt"`
	UpdatedAt     string       `json:"updatedAt"`
}

type signedURLDTO struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expiresAt"`
}

type uploadPolicyDTO struct {
	MaxFileSizeBytes   int64    `json:"maxFileSizeBytes"`
	MaxAvatarSizeBytes int64    `json:"maxAvatarSizeBytes"`
	AllowedExtensions  []string `json:"allowedExtensions"`
}

func toFileDTO(f File) fileDTO {
	var uploader *uploaderDTO
	if f.Uploader != nil {
		uploader = &uploaderDTO{
			ID:       f.Uploader.ID.String(),
			Username: f.Uploader.Username,
		}
	}
	return fileDTO{
		ID:            f.ID.String(),
		OriginalName:  f.OriginalName,
		MIMEType:      f.MIMEType,
		Category:      f.Category(),
		SizeBytes:     f.SizeBytes,
		SHA256:        f.SHA256,
		StorageDriver: f.StorageDriver,
		IsImage:       f.IsImage(),
		Uploader:      uploader,
		CreatedAt:     f.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     f.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toFileListResponse(res pagination.Result[File]) pagination.Result[fileDTO] {
	items := make([]fileDTO, 0, len(res.Items))
	for _, f := range res.Items {
		items = append(items, toFileDTO(f))
	}
	return pagination.Result[fileDTO]{
		Items:    items,
		Total:    res.Total,
		Page:     res.Page,
		PageSize: res.PageSize,
	}
}

func toSignedURLDTO(res SignedURLResult) signedURLDTO {
	return signedURLDTO{
		URL:       res.URL,
		ExpiresAt: res.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func toUploadPolicyDTO(p UploadPolicy) uploadPolicyDTO {
	extensions := p.AllowedExtensions
	if extensions == nil {
		extensions = make([]string, 0)
	}
	return uploadPolicyDTO{
		MaxFileSizeBytes:   p.MaxFileSizeBytes,
		MaxAvatarSizeBytes: p.MaxAvatarSizeBytes,
		AllowedExtensions:  extensions,
	}
}
