package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// desktopReleaseQueryOrHeader prefers the query parameter, matching the
// desktop client which sends both.
func desktopReleaseQueryOrHeader(c *gin.Context, query, header string) string {
	if value := c.Query(query); value != "" {
		return value
	}
	return c.GetHeader(header)
}

// GetDesktopReleaseManifest serves the electron-builder latest*.yml the
// desktop client's ManifestUpdateProvider requests. No release is a 404 with
// an empty body; the client counts it as one failed check.
func GetDesktopReleaseManifest(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	release, err := service.SelectDesktopRelease(
		desktopReleaseQueryOrHeader(c, "platform", "X-Platform"),
		desktopReleaseQueryOrHeader(c, "channel", "X-Release-Channel"),
		desktopReleaseQueryOrHeader(c, "device_mid", "X-Device-Mid"),
	)
	if errors.Is(err, service.ErrDesktopReleaseNotFound) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if err != nil {
		common.SysError(common.LogText("failed to select desktop release: %s", err.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	body, err := service.RenderDesktopReleaseManifest(release)
	if err != nil {
		common.SysError(common.LogText("failed to render desktop release manifest: %s", err.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, "application/x-yaml; charset=utf-8", body)
}

func desktopReleaseError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrDesktopReleaseInvalid):
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
	case errors.Is(err, model.ErrDesktopReleaseExists):
		common.ApiErrorStatus(c, http.StatusConflict, err)
	case errors.Is(err, gorm.ErrRecordNotFound):
		common.ApiErrorStatus(c, http.StatusNotFound, errors.New("desktop release not found"))
	default:
		common.SysError(common.LogText("desktop release admin request failed: %s", err.Error()))
		common.ApiErrorStatus(c, http.StatusInternalServerError, errors.New("desktop release request failed"))
	}
}

func AdminListDesktopReleases(c *gin.Context) {
	releases, err := model.ListDesktopReleases()
	if err != nil {
		desktopReleaseError(c, err)
		return
	}
	views := make([]service.DesktopReleaseView, 0, len(releases))
	for _, release := range releases {
		views = append(views, service.NewDesktopReleaseView(release))
	}
	common.ApiSuccess(c, views)
}

func AdminCreateDesktopRelease(c *gin.Context) {
	var input service.DesktopReleaseCreateInput
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("invalid desktop release request"))
		return
	}
	release, err := service.CreateDesktopRelease(input, common.GetTimestamp())
	if err != nil {
		desktopReleaseError(c, err)
		return
	}
	common.ApiSuccess(c, service.NewDesktopReleaseView(*release))
}

func AdminUpdateDesktopRelease(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("invalid desktop release id"))
		return
	}
	var input service.DesktopReleaseRolloutInput
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("invalid desktop release request"))
		return
	}
	release, err := service.UpdateDesktopReleaseRollout(id, input)
	if err != nil {
		desktopReleaseError(c, err)
		return
	}
	common.ApiSuccess(c, service.NewDesktopReleaseView(*release))
}
