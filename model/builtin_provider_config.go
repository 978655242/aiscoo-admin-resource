package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/builtinconfig"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const builtinOptionPrefix = "builtin_provider_config."
const builtinStateKey = builtinOptionPrefix + "state"
const builtinArchiveKey = builtinOptionPrefix + "legacy_archive"

var (
	ErrBuiltinConfigConflict = errors.New("Built-in configuration changed; reload before saving or publishing")
	ErrBuiltinConfigInvalid  = errors.New("invalid Built-in configuration")
	builtinMutationMu        sync.Mutex
	builtinDigestPattern     = regexp.MustCompile(`^[a-f0-9]{64}$`)
	builtinReleaseCache      struct {
		sync.Mutex
		digest  string
		release *builtinconfig.Release
	}
)

func IsBuiltinProviderOption(key string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), builtinOptionPrefix)
}

type BuiltinProviderSnapshot struct {
	Version           int64    `json:"version"`
	DraftJSON         string   `json:"draft_json"`
	PublishedRevision *int64   `json:"published_revision"`
	PublishedURL      *string  `json:"published_url"`
	MigrationIssues   []string `json:"migration_issues"`
	PublishedHash     string   `json:"-"`
}

type builtinProviderState struct {
	Version           int64    `json:"version"`
	DraftJSON         string   `json:"draft_json"`
	PublishedRevision *int64   `json:"published_revision"`
	PublishedHash     string   `json:"published_hash"`
	MigrationIssues   []string `json:"migration_issues"`
}

func (s builtinProviderState) snapshot() *BuiltinProviderSnapshot {
	issues := slices.Clone(s.MigrationIssues)
	if issues == nil {
		issues = []string{}
	}
	return &BuiltinProviderSnapshot{Version: s.Version, DraftJSON: s.DraftJSON, PublishedRevision: s.PublishedRevision, MigrationIssues: issues, PublishedHash: s.PublishedHash}
}

func readBuiltinProviderState(tx *gorm.DB, locked bool) (builtinProviderState, string, error) {
	var row Option
	query := tx.Where(commonKeyCol+" = ?", builtinStateKey)
	if locked {
		if tx.Dialector.Name() == "sqlite" {
			// SQLite 的延迟事务若先读再写，多进程会竞争快照升级；先取得写锁，
			// 再读取 expected_version，保持与初始化相同的串行 CAS 边界。
			if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", builtinStateKey).UpdateColumn("value", gorm.Expr("value")).Error; err != nil {
				return builtinProviderState{}, "", err
			}
		}
		query = lockForUpdate(query)
	}
	if err := query.First(&row).Error; err != nil {
		return builtinProviderState{}, "", err
	}
	var state builtinProviderState
	if err := common.UnmarshalJsonStr(row.Value, &state); err != nil {
		return state, "", err
	}
	if state.Version <= 0 || state.Version > builtinconfig.MaxSafeInteger || (state.PublishedRevision != nil && (*state.PublishedRevision <= 43 || *state.PublishedRevision > builtinconfig.MaxSafeInteger || !builtinDigestPattern.MatchString(state.PublishedHash))) {
		return state, "", errors.New("invalid persisted Built-in state")
	}
	return state, row.Value, nil
}

func GetBuiltinProviderConfig() (*BuiltinProviderSnapshot, error) {
	state, _, err := readBuiltinProviderState(DB, false)
	if err != nil {
		return nil, err
	}
	return state.snapshot(), nil
}

func SaveBuiltinProviderConfig(expected int64, draft string) (*BuiltinProviderSnapshot, error) {
	if expected <= 0 || expected > builtinconfig.MaxSafeInteger {
		return nil, fmt.Errorf("%w: expected_version must be a positive safe integer", ErrBuiltinConfigInvalid)
	}
	if _, err := builtinconfig.Parse(draft); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBuiltinConfigInvalid, err)
	}
	builtinMutationMu.Lock()
	defer builtinMutationMu.Unlock()
	var snapshot *BuiltinProviderSnapshot
	err := DB.Transaction(func(tx *gorm.DB) error {
		state, previous, err := readBuiltinProviderState(tx, true)
		if err != nil {
			return err
		}
		if state.Version != expected {
			return ErrBuiltinConfigConflict
		}
		if state.Version == builtinconfig.MaxSafeInteger {
			return fmt.Errorf("%w: version exhausted", ErrBuiltinConfigInvalid)
		}
		state.Version++
		state.DraftJSON = draft
		if err := writeBuiltinProviderState(tx, previous, state); err != nil {
			return err
		}
		snapshot = state.snapshot()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func PublishBuiltinProviderConfig(expected int64) (*BuiltinProviderSnapshot, error) {
	if expected <= 0 || expected > builtinconfig.MaxSafeInteger {
		return nil, fmt.Errorf("%w: expected_version must be a positive safe integer", ErrBuiltinConfigInvalid)
	}
	builtinMutationMu.Lock()
	defer builtinMutationMu.Unlock()
	var snapshot *BuiltinProviderSnapshot
	err := DB.Transaction(func(tx *gorm.DB) error {
		state, previous, err := readBuiltinProviderState(tx, true)
		if err != nil {
			return err
		}
		if state.Version != expected {
			return ErrBuiltinConfigConflict
		}
		if state.Version == builtinconfig.MaxSafeInteger {
			return fmt.Errorf("%w: version exhausted", ErrBuiltinConfigInvalid)
		}
		release, err := builtinconfig.Parse(state.DraftJSON)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBuiltinConfigInvalid, err)
		}
		last := int64(43)
		if state.PublishedRevision != nil {
			last = *state.PublishedRevision
		}
		if last == builtinconfig.MaxSafeInteger {
			return fmt.Errorf("%w: revision exhausted", ErrBuiltinConfigInvalid)
		}
		release.Revision = max(time.Now().UnixMilli(), last+1, release.Revision)
		if release.Revision > builtinconfig.MaxSafeInteger {
			return fmt.Errorf("%w: revision exceeds safe integer boundary", ErrBuiltinConfigInvalid)
		}
		body, err := common.Marshal(release)
		if err != nil {
			return err
		}
		digest, err := persistBuiltinRelease(tx, string(body))
		if err != nil {
			return err
		}
		state.Version++
		state.PublishedRevision = &release.Revision
		state.PublishedHash = digest
		if err := writeBuiltinProviderState(tx, previous, state); err != nil {
			return err
		}
		snapshot = state.snapshot()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func writeBuiltinProviderState(tx *gorm.DB, previous string, state builtinProviderState) error {
	encoded, err := common.Marshal(state)
	if err != nil {
		return err
	}
	// 中文原因：Options 的单项更新忽略数据库错误；中央状态必须用事务内 CAS，
	// 防止并发保存/发布覆盖，任一步失败均不发布指针或内存状态。
	result := tx.Model(&Option{}).Where(commonKeyCol+" = ? AND value = ?", builtinStateKey, previous).Update("value", string(encoded))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrBuiltinConfigConflict
	}
	return nil
}

func persistBuiltinRelease(tx *gorm.DB, body string) (string, error) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	row := Option{Key: builtinOptionPrefix + "release." + digest, Value: body}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return "", err
	}
	var existing Option
	if err := tx.Where(commonKeyCol+" = ?", row.Key).First(&existing).Error; err != nil {
		return "", err
	}
	if existing.Value != body {
		return "", errors.New("immutable Built-in release digest collision")
	}
	return digest, nil
}

func GetBuiltinProviderRelease(digest string) (string, error) {
	if !builtinDigestPattern.MatchString(digest) {
		return "", gorm.ErrRecordNotFound
	}
	var row Option
	if err := DB.Where(commonKeyCol+" = ?", builtinOptionPrefix+"release."+digest).First(&row).Error; err != nil {
		return "", err
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(row.Value))) != digest {
		return "", errors.New("Built-in release integrity check failed")
	}
	return row.Value, nil
}

// GetPublishedBuiltinProviderConfig always reads the authoritative DB pointer.
// Only its immutable, validated body is cached; a stale process cannot authorize
// models using a previously published mutable configuration.
func GetPublishedBuiltinProviderConfig() (*builtinconfig.Release, string, error) {
	state, _, err := readBuiltinProviderState(DB, false)
	if err != nil {
		return nil, "", err
	}
	if state.PublishedRevision == nil {
		return nil, "", errors.New("Built-in configuration has not been published")
	}
	builtinReleaseCache.Lock()
	defer builtinReleaseCache.Unlock()
	if builtinReleaseCache.digest == state.PublishedHash && builtinReleaseCache.release != nil {
		return builtinReleaseCache.release, state.PublishedHash, nil
	}
	body, err := GetBuiltinProviderRelease(state.PublishedHash)
	if err != nil {
		return nil, "", err
	}
	release, err := builtinconfig.Parse(body)
	if err != nil {
		return nil, "", err
	}
	if release.Revision != *state.PublishedRevision {
		return nil, "", errors.New("Built-in publication pointer does not match release revision")
	}
	builtinReleaseCache.digest, builtinReleaseCache.release = state.PublishedHash, release
	return release, state.PublishedHash, nil
}

type archivedBuiltinModel struct {
	ID        int    `json:"id"`
	ModelName string `json:"model_name"`
	NameRule  int    `json:"name_rule"`
	Original  string `json:"original"`
	APIType   string `json:"api_type,omitempty"`
}

// InitializeBuiltinProviderConfig is safe on fresh and upgraded databases and
// under concurrent node startup. The empty anchor insert is the first write, so
// SQLite acquires its writer lock before any snapshot is read. FOR UPDATE uses
// the shared dialect-aware helper for MySQL and PostgreSQL.
func InitializeBuiltinProviderConfig(db *gorm.DB) error {
	builtinMutationMu.Lock()
	defer builtinMutationMu.Unlock()
	return db.Transaction(func(tx *gorm.DB) error {
		anchor := Option{Key: builtinStateKey, Value: ""}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&anchor).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where(commonKeyCol+" = ?", builtinStateKey).First(&anchor).Error; err != nil {
			return err
		}
		if anchor.Value != "" {
			_, _, err := readBuiltinProviderState(tx, false)
			return err
		}
		release, err := builtinconfig.Parse(string(builtinconfig.EmbeddedRelease()))
		if err != nil {
			return fmt.Errorf("invalid bundled Built-in: %w", err)
		}
		var rows []Model
		if err := tx.Unscoped().Select("id", "model_name", "name_rule", "x_code_config", "deleted_at").Where("x_code_config <> ?", "").Order("id").Find(&rows).Error; err != nil {
			return err
		}
		archive := make([]archivedBuiltinModel, 0, len(rows))
		issues := []string{}
		seenModels := map[string]int{}
		for _, row := range rows {
			entry := archivedBuiltinModel{ID: row.Id, ModelName: row.ModelName, NameRule: row.NameRule, Original: string(row.ZenConfig)}
			parsed, parseErr := ParseZenModelMetadata(entry.Original)
			if parseErr == nil {
				entry.APIType = parsed.APIType
			}
			archive = append(archive, entry)
			if row.DeletedAt.Valid {
				continue
			}
			if row.NameRule == NameRuleExact {
				if previous, duplicate := seenModels[row.ModelName]; duplicate {
					issues = append(issues, fmt.Sprintf("models %d and %d (%s): conflicting historical exact declarations; originals retained in archive", previous, row.Id, row.ModelName))
					for _, apiType := range []string{dto.ZenAPITypeAnthropicMessages, dto.ZenAPITypeOpenAIChatCompletions, dto.ZenAPITypeOpenAIResponses} {
						release.Config.ModelConfigRules.BuiltinProviderModelRules = append(release.Config.ModelConfigRules.BuiltinProviderModelRules, map[string]any{"providerId": "account:new-api-" + apiType, "modelId": row.ModelName, "config": map[string]any{"enabled": false}})
					}
					continue
				}
				seenModels[row.ModelName] = row.Id
			}
			if row.NameRule != NameRuleExact || parseErr != nil {
				issues = append(issues, fmt.Sprintf("model %d (%s): invalid or non-exact historical xcode_config; original retained in archive", row.Id, row.ModelName))
				if row.NameRule == NameRuleExact {
					for _, apiType := range []string{dto.ZenAPITypeAnthropicMessages, dto.ZenAPITypeOpenAIChatCompletions, dto.ZenAPITypeOpenAIResponses} {
						release.Config.ModelConfigRules.BuiltinProviderModelRules = append(release.Config.ModelConfigRules.BuiltinProviderModelRules, map[string]any{"providerId": "account:new-api-" + apiType, "modelId": row.ModelName, "config": map[string]any{"enabled": false}})
					}
				}
				continue
			}
			encoded, err := common.Marshal(parsed.ModelConfig)
			if err != nil {
				return err
			}
			var config map[string]any
			if err := common.Unmarshal(encoded, &config); err != nil {
				return err
			}
			rule := map[string]any{"providerId": "account:new-api-" + parsed.APIType, "modelId": row.ModelName, "config": config}
			if err := builtinconfig.ValidateExactModelRule(rule); err != nil {
				// 旧结构校验不检查秘密字段；不能让一条历史坏记录阻断全部初始化，
				// 或将其秘密放进匿名 Release。原文只留归档并停用该精确声明。
				issues = append(issues, fmt.Sprintf("model %d (%s): historical configuration fails publication validation; original retained in archive", row.Id, row.ModelName))
				rule["config"] = map[string]any{"enabled": false}
				if err := builtinconfig.ValidateExactModelRule(rule); err != nil {
					return err
				}
			}
			// 旧规则完整投影为最后一层精确规则，保留 false、数组顺序和 map 原文。
			release.Config.ModelConfigRules.BuiltinProviderModelRules = append(release.Config.ModelConfigRules.BuiltinProviderModelRules, rule)
		}
		release.Revision = max(time.Now().UnixMilli(), int64(44), release.Revision)
		body, err := common.Marshal(release)
		if err != nil {
			return err
		}
		if _, err := builtinconfig.Parse(string(body)); err != nil {
			return fmt.Errorf("invalid migrated Built-in: %w", err)
		}
		digest, err := persistBuiltinRelease(tx, string(body))
		if err != nil {
			return err
		}
		archived, err := common.Marshal(archive)
		if err != nil {
			return err
		}
		if err := tx.Create(&Option{Key: builtinArchiveKey, Value: string(archived)}).Error; err != nil {
			return err
		}
		return writeBuiltinProviderState(tx, "", builtinProviderState{Version: 1, DraftJSON: string(body), PublishedRevision: &release.Revision, PublishedHash: digest, MigrationIssues: issues})
	})
}
