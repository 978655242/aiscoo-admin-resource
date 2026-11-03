package controller

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/builtinconfig"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func builtinPublicationURL(origin, digest string) string {
	return origin + "/api/v1/client/builtin-provider-config/" + digest + ".json"
}

func writeBuiltinSnapshot(c *gin.Context, snapshot *model.BuiltinProviderSnapshot, origin string) {
	if snapshot.PublishedRevision != nil {
		value := builtinPublicationURL(origin, snapshot.PublishedHash)
		snapshot.PublishedURL = &value
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, snapshot)
}

func writeBuiltinConfigError(c *gin.Context, err error) {
	status := http.StatusServiceUnavailable
	if errors.Is(err, model.ErrBuiltinConfigConflict) {
		status = http.StatusConflict
	}
	if errors.Is(err, model.ErrBuiltinConfigInvalid) {
		status = http.StatusBadRequest
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
}

func builtinAdminOrigin(c *gin.Context) (string, bool) {
	c.Header("Cache-Control", "no-store")
	if c.GetInt("role") != common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Only a super administrator can manage Built-in provider configuration"})
		return "", false
	}
	origin, err := common.GetZenPublicOrigin()
	if err != nil {
		writeBuiltinConfigError(c, err)
		return "", false
	}
	return origin, true
}

func GetBuiltinProviderConfig(c *gin.Context) {
	origin, ok := builtinAdminOrigin(c)
	if !ok {
		return
	}
	snapshot, err := model.GetBuiltinProviderConfig()
	if err != nil {
		writeBuiltinConfigError(c, err)
		return
	}
	writeBuiltinSnapshot(c, snapshot, origin)
}

func SaveBuiltinProviderConfig(c *gin.Context) {
	origin, ok := builtinAdminOrigin(c)
	if !ok {
		return
	}
	var request struct {
		ExpectedVersion *int64  `json:"expected_version"`
		DraftJSON       *string `json:"draft_json"`
	}
	// JSON string escaping may expand one draft byte to six request bytes.
	const maxRequestBytes = 6*builtinconfig.MaxBytes + 4096
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxRequestBytes+1))
	if err != nil || len(body) > maxRequestBytes || common.Unmarshal(body, &request) != nil || request.ExpectedVersion == nil || request.DraftJSON == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid Built-in configuration request"})
		return
	}
	snapshot, err := model.SaveBuiltinProviderConfig(*request.ExpectedVersion, *request.DraftJSON)
	if err != nil {
		writeBuiltinConfigError(c, err)
		return
	}
	recordManageAudit(c, "builtin_provider_config.save", map[string]any{"version": snapshot.Version})
	writeBuiltinSnapshot(c, snapshot, origin)
}

func PublishBuiltinProviderConfig(c *gin.Context) {
	origin, ok := builtinAdminOrigin(c)
	if !ok {
		return
	}
	var request struct {
		ExpectedVersion *int64 `json:"expected_version"`
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
	if err != nil || len(body) > 4096 || common.Unmarshal(body, &request) != nil || request.ExpectedVersion == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid Built-in configuration request"})
		return
	}
	snapshot, err := model.PublishBuiltinProviderConfig(*request.ExpectedVersion)
	if err != nil {
		writeBuiltinConfigError(c, err)
		return
	}
	recordManageAudit(c, "builtin_provider_config.publish", map[string]any{"version": snapshot.Version, "revision": snapshot.PublishedRevision, "digest": snapshot.PublishedHash})
	writeBuiltinSnapshot(c, snapshot, origin)
}

// PublicClientConfigs intentionally uses the upstream discovery envelope and
// has no account/token authentication middleware. Never derive URLs from Host.
func PublicClientConfigs(c *gin.Context) {
	c.Header("Cache-Control", "no-cache")
	origin, err := common.GetZenPublicOrigin()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 1, "message": "Client configuration origin is unavailable"})
		return
	}
	snapshot, err := model.GetBuiltinProviderConfig()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 1, "message": "Client configuration is unavailable"})
		return
	}
	configs := map[string]string{}
	if snapshot.PublishedRevision != nil {
		configs["builtin_provider_config_json"] = builtinPublicationURL(origin, snapshot.PublishedHash)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"configs": configs}})
}

func PublicBuiltinProviderConfig(c *gin.Context) {
	digest, ok := strings.CutSuffix(c.Param("digest"), ".json")
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	body, err := model.GetBuiltinProviderRelease(digest)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", `"`+digest+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	if c.GetHeader("If-None-Match") == `"`+digest+`"` {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(body))
}
