package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// DesktopRelease is one desktop installer version for one platform, as served
// by GET /api/v1/releases/electron/manifest. Files are immutable after
// registration; publishing a different build means registering a new version.
type DesktopRelease struct {
	Id                   int    `json:"id"`
	Version              string `gorm:"type:varchar(64);not null;uniqueIndex:idx_desktop_release_version_platform" json:"version"`
	Platform             string `gorm:"type:varchar(32);not null;uniqueIndex:idx_desktop_release_version_platform;index" json:"platform"`
	Status               int    `gorm:"not null;index" json:"status"`
	RolloutPercentage    int    `gorm:"not null" json:"rollout_percentage"`
	RolloutDisabled      bool   `gorm:"not null" json:"rollout_disabled"`
	DeviceMidAllowlist   string `gorm:"type:text" json:"-"`
	MinimumSystemVersion string `gorm:"type:varchar(32)" json:"minimum_system_version"`
	ReleaseDate          int64  `gorm:"not null" json:"release_date"`
	ReleaseNotesByLocale string `gorm:"type:text" json:"-"`
	CreatedAt            int64  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            int64  `gorm:"autoUpdateTime" json:"updated_at"`

	Files []DesktopReleaseFile `gorm:"foreignKey:ReleaseId" json:"files"`
}

// DesktopReleaseFile is one downloadable file of a release. The first file by
// Position is the manifest's top-level path/sha512.
type DesktopReleaseFile struct {
	Id        int    `json:"-"`
	ReleaseId int    `gorm:"not null;index" json:"-"`
	Position  int    `gorm:"not null" json:"-"`
	Url       string `gorm:"type:text;not null" json:"url"`
	Sha512    string `gorm:"type:varchar(128);not null" json:"sha512"`
	Size      int64  `gorm:"not null" json:"size"`
}

// errDesktopReleaseExistsKey is the English source text of the refusal and its
// key in the web console's locale files.
const errDesktopReleaseExistsKey = "desktop release already exists"

// ErrDesktopReleaseExists is also a console message, so the web console can
// translate it. It keeps its identity for errors.Is.
var ErrDesktopReleaseExists = fmt.Errorf("%w", common.NewMessage(errDesktopReleaseExistsKey))

func preloadDesktopReleaseFiles(db *gorm.DB) *gorm.DB {
	return db.Order("position ASC")
}

// ListDesktopReleases returns every registered release, newest first.
func ListDesktopReleases() ([]DesktopRelease, error) {
	var releases []DesktopRelease
	err := DB.Preload("Files", preloadDesktopReleaseFiles).Order("created_at DESC, id DESC").Find(&releases).Error
	return releases, err
}

// ListServableDesktopReleases returns the releases of platform that are in
// rollout or fully released, with their files.
func ListServableDesktopReleases(platform string, statuses []int) ([]DesktopRelease, error) {
	var releases []DesktopRelease
	err := DB.Preload("Files", preloadDesktopReleaseFiles).
		Where("platform = ? AND status IN ?", platform, statuses).
		Find(&releases).Error
	return releases, err
}

func GetDesktopRelease(id int) (*DesktopRelease, error) {
	var release DesktopRelease
	if err := DB.Preload("Files", preloadDesktopReleaseFiles).First(&release, id).Error; err != nil {
		return nil, err
	}
	return &release, nil
}

// CreateDesktopRelease stores the release and its files in one transaction.
// A second registration of the same version and platform is rejected instead
// of replacing files that clients may already be downloading.
func CreateDesktopRelease(release *DesktopRelease) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&DesktopRelease{}).
			Where("version = ? AND platform = ?", release.Version, release.Platform).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrDesktopReleaseExists
		}
		return tx.Create(release).Error
	})
}

// UpdateDesktopReleaseRollout changes only the rollout and notes fields; files
// stay as registered.
func UpdateDesktopReleaseRollout(id int, fields map[string]any) error {
	result := DB.Model(&DesktopRelease{}).Where("id = ?", id).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
