package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gopkg.in/yaml.v3"
)

// Desktop release status values, shared with the desktop client protocol
// (docs/desktop/auto-update-service-manifest.md in the desktop repository).
const (
	DesktopReleaseStatusWithdrawn = 0
	DesktopReleaseStatusStable    = 1
	DesktopReleaseStatusRollout   = 3
)

const (
	desktopReleaseChannelStable  = "stable"
	desktopReleaseChannelPreview = "preview"

	maxDesktopReleaseAllowlist = 200
	maxDesktopReleaseFiles     = 8
	maxDesktopReleaseNotesSize = 64 * 1024
)

// desktopReleasePlatformAliases maps every accepted platform spelling to the
// canonical key the desktop client sends (getElectronReleasePlatform).
var desktopReleasePlatformAliases = map[string]string{
	"darwin-aarch64": "darwin-aarch64",
	"darwin-arm64":   "darwin-aarch64",
}

var (
	ErrDesktopReleaseInvalid  = errors.New("invalid desktop release")
	ErrDesktopReleaseNotFound = errors.New("no desktop release available")

	desktopReleaseVersionPattern       = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
	desktopReleaseSystemVersionPattern = regexp.MustCompile(`^\d+(?:\.\d+){0,2}$`)
	desktopReleaseDeviceMidPattern     = regexp.MustCompile(`^[0-9A-Za-z-]{1,64}$`)
)

func desktopReleaseInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrDesktopReleaseInvalid, fmt.Sprintf(format, args...))
}

// NormalizeDesktopReleasePlatform returns the canonical platform key, or ""
// for a platform this service does not publish.
func NormalizeDesktopReleasePlatform(platform string) string {
	return desktopReleasePlatformAliases[strings.ToLower(strings.TrimSpace(platform))]
}

// NormalizeDesktopReleaseChannel maps the client's channel value ("1" stable,
// "3" preview) to a channel name. Anything else is stable, so an unknown value
// never widens what a client can see.
func NormalizeDesktopReleaseChannel(channel string) string {
	if strings.TrimSpace(channel) == strconv.Itoa(DesktopReleaseStatusRollout) {
		return desktopReleaseChannelPreview
	}
	return desktopReleaseChannelStable
}

type desktopReleaseSemver struct {
	core       [3]int
	prerelease []string
}

func parseDesktopReleaseVersion(version string) (desktopReleaseSemver, bool) {
	match := desktopReleaseVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return desktopReleaseSemver{}, false
	}
	var parsed desktopReleaseSemver
	for i := range 3 {
		value, err := strconv.Atoi(match[i+1])
		if err != nil {
			return desktopReleaseSemver{}, false
		}
		parsed.core[i] = value
	}
	if match[4] != "" {
		parsed.prerelease = strings.Split(match[4], ".")
	}
	return parsed, true
}

// compareDesktopReleaseVersions orders versions by semantic version precedence
// (https://semver.org/#spec-item-11), the same order electron-updater uses.
func compareDesktopReleaseVersions(left, right string) int {
	a, okA := parseDesktopReleaseVersion(left)
	b, okB := parseDesktopReleaseVersion(right)
	if !okA || !okB {
		return strings.Compare(left, right)
	}
	for i := range 3 {
		if a.core[i] != b.core[i] {
			return a.core[i] - b.core[i]
		}
	}
	if len(a.prerelease) == 0 || len(b.prerelease) == 0 {
		return len(b.prerelease) - len(a.prerelease)
	}
	for i := range min(len(a.prerelease), len(b.prerelease)) {
		x, y := a.prerelease[i], b.prerelease[i]
		xNum, xErr := strconv.Atoi(x)
		yNum, yErr := strconv.Atoi(y)
		switch {
		case xErr == nil && yErr == nil:
			if xNum != yNum {
				return xNum - yNum
			}
		case xErr == nil:
			return -1
		case yErr == nil:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	return len(a.prerelease) - len(b.prerelease)
}

// desktopReleaseVisibleInRollout decides whether one device sees a rollout
// release. The bucket is a stable hash of version and device, so a device's
// answer only changes when the percentage crosses its bucket, and each version
// rolls out to a different slice of devices.
func desktopReleaseVisibleInRollout(release model.DesktopRelease, deviceMid string) bool {
	if release.RolloutDisabled || deviceMid == "" {
		return false
	}
	if slices.Contains(decodeDesktopReleaseAllowlist(release.DeviceMidAllowlist), deviceMid) {
		return true
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(release.Version + ":" + deviceMid))
	return int(hash.Sum32()%100) < release.RolloutPercentage
}

func decodeDesktopReleaseAllowlist(stored string) []string {
	if stored == "" {
		return nil
	}
	var allowlist []string
	if err := common.UnmarshalJsonStr(stored, &allowlist); err != nil {
		return nil
	}
	return allowlist
}

// SelectDesktopRelease returns the release a client should be offered, or
// ErrDesktopReleaseNotFound. Stable clients only see fully released versions;
// preview clients also see rollout versions that include their device.
func SelectDesktopRelease(platform, channel, deviceMid string) (*model.DesktopRelease, error) {
	platform = NormalizeDesktopReleasePlatform(platform)
	if platform == "" {
		return nil, ErrDesktopReleaseNotFound
	}
	deviceMid = strings.TrimSpace(deviceMid)
	if !desktopReleaseDeviceMidPattern.MatchString(deviceMid) {
		deviceMid = ""
	}
	statuses := []int{DesktopReleaseStatusStable}
	if NormalizeDesktopReleaseChannel(channel) == desktopReleaseChannelPreview {
		statuses = append(statuses, DesktopReleaseStatusRollout)
	}
	releases, err := model.ListServableDesktopReleases(platform, statuses)
	if err != nil {
		return nil, err
	}
	var best *model.DesktopRelease
	for i := range releases {
		candidate := releases[i]
		if len(candidate.Files) == 0 {
			continue
		}
		if candidate.Status == DesktopReleaseStatusRollout && !desktopReleaseVisibleInRollout(candidate, deviceMid) {
			continue
		}
		if best == nil || compareDesktopReleaseVersions(candidate.Version, best.Version) > 0 {
			best = &releases[i]
		}
	}
	if best == nil {
		return nil, ErrDesktopReleaseNotFound
	}
	return best, nil
}

type desktopManifestFile struct {
	URL    string `yaml:"url"`
	Sha512 string `yaml:"sha512"`
	Size   int64  `yaml:"size"`
}

type desktopManifest struct {
	Version              string                `yaml:"version"`
	Files                []desktopManifestFile `yaml:"files"`
	Path                 string                `yaml:"path"`
	Sha512               string                `yaml:"sha512"`
	ReleaseDate          string                `yaml:"releaseDate"`
	MinimumSystemVersion string                `yaml:"minimumSystemVersion,omitempty"`
	ReleaseNotesByLocale map[string]string     `yaml:"releaseNotesByLocale,omitempty"`
}

// RenderDesktopReleaseManifest renders the electron-builder latest*.yml shape
// the desktop ManifestUpdateProvider parses.
func RenderDesktopReleaseManifest(release *model.DesktopRelease) ([]byte, error) {
	if release == nil || len(release.Files) == 0 {
		return nil, ErrDesktopReleaseNotFound
	}
	manifest := desktopManifest{
		Version:              release.Version,
		Path:                 release.Files[0].Url,
		Sha512:               release.Files[0].Sha512,
		ReleaseDate:          time.Unix(release.ReleaseDate, 0).UTC().Format(time.RFC3339),
		MinimumSystemVersion: release.MinimumSystemVersion,
	}
	for _, file := range release.Files {
		manifest.Files = append(manifest.Files, desktopManifestFile{URL: file.Url, Sha512: file.Sha512, Size: file.Size})
	}
	if release.ReleaseNotesByLocale != "" {
		if err := common.UnmarshalJsonStr(release.ReleaseNotesByLocale, &manifest.ReleaseNotesByLocale); err != nil {
			return nil, err
		}
	}
	return yaml.Marshal(manifest)
}

// DesktopReleaseFileInput is one file of a registration request.
type DesktopReleaseFileInput struct {
	URL    string `json:"url"`
	Sha512 string `json:"sha512"`
	Size   int64  `json:"size"`
}

// DesktopReleaseCreateInput is the admin registration request.
type DesktopReleaseCreateInput struct {
	Version              string                    `json:"version"`
	Platform             string                    `json:"platform"`
	Status               int                       `json:"status"`
	RolloutPercentage    int                       `json:"rollout_percentage"`
	RolloutDisabled      bool                      `json:"rollout_disabled"`
	DeviceMidAllowlist   []string                  `json:"device_mid_allowlist"`
	MinimumSystemVersion string                    `json:"minimum_system_version"`
	ReleaseDate          int64                     `json:"release_date"`
	ReleaseNotesByLocale map[string]string         `json:"release_notes_by_locale"`
	Files                []DesktopReleaseFileInput `json:"files"`
}

// DesktopReleaseRolloutInput is the admin update request. Only present fields
// change; files and version are immutable.
type DesktopReleaseRolloutInput struct {
	Status               *int               `json:"status"`
	RolloutPercentage    *int               `json:"rollout_percentage"`
	RolloutDisabled      *bool              `json:"rollout_disabled"`
	DeviceMidAllowlist   *[]string          `json:"device_mid_allowlist"`
	ReleaseNotesByLocale *map[string]string `json:"release_notes_by_locale"`
}

// DesktopReleaseView is the admin listing shape.
type DesktopReleaseView struct {
	model.DesktopRelease
	DeviceMidAllowlist   []string          `json:"device_mid_allowlist"`
	ReleaseNotesByLocale map[string]string `json:"release_notes_by_locale"`
}

func NewDesktopReleaseView(release model.DesktopRelease) DesktopReleaseView {
	view := DesktopReleaseView{DesktopRelease: release, DeviceMidAllowlist: decodeDesktopReleaseAllowlist(release.DeviceMidAllowlist)}
	if release.ReleaseNotesByLocale != "" {
		_ = common.UnmarshalJsonStr(release.ReleaseNotesByLocale, &view.ReleaseNotesByLocale)
	}
	return view
}

// desktopReleaseAllowedHosts reads DESKTOP_RELEASE_ALLOWED_HOSTS. It is read
// on every registration so an operator can change it without a restart; an
// empty list rejects every file URL.
func desktopReleaseAllowedHosts() []string {
	var hosts []string
	for host := range strings.SplitSeq(common.GetEnvOrDefaultString("DESKTOP_RELEASE_ALLOWED_HOSTS", ""), ",") {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func validateDesktopReleaseFile(file DesktopReleaseFileInput, allowedHosts []string) error {
	parsed, err := url.Parse(strings.TrimSpace(file.URL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return desktopReleaseInvalid("file url must be an https URL without credentials")
	}
	if !slices.Contains(allowedHosts, strings.ToLower(parsed.Host)) {
		return desktopReleaseInvalid("file host %q is not in DESKTOP_RELEASE_ALLOWED_HOSTS", parsed.Host)
	}
	digest, err := base64.StdEncoding.DecodeString(file.Sha512)
	if err != nil || len(digest) != 64 {
		return desktopReleaseInvalid("sha512 must be the base64 of a 64-byte digest")
	}
	if file.Size <= 0 {
		return desktopReleaseInvalid("size must be positive")
	}
	return nil
}

func validateDesktopReleaseRollout(status, percentage int, allowlist []string) error {
	if status != DesktopReleaseStatusWithdrawn && status != DesktopReleaseStatusStable && status != DesktopReleaseStatusRollout {
		return desktopReleaseInvalid("status must be 0, 1 or 3")
	}
	if percentage < 0 || percentage > 100 {
		return desktopReleaseInvalid("rollout_percentage must be between 0 and 100")
	}
	if len(allowlist) > maxDesktopReleaseAllowlist {
		return desktopReleaseInvalid("device_mid_allowlist allows at most %d devices", maxDesktopReleaseAllowlist)
	}
	for _, deviceMid := range allowlist {
		if !desktopReleaseDeviceMidPattern.MatchString(deviceMid) {
			return desktopReleaseInvalid("device_mid_allowlist contains an invalid device id")
		}
	}
	return nil
}

func encodeDesktopReleaseNotes(notes map[string]string) (string, error) {
	if len(notes) == 0 {
		return "", nil
	}
	for locale := range notes {
		if locale != "zh-CN" && locale != "en-US" {
			return "", desktopReleaseInvalid("release notes locale must be zh-CN or en-US")
		}
	}
	encoded, err := common.Marshal(notes)
	if err != nil {
		return "", err
	}
	if len(encoded) > maxDesktopReleaseNotesSize {
		return "", desktopReleaseInvalid("release notes are too large")
	}
	return string(encoded), nil
}

func encodeDesktopReleaseAllowlist(allowlist []string) (string, error) {
	if len(allowlist) == 0 {
		return "", nil
	}
	encoded, err := common.Marshal(allowlist)
	return string(encoded), err
}

// CreateDesktopRelease validates and stores a new release. A release starts
// withdrawn or in rollout; full release is a separate, deliberate update.
func CreateDesktopRelease(input DesktopReleaseCreateInput, now int64) (*model.DesktopRelease, error) {
	version := strings.TrimSpace(input.Version)
	if _, ok := parseDesktopReleaseVersion(version); !ok {
		return nil, desktopReleaseInvalid("version must be a semantic version")
	}
	platform := NormalizeDesktopReleasePlatform(input.Platform)
	if platform == "" {
		return nil, desktopReleaseInvalid("unsupported platform")
	}
	if input.Status != DesktopReleaseStatusWithdrawn && input.Status != DesktopReleaseStatusRollout {
		return nil, desktopReleaseInvalid("a new release must start with status 0 or 3")
	}
	if err := validateDesktopReleaseRollout(input.Status, input.RolloutPercentage, input.DeviceMidAllowlist); err != nil {
		return nil, err
	}
	minimumSystemVersion := strings.TrimSpace(input.MinimumSystemVersion)
	if minimumSystemVersion != "" && !desktopReleaseSystemVersionPattern.MatchString(minimumSystemVersion) {
		return nil, desktopReleaseInvalid("minimum_system_version must be dotted numbers")
	}
	if len(input.Files) == 0 || len(input.Files) > maxDesktopReleaseFiles {
		return nil, desktopReleaseInvalid("a release needs between 1 and %d files", maxDesktopReleaseFiles)
	}
	allowedHosts := desktopReleaseAllowedHosts()
	release := &model.DesktopRelease{
		Version:              version,
		Platform:             platform,
		Status:               input.Status,
		RolloutPercentage:    input.RolloutPercentage,
		RolloutDisabled:      input.RolloutDisabled,
		MinimumSystemVersion: minimumSystemVersion,
		ReleaseDate:          input.ReleaseDate,
	}
	if release.ReleaseDate <= 0 {
		release.ReleaseDate = now
	}
	for position, file := range input.Files {
		if err := validateDesktopReleaseFile(file, allowedHosts); err != nil {
			return nil, err
		}
		release.Files = append(release.Files, model.DesktopReleaseFile{
			Position: position,
			Url:      strings.TrimSpace(file.URL),
			Sha512:   file.Sha512,
			Size:     file.Size,
		})
	}
	var err error
	if release.DeviceMidAllowlist, err = encodeDesktopReleaseAllowlist(input.DeviceMidAllowlist); err != nil {
		return nil, err
	}
	if release.ReleaseNotesByLocale, err = encodeDesktopReleaseNotes(input.ReleaseNotesByLocale); err != nil {
		return nil, err
	}
	if err := model.CreateDesktopRelease(release); err != nil {
		return nil, err
	}
	return release, nil
}

// UpdateDesktopReleaseRollout applies an admin rollout change and returns the
// stored release.
func UpdateDesktopReleaseRollout(id int, input DesktopReleaseRolloutInput) (*model.DesktopRelease, error) {
	current, err := model.GetDesktopRelease(id)
	if err != nil {
		return nil, err
	}
	status, percentage := current.Status, current.RolloutPercentage
	allowlist := decodeDesktopReleaseAllowlist(current.DeviceMidAllowlist)
	fields := map[string]any{}
	if input.Status != nil {
		status = *input.Status
		fields["status"] = status
	}
	if input.RolloutPercentage != nil {
		percentage = *input.RolloutPercentage
		fields["rollout_percentage"] = percentage
	}
	if input.RolloutDisabled != nil {
		fields["rollout_disabled"] = *input.RolloutDisabled
	}
	if input.DeviceMidAllowlist != nil {
		allowlist = *input.DeviceMidAllowlist
		encoded, err := encodeDesktopReleaseAllowlist(allowlist)
		if err != nil {
			return nil, err
		}
		fields["device_mid_allowlist"] = encoded
	}
	if input.ReleaseNotesByLocale != nil {
		encoded, err := encodeDesktopReleaseNotes(*input.ReleaseNotesByLocale)
		if err != nil {
			return nil, err
		}
		fields["release_notes_by_locale"] = encoded
	}
	if err := validateDesktopReleaseRollout(status, percentage, allowlist); err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return current, nil
	}
	if err := model.UpdateDesktopReleaseRollout(id, fields); err != nil {
		return nil, err
	}
	return model.GetDesktopRelease(id)
}
