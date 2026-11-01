package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// DesktopReleaseManifestRateLimit bounds the anonymous desktop update
// manifest per IP. Desktop clients poll every 15 minutes and every 2 minutes
// after an update is found, so several devices behind one office IP need far
// more than CriticalRateLimit's 20 requests per 20 minutes.
func DesktopReleaseManifestRateLimit() func(c *gin.Context) {
	if !common.GetEnvOrDefaultBool("DESKTOP_RELEASE_MANIFEST_RATE_LIMIT_ENABLE", true) {
		return defNext
	}
	return rateLimitFactory(
		common.GetEnvOrDefault("DESKTOP_RELEASE_MANIFEST_RATE_LIMIT", 120),
		int64(common.GetEnvOrDefault("DESKTOP_RELEASE_MANIFEST_RATE_LIMIT_DURATION", 180)),
		"DRM",
	)
}
