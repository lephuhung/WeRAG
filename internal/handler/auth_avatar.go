package handler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// maxAvatarSize caps an avatar upload at 5MB: avatars are tiny UI assets and
// the upload is buffered whole in memory.
const maxAvatarSize = 5 << 20

// sniffAvatarExt identifies an uploaded image by magic bytes — the declared
// multipart Content-Type is client-controlled and proves nothing. webp has no
// stdlib decoder, so it is accepted by signature only.
func sniffAvatarExt(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return ".png"
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return ".jpg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return ".gif"
	case len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return ".webp"
	default:
		return ""
	}
}

// UploadMyAvatar godoc
// @Summary      Upload avatar for the current user
// @Description  Accepts one multipart image (png/jpg/webp/gif, ≤5MB), stores it
// @Description  via the tenant's file service and records the returned
// @Description  provider path on the caller's profile. Clients render it back
// @Description  through the authenticated /files proxy.
// @Tags         Auth
// @Accept       multipart/form-data
// @Produce      json
// @Param        file  formData  file  true  "Avatar image"
// @Success      200   {object}  map[string]interface{}  "Stored avatar path"
// @Failure      400   {object}  errors.AppError         "Invalid image"
// @Failure      401   {object}  errors.AppError         "Unauthorized"
// @Failure      503   {object}  errors.AppError         "Storage not configured"
// @Security     Bearer
// @Router       /auth/me/avatar [post]
func (h *AuthHandler) UploadMyAvatar(c *gin.Context) {
	ctx := c.Request.Context()
	if h.fileService == nil {
		c.Error(errors.NewServiceUnavailableError("Avatar storage is not configured"))
		return
	}
	user, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("Failed to get user information").WithDetails(err.Error()))
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarSize+(1<<20))
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.Error(errors.NewValidationError(fmt.Sprintf("invalid avatar upload: %v", err)))
		return
	}
	if fileHeader.Size > maxAvatarSize {
		c.Error(errors.NewValidationError("avatar image must be 5MB or smaller"))
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.Error(errors.NewBadRequestError("failed to open avatar image"))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxAvatarSize+1))
	if err != nil {
		c.Error(errors.NewBadRequestError("failed to read avatar image"))
		return
	}
	if len(data) > maxAvatarSize {
		c.Error(errors.NewValidationError("avatar image must be 5MB or smaller"))
		return
	}
	ext := sniffAvatarExt(data)
	if ext == "" {
		c.Error(errors.NewValidationError("file is not a supported image (png, jpg, webp, gif)"))
		return
	}

	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	avatarName := fmt.Sprintf("avatars/%s%s", user.ID, ext)
	storedPath, err := h.fileService.SaveBytes(ctx, data, tenantID, avatarName, false)
	if err != nil {
		logger.Errorf(ctx, "Failed to store avatar for user %s: %v", user.Email, err)
		c.Error(errors.NewBadRequestError("Failed to store avatar").WithDetails(err.Error()))
		return
	}

	// Best-effort cleanup of the replaced object; keeping it harms nothing
	// but leaks storage, so never fail the request over it.
	if user.Avatar != "" && isStoredAvatarPath(user.Avatar) {
		if err := h.fileService.DeleteFile(ctx, user.Avatar); err != nil {
			logger.Warnf(ctx, "Failed to delete previous avatar %s for user %s: %v", user.Avatar, user.Email, err)
		}
	}

	user.Avatar = storedPath
	if err := h.userService.UpdateUser(ctx, user); err != nil {
		logger.Errorf(ctx, "Failed to save avatar path for user %s: %v", user.Email, err)
		c.Error(errors.NewBadRequestError("Failed to update profile").WithDetails(err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"avatar": storedPath,
		},
	})
}

// isStoredAvatarPath reports whether an existing avatar value is a storage
// path this service owns (provider:// or storage://<backend>/provider://).
// External URLs (e.g. from OIDC providers) must not be passed to DeleteFile.
func isStoredAvatarPath(avatar string) bool {
	s := avatar
	if rest, ok := strings.CutPrefix(s, "storage://"); ok {
		_, rest, _ = strings.Cut(rest, "/")
		s = rest
	}
	for _, scheme := range []string{"local://", "minio://", "cos://", "tos://", "s3://", "oss://", "ks3://", "obs://"} {
		if strings.HasPrefix(s, scheme) {
			return true
		}
	}
	return false
}
