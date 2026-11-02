package model

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"

	"gorm.io/gorm"
)

const (
	ClientPurposeGeneral   = "general"
	ClientPurposeDashboard = "dashboard"
	ClientPurposeZen       = "xcode"

	SubscriptionUsageScopeGeneral = "general"
	SubscriptionUsageScopeZen     = "zen"
)

// ZenAuthorization identifies the designated subscription, not a second balance.
// A cancelled or expired subscription remains referenced for history and settlement.
type ZenAuthorization struct {
	Id             int   `json:"-"`
	UserId         int   `json:"user_id" gorm:"not null;uniqueIndex"`
	SubscriptionId int   `json:"subscription_id" gorm:"not null;index"`
	Revision       int64 `json:"revision" gorm:"type:bigint;not null"`
	UpdatedAt      int64 `json:"updated_at" gorm:"type:bigint"`
}

// TableName keeps the deployed table name. The Go type is named after the aiscoo zen plan,
// but the table, its indexes and the XCODE_* wire identifiers predate the rename; changing
// them would orphan existing authorizations and break installed desktop clients.
func (ZenAuthorization) TableName() string { return "x_code_authorizations" }

var (
	ErrZenPlanUnassigned                = errors.New("aiscoo zen plan is unassigned")
	ErrZenPlanExpired                   = errors.New("aiscoo zen plan is expired")
	ErrZenPlanRevoked                   = errors.New("aiscoo zen plan is revoked")
	ErrZenAuthorizationConflict         = errors.New("aiscoo zen authorization changed")
	ErrZenPlanConfigInvalid             = errors.New("aiscoo zen plan configuration is invalid")
	ErrZenPaymentComplianceRequired     = errors.New("payment compliance confirmation is required")
	ErrNoActiveSubscription             = errors.New("no active subscription")
	ErrSubscriptionQuotaInsufficient    = errors.New("subscription quota insufficient")
	ErrSubscriptionFundingTargetInvalid = errors.New("invalid subscription funding target")
	ErrSubscriptionRequestConflict      = errors.New("subscription request conflicts with original funding")
)

// GetZenFundingTarget resolves the server-owned designation without selecting
// another subscription or consulting the user's native billing preference.
func GetZenFundingTarget(userID int) (SubscriptionFundingTarget, error) {
	if userID <= 0 {
		return SubscriptionFundingTarget{}, ErrSubscriptionFundingTargetInvalid
	}
	var authorization ZenAuthorization
	if err := DB.Where("user_id = ?", userID).First(&authorization).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SubscriptionFundingTarget{}, ErrZenPlanUnassigned
		}
		return SubscriptionFundingTarget{}, err
	}
	if authorization.SubscriptionId <= 0 || authorization.Revision <= 0 {
		return SubscriptionFundingTarget{}, ErrZenPlanRevoked
	}
	var subscription UserSubscription
	if err := DB.First(&subscription, authorization.SubscriptionId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SubscriptionFundingTarget{}, ErrZenPlanRevoked
		}
		return SubscriptionFundingTarget{}, err
	}
	now, err := getDBTimestamp(DB)
	if err != nil {
		return SubscriptionFundingTarget{}, err
	}
	if err := validateZenFundingSubscription(&subscription, userID, now); err != nil {
		return SubscriptionFundingTarget{}, err
	}
	return SubscriptionFundingTarget{
		Scope:                 SubscriptionUsageScopeZen,
		SubscriptionID:        subscription.Id,
		AuthorizationRevision: authorization.Revision,
	}, nil
}

func validateZenFundingSubscription(subscription *UserSubscription, userID int, now int64) error {
	if subscription.UserId != userID || subscription.UsageScope != SubscriptionUsageScopeZen {
		return ErrZenPlanRevoked
	}
	if subscription.Status == "expired" {
		return ErrZenPlanExpired
	}
	if subscription.Status != "active" {
		return ErrZenPlanRevoked
	}
	if subscription.EndTime <= now {
		return ErrZenPlanExpired
	}
	if subscription.StartTime > now {
		return ErrZenPlanUnassigned
	}
	return nil
}

// ZenAuthorizationState is the locked, persisted state, not a virtual balance.
type ZenAuthorizationState struct {
	Authorization *ZenAuthorization
	Subscription  *UserSubscription
	PlanTitle     string
	Now           int64
	Audit         *ZenAuthorizationChange
}

type ZenAuthorizationChange struct {
	OldSubscriptionID  int
	OldEndTime         int64
	OldResetGeneration int64
	OldLastResetTime   int64
	OldNextResetTime   int64
}

func lockZenAuthorizationTx(tx *gorm.DB, userID int) (*ZenAuthorization, error) {
	var user User
	if err := lockForUpdate(tx).Select("id").First(&user, userID).Error; err != nil {
		return nil, err
	}
	var authorization ZenAuthorization
	err := lockForUpdate(tx).Where("user_id = ?", userID).First(&authorization).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if authorization.Revision <= 0 || authorization.SubscriptionId <= 0 {
		return nil, ErrZenAuthorizationConflict
	}
	return &authorization, nil
}

func loadZenAuthorizationStateTx(tx *gorm.DB, userID int) (*ZenAuthorizationState, error) {
	authorization, err := lockZenAuthorizationTx(tx, userID)
	if err != nil {
		return nil, err
	}
	state := &ZenAuthorizationState{Authorization: authorization}
	if authorization != nil {
		var sub UserSubscription
		if err := lockForUpdate(tx).First(&sub, authorization.SubscriptionId).Error; err != nil {
			return nil, err
		}
		if sub.UserId != userID || sub.UsageScope != SubscriptionUsageScopeZen {
			return nil, ErrSubscriptionFundingTargetInvalid
		}
		state.Subscription = &sub
	}
	state.Now, err = getDBTimestamp(tx)
	return state, err
}

func completeZenAuthorizationStateTx(tx *gorm.DB, state *ZenAuthorizationState, applyReset bool) error {
	sub := state.Subscription
	if sub == nil {
		return nil
	}
	if applyReset && sub.Status == "active" && sub.StartTime <= state.Now && sub.EndTime > state.Now {
		if err := maybeResetUserSubscriptionWithPlanTx(tx, sub, nil, state.Now); err != nil {
			return err
		}
	}
	var plan SubscriptionPlan
	err := tx.Select("id", "title").First(&plan, sub.PlanId).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	state.PlanTitle = plan.Title
	return nil
}

// GetZenAuthorizationState applies a due reset under the designation's instance lock.
func GetZenAuthorizationState(userID int) (*ZenAuthorizationState, error) {
	if userID <= 0 {
		return nil, ErrSubscriptionFundingTargetInvalid
	}
	var state *ZenAuthorizationState
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		state, err = loadZenAuthorizationStateTx(tx, userID)
		if err != nil {
			return err
		}
		return completeZenAuthorizationStateTx(tx, state, true)
	})
	if err != nil {
		return nil, err
	}
	return state, nil
}

// ZEN: 共用管理员授予校验，避免展示页承诺实际上不可授予的模板。
func ValidateZenPlanForGrant(plan *SubscriptionPlan, start int64) error {
	if plan == nil || plan.Id <= 0 || !plan.Enabled || plan.MaxPurchasePerUser != 0 || plan.TotalAmount < 0 ||
		plan.CustomSeconds < 0 || plan.QuotaResetCustomSeconds < 0 {
		return ErrZenPlanConfigInvalid
	}
	switch plan.DurationUnit {
	case SubscriptionDurationYear:
		if plan.DurationValue <= 0 || int64(plan.DurationValue) > math.MaxInt64/1000/(366*86400) {
			return ErrZenPlanConfigInvalid
		}
	case SubscriptionDurationMonth:
		if plan.DurationValue <= 0 || int64(plan.DurationValue) > math.MaxInt64/1000/(31*86400) {
			return ErrZenPlanConfigInvalid
		}
	case SubscriptionDurationDay:
		if plan.DurationValue <= 0 || int64(plan.DurationValue) > math.MaxInt64/int64(24*time.Hour) {
			return ErrZenPlanConfigInvalid
		}
	case SubscriptionDurationHour:
		if plan.DurationValue <= 0 || int64(plan.DurationValue) > math.MaxInt64/int64(time.Hour) {
			return ErrZenPlanConfigInvalid
		}
	case SubscriptionDurationCustom:
		if plan.CustomSeconds <= 0 || plan.CustomSeconds > math.MaxInt64/int64(time.Second) {
			return ErrZenPlanConfigInvalid
		}
	default:
		return ErrZenPlanConfigInvalid
	}
	if plan.QuotaResetPeriod != NormalizeResetPeriod(plan.QuotaResetPeriod) ||
		(plan.QuotaResetPeriod == SubscriptionResetCustom &&
			(plan.QuotaResetCustomSeconds <= 0 || plan.QuotaResetCustomSeconds > math.MaxInt64/int64(time.Second))) {
		return ErrZenPlanConfigInvalid
	}
	end, err := calcPlanEndTime(time.Unix(start, 0), plan)
	if err != nil || end <= start || end > math.MaxInt64/1000 {
		return ErrZenPlanConfigInvalid
	}
	return nil
}

func advanceZenAuthorizationRevisionTx(tx *gorm.DB, authorization *ZenAuthorization, now int64) error {
	if authorization == nil || authorization.Revision <= 0 || authorization.Revision == math.MaxInt64 {
		return ErrZenAuthorizationConflict
	}
	result := tx.Model(&ZenAuthorization{}).Where("id = ? AND revision = ?", authorization.Id, authorization.Revision).
		Updates(map[string]any{"revision": authorization.Revision + 1, "subscription_id": authorization.SubscriptionId, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrZenAuthorizationConflict
	}
	authorization.Revision++
	authorization.UpdatedAt = now
	return nil
}

func cancelZenSubscriptionTx(tx *gorm.DB, sub *UserSubscription, now int64) (bool, error) {
	if err := maybeResetUserSubscriptionWithPlanTx(tx, sub, nil, now); err != nil {
		return false, err
	}
	if err := tx.Model(sub).Updates(map[string]any{"status": "cancelled", "end_time": now, "updated_at": now}).Error; err != nil {
		return false, err
	}
	sub.Status, sub.EndTime = "cancelled", now
	target, err := downgradeUserGroupForSubscriptionTx(tx, sub, now)
	return target != "", err
}

// MutateZenAuthorization serializes administrator transitions by user and revision.
func MutateZenAuthorization(userID int, action string, expectedRevision int64, planID int, advanceResetTime bool) (*ZenAuthorizationState, error) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		return nil, ErrZenPaymentComplianceRequired
	}
	if userID <= 0 || expectedRevision < 0 || expectedRevision == math.MaxInt64 {
		return nil, ErrZenAuthorizationConflict
	}
	var state *ZenAuthorizationState
	groupChanged := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		state, err = loadZenAuthorizationStateTx(tx, userID)
		if err != nil {
			return err
		}
		revision := int64(0)
		if state.Authorization != nil {
			revision = state.Authorization.Revision
		}
		if revision != expectedRevision {
			return ErrZenAuthorizationConflict
		}
		sub := state.Subscription
		state.Audit = &ZenAuthorizationChange{}
		if sub != nil {
			state.Audit.OldSubscriptionID = sub.Id
			state.Audit.OldEndTime = sub.EndTime
			state.Audit.OldResetGeneration = sub.QuotaResetGeneration
			state.Audit.OldLastResetTime = sub.LastResetTime
			state.Audit.OldNextResetTime = sub.NextResetTime
		}
		active := sub != nil && sub.Status == "active" && sub.StartTime <= state.Now && sub.EndTime > state.Now
		create := false
		switch action {
		case "grant":
			if active {
				return ErrZenAuthorizationConflict
			}
			create = true
		case "renew":
			if sub == nil || sub.PlanId != planID || (sub.Status != "active" && sub.Status != "expired") {
				return ErrZenAuthorizationConflict
			}
			if !active {
				if sub.EndTime > state.Now {
					return ErrZenAuthorizationConflict
				}
				create = true
			}
		case "replace":
			if !active || sub.PlanId == planID {
				return ErrZenAuthorizationConflict
			}
			create = true
		case "revoke":
			if sub == nil || (sub.Status != "active" && sub.Status != "expired") {
				return ErrZenAuthorizationConflict
			}
		case "reset":
			if !active {
				return ErrZenAuthorizationConflict
			}
		default:
			return ErrZenAuthorizationConflict
		}
		var plan SubscriptionPlan
		if action == "grant" || action == "renew" || action == "replace" {
			if planID <= 0 {
				return ErrZenPlanConfigInvalid
			}
			if err := lockForUpdate(tx).First(&plan, planID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrZenPlanConfigInvalid
				}
				return err
			}
			start := state.Now
			if action == "renew" && active {
				start = sub.EndTime
			}
			if err := ValidateZenPlanForGrant(&plan, start); err != nil {
				return err
			}
		}
		if action == "replace" || action == "revoke" {
			groupChanged, err = cancelZenSubscriptionTx(tx, sub, state.Now)
			if err != nil {
				return err
			}
		}
		if create {
			// Expired upgrades must be unwound before the new instance snapshots the user's group.
			if sub != nil && sub.Status != "cancelled" {
				target, err := downgradeUserGroupForSubscriptionTx(tx, sub, state.Now)
				if err != nil {
					return err
				}
				groupChanged = groupChanged || target != ""
			}
			state.Subscription, err = CreateUserSubscriptionFromPlanTx(tx, userID, &plan, "admin", SubscriptionUsageScopeZen)
			if err != nil {
				return err
			}
			state.Now = state.Subscription.StartTime
			groupChanged = groupChanged || state.Subscription.PrevUserGroup != ""
		} else if action == "renew" {
			// Reset any elapsed original period before extending the lifetime.
			if err := maybeResetUserSubscriptionWithPlanTx(tx, sub, nil, state.Now); err != nil {
				return err
			}
			end, err := calcPlanEndTime(time.Unix(sub.EndTime, 0), &plan)
			if err != nil {
				return err
			}
			sub.EndTime = end
			resetPlan, err := subscriptionResetPlan(sub, nil)
			if err != nil {
				return err
			}
			if sub.NextResetTime == 0 {
				anchor := sub.LastResetTime
				if anchor <= 0 {
					anchor = sub.StartTime
				}
				sub.NextResetTime = calcNextResetTime(time.Unix(anchor, 0), resetPlan, end)
			}
			if err := tx.Save(sub).Error; err != nil {
				return err
			}
		} else if action == "reset" {
			if err := resetUserSubscriptionTx(tx, sub, nil, state.Now, advanceResetTime); err != nil {
				return err
			}
		}
		if state.Authorization == nil {
			state.Authorization = &ZenAuthorization{UserId: userID, SubscriptionId: state.Subscription.Id, Revision: 1, UpdatedAt: state.Now}
			if err := tx.Create(state.Authorization).Error; err != nil {
				return err
			}
		} else {
			state.Authorization.SubscriptionId = state.Subscription.Id
			if err := advanceZenAuthorizationRevisionTx(tx, state.Authorization, state.Now); err != nil {
				return err
			}
		}
		return completeZenAuthorizationStateTx(tx, state, action != "reset")
	})
	if err != nil {
		// SQLite readers can race the sole writer; the losing transition never commits.
		if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "database table is locked") {
			return nil, ErrZenAuthorizationConflict
		}
		return nil, err
	}
	if groupChanged {
		if err := RefreshUserGroupCache(userID); err != nil {
			return state, fmt.Errorf("aiscoo zen authorization committed but group cache refresh failed: %w", err)
		}
	}
	return state, nil
}

func lockAdminSubscriptionTx(tx *gorm.DB, hint *UserSubscription) (*UserSubscription, *ZenAuthorization, int64, error) {
	var authorization *ZenAuthorization
	var err error
	if hint.UsageScope == SubscriptionUsageScopeZen {
		authorization, err = lockZenAuthorizationTx(tx, hint.UserId)
		if err != nil {
			return nil, nil, 0, err
		}
	} else {
		var user User
		if err := lockForUpdate(tx).Select("id").Where("id = ?", hint.UserId).Find(&user).Error; err != nil {
			return nil, nil, 0, err
		}
	}
	var sub UserSubscription
	if err := lockForUpdate(tx).First(&sub, hint.Id).Error; err != nil {
		return nil, nil, 0, err
	}
	if sub.UserId != hint.UserId || sub.UsageScope != hint.UsageScope {
		return nil, nil, 0, ErrZenAuthorizationConflict
	}
	now := time.Now().Unix()
	if sub.UsageScope == SubscriptionUsageScopeZen {
		now, err = getDBTimestamp(tx)
	}
	return &sub, authorization, now, err
}

func preventReferencedZenSubscriptionDeleteTx(tx *gorm.DB, sub *UserSubscription) error {
	var count int64
	if err := tx.Model(&ZenAuthorization{}).Where("subscription_id = ?", sub.Id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrZenAuthorizationConflict
	}
	if err := tx.Model(&SubscriptionPreConsumeRecord{}).Where("user_subscription_id = ?", sub.Id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrZenAuthorizationConflict
	}
	var tasks []Task
	if err := tx.Select("private_data").Where("user_id = ? AND (status NOT IN ? OR status IS NULL)",
		sub.UserId, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).Find(&tasks).Error; err != nil {
		return err
	}
	for _, task := range tasks {
		if task.PrivateData.SubscriptionId == sub.Id {
			return ErrZenAuthorizationConflict
		}
	}
	return nil
}
