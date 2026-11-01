package controller

import (
	"bytes"
	"crypto/sha512"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

const desktopReleaseTestHost = "aiscoo-releases.example.invalid"

func setupDesktopReleaseTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.DesktopRelease{}, &model.DesktopReleaseFile{}))
	previousDB := model.DB
	model.DB = db
	t.Setenv("DESKTOP_RELEASE_ALLOWED_HOSTS", desktopReleaseTestHost)
	t.Cleanup(func() {
		model.DB = previousDB
		_ = sqlDB.Close()
	})
}

func desktopReleaseTestSha512(seed string) string {
	sum := sha512.Sum512([]byte(seed))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func desktopReleaseTestInput(version string, status int) service.DesktopReleaseCreateInput {
	return service.DesktopReleaseCreateInput{
		Version:              version,
		Platform:             "darwin-aarch64",
		Status:               status,
		RolloutPercentage:    100,
		MinimumSystemVersion: "22.0.0",
		ReleaseDate:          1793491200,
		ReleaseNotesByLocale: map[string]string{"zh-CN": "修复若干问题", "en-US": "Bug fixes"},
		Files: []service.DesktopReleaseFileInput{{
			URL:    "https://" + desktopReleaseTestHost + "/releases/aiscoo/" + version + "/macos/arm64/build/Aiscoo-" + version + "-mac-arm64.zip",
			Sha512: desktopReleaseTestSha512(version),
			Size:   262246490,
		}},
	}
}

// registerDesktopRelease registers a release and, for a stable target status,
// promotes it, because a new release can only start withdrawn or in rollout.
func registerDesktopRelease(t *testing.T, input service.DesktopReleaseCreateInput, targetStatus int) *model.DesktopRelease {
	t.Helper()
	release, err := service.CreateDesktopRelease(input, 1793491200)
	require.NoError(t, err)
	if targetStatus != input.Status {
		release, err = service.UpdateDesktopReleaseRollout(release.Id, service.DesktopReleaseRolloutInput{Status: &targetStatus})
		require.NoError(t, err)
	}
	return release
}

func requestDesktopReleaseManifest(t *testing.T, query string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/releases/electron/manifest?"+query, nil)
	for key, value := range headers {
		context.Request.Header.Set(key, value)
	}
	GetDesktopReleaseManifest(context)
	return recorder
}

// desktopReleaseClientManifest mirrors the fields the desktop
// ManifestUpdateProvider reads from the YAML body.
type desktopReleaseClientManifest struct {
	Version string `yaml:"version"`
	Files   []struct {
		URL    string `yaml:"url"`
		Sha512 string `yaml:"sha512"`
		Size   int64  `yaml:"size"`
	} `yaml:"files"`
	Path                 string            `yaml:"path"`
	Sha512               string            `yaml:"sha512"`
	ReleaseDate          string            `yaml:"releaseDate"`
	MinimumSystemVersion string            `yaml:"minimumSystemVersion"`
	ReleaseNotesByLocale map[string]string `yaml:"releaseNotesByLocale"`
}

func TestDesktopReleaseManifestServesElectronBuilderYAML(t *testing.T) {
	setupDesktopReleaseTestDB(t)
	registerDesktopRelease(t, desktopReleaseTestInput("3.18.0", service.DesktopReleaseStatusRollout), service.DesktopReleaseStatusStable)

	recorder := requestDesktopReleaseManifest(t, "platform=darwin-aarch64&channel=1", nil)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/x-yaml; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	var manifest desktopReleaseClientManifest
	require.NoError(t, yaml.Unmarshal(recorder.Body.Bytes(), &manifest))
	assert.Equal(t, "3.18.0", manifest.Version)
	require.Len(t, manifest.Files, 1)
	assert.Equal(t, manifest.Files[0].URL, manifest.Path)
	assert.Equal(t, desktopReleaseTestSha512("3.18.0"), manifest.Sha512)
	assert.Equal(t, manifest.Files[0].Sha512, manifest.Sha512)
	assert.Equal(t, int64(262246490), manifest.Files[0].Size)
	assert.Equal(t, "2026-11-01T00:00:00Z", manifest.ReleaseDate)
	assert.Equal(t, "22.0.0", manifest.MinimumSystemVersion)
	assert.Equal(t, map[string]string{"zh-CN": "修复若干问题", "en-US": "Bug fixes"}, manifest.ReleaseNotesByLocale)
}

func TestDesktopReleaseManifestSelection(t *testing.T) {
	const device = "3f2b8c1e-5d4a-4e7b-9c0d-1a2b3c4d5e6f"
	testCases := []struct {
		name        string
		query       string
		headers     map[string]string
		rollout     func(*service.DesktopReleaseCreateInput)
		wantStatus  int
		wantVersion string
	}{
		{name: "stable ignores rollout", query: "platform=darwin-aarch64&channel=1&device_mid=" + device, wantStatus: http.StatusOK, wantVersion: "3.18.0"},
		{name: "unknown channel is stable", query: "platform=darwin-aarch64&channel=9&device_mid=" + device, wantStatus: http.StatusOK, wantVersion: "3.18.0"},
		{name: "preview sees full rollout", query: "platform=darwin-aarch64&channel=3&device_mid=" + device, wantStatus: http.StatusOK, wantVersion: "3.19.0"},
		{name: "preview headers only", headers: map[string]string{"X-Platform": "darwin-aarch64", "X-Release-Channel": "3", "X-Device-Mid": device}, wantStatus: http.StatusOK, wantVersion: "3.19.0"},
		{name: "platform alias", query: "platform=darwin-arm64&channel=3&device_mid=" + device, wantStatus: http.StatusOK, wantVersion: "3.19.0"},
		{name: "preview without device id", query: "platform=darwin-aarch64&channel=3", wantStatus: http.StatusOK, wantVersion: "3.18.0"},
		{
			name:  "zero percent hides rollout",
			query: "platform=darwin-aarch64&channel=3&device_mid=" + device,
			rollout: func(input *service.DesktopReleaseCreateInput) {
				input.RolloutPercentage = 0
			},
			wantStatus: http.StatusOK, wantVersion: "3.18.0",
		},
		{
			name:  "allowlist overrides zero percent",
			query: "platform=darwin-aarch64&channel=3&device_mid=" + device,
			rollout: func(input *service.DesktopReleaseCreateInput) {
				input.RolloutPercentage = 0
				input.DeviceMidAllowlist = []string{device}
			},
			wantStatus: http.StatusOK, wantVersion: "3.19.0",
		},
		{
			name:  "disabled rollout hides allowlisted device",
			query: "platform=darwin-aarch64&channel=3&device_mid=" + device,
			rollout: func(input *service.DesktopReleaseCreateInput) {
				input.RolloutDisabled = true
				input.DeviceMidAllowlist = []string{device}
			},
			wantStatus: http.StatusOK, wantVersion: "3.18.0",
		},
		{name: "unknown platform", query: "platform=windows-x86_64&channel=1", wantStatus: http.StatusNotFound},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setupDesktopReleaseTestDB(t)
			registerDesktopRelease(t, desktopReleaseTestInput("3.18.0", service.DesktopReleaseStatusRollout), service.DesktopReleaseStatusStable)
			rollout := desktopReleaseTestInput("3.19.0", service.DesktopReleaseStatusRollout)
			if testCase.rollout != nil {
				testCase.rollout(&rollout)
			}
			registerDesktopRelease(t, rollout, service.DesktopReleaseStatusRollout)

			recorder := requestDesktopReleaseManifest(t, testCase.query, testCase.headers)

			require.Equal(t, testCase.wantStatus, recorder.Code)
			if testCase.wantVersion == "" {
				assert.Empty(t, recorder.Body.Bytes())
				return
			}
			var manifest desktopReleaseClientManifest
			require.NoError(t, yaml.Unmarshal(recorder.Body.Bytes(), &manifest))
			assert.Equal(t, testCase.wantVersion, manifest.Version)
		})
	}
}

func TestDesktopReleaseManifestOrdersBySemanticVersion(t *testing.T) {
	setupDesktopReleaseTestDB(t)
	for _, version := range []string{"3.9.0", "3.10.0-beta.2", "3.10.0", "3.10.0-beta.10"} {
		registerDesktopRelease(t, desktopReleaseTestInput(version, service.DesktopReleaseStatusRollout), service.DesktopReleaseStatusStable)
	}
	withdrawn := desktopReleaseTestInput("4.0.0", service.DesktopReleaseStatusWithdrawn)
	registerDesktopRelease(t, withdrawn, service.DesktopReleaseStatusWithdrawn)

	recorder := requestDesktopReleaseManifest(t, "platform=darwin-aarch64&channel=1", nil)

	require.Equal(t, http.StatusOK, recorder.Code)
	var manifest desktopReleaseClientManifest
	require.NoError(t, yaml.Unmarshal(recorder.Body.Bytes(), &manifest))
	assert.Equal(t, "3.10.0", manifest.Version)
}

func TestDesktopReleaseManifestEmptyIsNotFound(t *testing.T) {
	setupDesktopReleaseTestDB(t)
	registerDesktopRelease(t, desktopReleaseTestInput("3.19.0", service.DesktopReleaseStatusRollout), service.DesktopReleaseStatusRollout)

	recorder := requestDesktopReleaseManifest(t, "platform=darwin-aarch64&channel=1", nil)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Empty(t, recorder.Body.Bytes())
}

func TestDesktopReleaseRolloutBucketsAreStable(t *testing.T) {
	setupDesktopReleaseTestDB(t)
	registerDesktopRelease(t, desktopReleaseTestInput("3.18.0", service.DesktopReleaseStatusRollout), service.DesktopReleaseStatusStable)
	rollout := desktopReleaseTestInput("3.19.0", service.DesktopReleaseStatusRollout)
	rollout.RolloutPercentage = 50
	registerDesktopRelease(t, rollout, service.DesktopReleaseStatusRollout)

	seen := 0
	for i := range 200 {
		deviceMid := "device-" + strconv.Itoa(i)
		first, err := service.SelectDesktopRelease("darwin-aarch64", "3", deviceMid)
		require.NoError(t, err)
		second, err := service.SelectDesktopRelease("darwin-aarch64", "3", deviceMid)
		require.NoError(t, err)
		assert.Equal(t, first.Version, second.Version, "a device must get the same answer on every check")
		if first.Version == "3.19.0" {
			seen++
		}
	}
	// FNV-1a buckets for these 200 fixed device ids; a half rollout reaches a
	// share of them that stays well away from none or all.
	assert.Greater(t, seen, 60)
	assert.Less(t, seen, 140)
}

func TestDesktopReleaseCreateValidation(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*service.DesktopReleaseCreateInput)
	}{
		{name: "not semver", mutate: func(input *service.DesktopReleaseCreateInput) { input.Version = "3.18" }},
		{name: "unknown platform", mutate: func(input *service.DesktopReleaseCreateInput) { input.Platform = "windows-x86_64" }},
		{name: "starts as stable", mutate: func(input *service.DesktopReleaseCreateInput) { input.Status = service.DesktopReleaseStatusStable }},
		{name: "percentage over 100", mutate: func(input *service.DesktopReleaseCreateInput) { input.RolloutPercentage = 101 }},
		{name: "no files", mutate: func(input *service.DesktopReleaseCreateInput) { input.Files = nil }},
		{name: "http url", mutate: func(input *service.DesktopReleaseCreateInput) {
			input.Files[0].URL = "http://" + desktopReleaseTestHost + "/a.zip"
		}},
		{name: "host not allowed", mutate: func(input *service.DesktopReleaseCreateInput) {
			input.Files[0].URL = "https://downloads.example.invalid/a.zip"
		}},
		{name: "url with credentials", mutate: func(input *service.DesktopReleaseCreateInput) {
			input.Files[0].URL = "https://user:secret@" + desktopReleaseTestHost + "/a.zip"
		}},
		{name: "short sha512", mutate: func(input *service.DesktopReleaseCreateInput) {
			input.Files[0].Sha512 = base64.StdEncoding.EncodeToString([]byte("short"))
		}},
		{name: "zero size", mutate: func(input *service.DesktopReleaseCreateInput) { input.Files[0].Size = 0 }},
		{name: "bad minimum system version", mutate: func(input *service.DesktopReleaseCreateInput) { input.MinimumSystemVersion = "macOS 13" }},
		{name: "unsupported notes locale", mutate: func(input *service.DesktopReleaseCreateInput) {
			input.ReleaseNotesByLocale = map[string]string{"fr": "Corrections"}
		}},
		{name: "invalid allowlisted device", mutate: func(input *service.DesktopReleaseCreateInput) {
			input.DeviceMidAllowlist = []string{"not a device id"}
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setupDesktopReleaseTestDB(t)
			input := desktopReleaseTestInput("3.18.0", service.DesktopReleaseStatusRollout)
			testCase.mutate(&input)
			_, err := service.CreateDesktopRelease(input, 1793491200)
			assert.ErrorIs(t, err, service.ErrDesktopReleaseInvalid)
		})
	}
}

func TestDesktopReleaseRejectsEmptyAllowedHosts(t *testing.T) {
	setupDesktopReleaseTestDB(t)
	t.Setenv("DESKTOP_RELEASE_ALLOWED_HOSTS", "")

	_, err := service.CreateDesktopRelease(desktopReleaseTestInput("3.18.0", service.DesktopReleaseStatusRollout), 1793491200)

	assert.ErrorIs(t, err, service.ErrDesktopReleaseInvalid)
}

func TestDesktopReleaseAdminEndpoints(t *testing.T) {
	setupDesktopReleaseTestDB(t)
	input := desktopReleaseTestInput("3.18.0", service.DesktopReleaseStatusRollout)
	body, err := common.Marshal(input)
	require.NoError(t, err)

	createRecorder := httptest.NewRecorder()
	createContext, _ := gin.CreateTestContext(createRecorder)
	createContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/releases", bytes.NewReader(body))
	AdminCreateDesktopRelease(createContext)
	require.Equal(t, http.StatusOK, createRecorder.Code, createRecorder.Body.String())
	var created struct {
		Success bool                       `json:"success"`
		Data    service.DesktopReleaseView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(createRecorder.Body.Bytes(), &created))
	require.True(t, created.Success)

	duplicateRecorder := httptest.NewRecorder()
	duplicateContext, _ := gin.CreateTestContext(duplicateRecorder)
	duplicateContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/releases", bytes.NewReader(body))
	AdminCreateDesktopRelease(duplicateContext)
	assert.Equal(t, http.StatusConflict, duplicateRecorder.Code, "registered files must not be replaced")

	invalidRecorder := httptest.NewRecorder()
	invalidContext, _ := gin.CreateTestContext(invalidRecorder)
	invalidContext.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/releases/1", bytes.NewReader([]byte(`{"status":2}`)))
	invalidContext.Params = gin.Params{{Key: "id", Value: strconv.Itoa(created.Data.Id)}}
	AdminUpdateDesktopRelease(invalidContext)
	assert.Equal(t, http.StatusBadRequest, invalidRecorder.Code)

	promoteRecorder := httptest.NewRecorder()
	promoteContext, _ := gin.CreateTestContext(promoteRecorder)
	promoteContext.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/releases/1", bytes.NewReader([]byte(`{"status":1}`)))
	promoteContext.Params = gin.Params{{Key: "id", Value: strconv.Itoa(created.Data.Id)}}
	AdminUpdateDesktopRelease(promoteContext)
	require.Equal(t, http.StatusOK, promoteRecorder.Code, promoteRecorder.Body.String())

	missingRecorder := httptest.NewRecorder()
	missingContext, _ := gin.CreateTestContext(missingRecorder)
	missingContext.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/releases/999", bytes.NewReader([]byte(`{"status":1}`)))
	missingContext.Params = gin.Params{{Key: "id", Value: "999"}}
	AdminUpdateDesktopRelease(missingContext)
	assert.Equal(t, http.StatusNotFound, missingRecorder.Code)

	stable := requestDesktopReleaseManifest(t, "platform=darwin-aarch64&channel=1", nil)
	require.Equal(t, http.StatusOK, stable.Code)
	var manifest desktopReleaseClientManifest
	require.NoError(t, yaml.Unmarshal(stable.Body.Bytes(), &manifest))
	assert.Equal(t, "3.18.0", manifest.Version)
	assert.Equal(t, input.Files[0].URL, manifest.Path, "promotion must keep the registered file")

	listRecorder := httptest.NewRecorder()
	listContext, _ := gin.CreateTestContext(listRecorder)
	listContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/releases", nil)
	AdminListDesktopReleases(listContext)
	require.Equal(t, http.StatusOK, listRecorder.Code)
	var listed struct {
		Data []service.DesktopReleaseView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(listRecorder.Body.Bytes(), &listed))
	require.Len(t, listed.Data, 1)
	assert.Equal(t, service.DesktopReleaseStatusStable, listed.Data[0].Status)
	assert.Equal(t, map[string]string{"zh-CN": "修复若干问题", "en-US": "Bug fixes"}, listed.Data[0].ReleaseNotesByLocale)
}
