package model

import (
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

var zenPlanDisplayGroupKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ZenPlanDisplay is optional presentation-only metadata for the aiscoo zen plan page.
type ZenPlanDisplay struct {
	GroupKey   string   `json:"group_key"`
	GroupTitle string   `json:"group_title"`
	OfferKind  string   `json:"offer_kind"`
	Highlights []string `json:"highlights"`
	Features   []string `json:"features"`

	invalidRaw string
	invalid    bool
}

func (display *ZenPlanDisplay) Validate() error {
	if display == nil {
		return nil
	}
	if !zenPlanDisplayGroupKey.MatchString(display.GroupKey) || strings.TrimSpace(display.GroupTitle) == "" || utf8.RuneCountInString(display.GroupTitle) > 128 {
		return errors.New("invalid aiscoo zen plan display group")
	}
	if display.OfferKind != "standard" && display.OfferKind != "trial" {
		return errors.New("invalid aiscoo zen plan display offer kind")
	}
	if len(display.Highlights) > 4 || len(display.Features) > 16 {
		return errors.New("invalid aiscoo zen plan display item count")
	}
	for _, lines := range [][]string{display.Highlights, display.Features} {
		for _, line := range lines {
			if strings.TrimSpace(line) == "" || utf8.RuneCountInString(line) > 256 {
				return errors.New("invalid aiscoo zen plan display item")
			}
		}
	}
	return nil
}

func (display *ZenPlanDisplay) IsInvalid() bool {
	return display != nil && display.invalid
}

func (display ZenPlanDisplay) Value() (driver.Value, error) {
	if display.invalid {
		return display.invalidRaw, nil
	}
	if err := display.Validate(); err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(display)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func (display ZenPlanDisplay) MarshalJSON() ([]byte, error) {
	if display.invalid {
		return []byte("null"), nil
	}
	type wire ZenPlanDisplay
	return common.Marshal(wire(display))
}

func (display *ZenPlanDisplay) Scan(value any) error {
	if value == nil {
		*display = ZenPlanDisplay{}
		return nil
	}

	var raw string
	switch value := value.(type) {
	case string:
		raw = value
	case []byte:
		raw = string(value)
	default:
		return errors.New("invalid aiscoo zen plan display database value")
	}
	var decoded ZenPlanDisplay
	if err := common.UnmarshalJsonStr(raw, &decoded); err != nil || decoded.Validate() != nil {
		// ZEN: 空字符串也是损坏记录；单独标记，避免错误元数据被当成合法配置。
		*display = ZenPlanDisplay{invalidRaw: raw, invalid: true}
		return nil
	}
	*display = decoded
	return nil
}

func (display *ZenPlanDisplay) UnmarshalJSON(data []byte) error {
	type wire ZenPlanDisplay
	var fields map[string]common.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"group_key", "group_title", "offer_kind", "highlights", "features"} {
		if _, ok := fields[key]; !ok {
			return errors.New("incomplete aiscoo zen plan display")
		}
		delete(fields, key)
	}
	if len(fields) != 0 {
		return errors.New("unknown aiscoo zen plan display field")
	}
	var decoded wire
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Highlights == nil || decoded.Features == nil {
		return errors.New("invalid aiscoo zen plan display arrays")
	}
	*display = ZenPlanDisplay(decoded)
	return display.Validate()
}
