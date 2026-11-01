package model

import (
	"crypto/hmac"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// XCodeRevokeOperation records only keyed verifiers, never a refresh secret.
// OperationHash is the service-derived HMAC of "<sid>:<operation_id>", so the
// key space is disjoint per session: an ID used by one session is invisible to
// every other session and never conflicts with it.
type XCodeRevokeOperation struct {
	OperationHash  string `gorm:"type:char(64);primaryKey" json:"-"`
	CredentialHash string `gorm:"type:char(64);not null" json:"-"`
	UserID         int    `gorm:"not null"`
	SessionID      string `gorm:"type:varchar(64);not null"`
	ClientID       string `gorm:"type:varchar(32);not null"`
	ExpiresAt      int64  `gorm:"not null;index"`
	Result         string `gorm:"type:varchar(20);not null"`
}

// DeleteExpiredXCodeRevokeOperations bounds stored result verifiers.
func DeleteExpiredXCodeRevokeOperations(now int64) error {
	for {
		var hashes []string
		if err := DB.Model(&XCodeRevokeOperation{}).Where("expires_at <= ?", now).Limit(500).Pluck("operation_hash", &hashes).Error; err != nil {
			return err
		}
		if len(hashes) == 0 {
			return nil
		}
		if err := DB.Where("operation_hash IN ? AND expires_at <= ?", hashes, now).Delete(&XCodeRevokeOperation{}).Error; err != nil {
			return err
		}
	}
}

// xcodeRevokeCredentialAccepted is the single rule for "this refresh digest may
// start a new revocation": it must be the current digest, or the immediately
// previous one inside the concurrent-refresh grace window, and the session must
// not have expired. It is applied twice on purpose: once on a snapshot read
// before any transaction (so an invalid credential never queues on row locks),
// and again on the locked row inside the transaction (the snapshot may be
// stale, so it can only reject early, never authorize).
func xcodeRevokeCredentialAccepted(session *UserSession, refreshHash string, now int64) bool {
	current := hmac.Equal([]byte(session.RefreshHash), []byte(refreshHash))
	previous := session.PreviousRefreshHash != "" && now <= session.PreviousValidUntil &&
		hmac.Equal([]byte(session.PreviousRefreshHash), []byte(refreshHash))
	return (current || previous) && session.ExpiresAt > now
}

// RunXCodeRevokeOperation serializes the mutation on the session owner and
// records its result in the same transaction as the revocation.
func RunXCodeRevokeOperation(sid, clientID, operationHash, credentialHash, refreshHash string, query bool) (string, error) {
	// 结果保留期独立于 SID 清理策略；仅凭绑定的原 refresh 证明可读已提交结果。
	var saved XCodeRevokeOperation
	err := DB.Where("operation_hash = ?", operationHash).First(&saved).Error
	if err == nil {
		if saved.SessionID != sid || saved.ClientID != clientID || saved.ExpiresAt <= time.Now().Unix() || !hmac.Equal([]byte(saved.CredentialHash), []byte(credentialHash)) {
			return "", ErrUserSessionRefreshInvalid
		}
		if saved.Result != "committed" && saved.Result != "already_revoked" {
			return "", ErrUserSessionInvalid
		}
		return saved.Result, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	if query {
		return "", ErrUserSessionRefreshInvalid
	}
	candidate, err := GetUserSessionBySID(sid)
	if err != nil {
		return "", err
	}
	// MySQL 的默认排序规则不区分大小写，大写 SID 也能命中小写行；操作键由 SID 派生，
	// 必须与存储的规范 SID 逐字节一致，否则同一会话会得到多个键空间。
	if candidate.SID != sid {
		return "", ErrUserSessionInvalid
	}
	// 预检只作用于“尚无操作记录、要新建撤销”的分支（上面已读过记录并在查询时返回），
	// 因此不影响凭据已轮换后凭记录读取已提交结果的语义。它基于事务外的快照，只能提前
	// 拒绝，不能授权；事务内仍在行锁下用同一规则重新核验。
	if !xcodeRevokeCredentialAccepted(candidate, refreshHash, time.Now().Unix()) {
		return "", ErrUserSessionRefreshInvalid
	}
	var session UserSession
	result := ""
	err = DB.Transaction(func(tx *gorm.DB) error {
		// SQLite 不支持 FOR UPDATE；先获取写锁，避免并发读事务升级丢失隔离。
		if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			if err := tx.Model(&User{}).Where("id = ?", candidate.UserID).UpdateColumn("auth_version", gorm.Expr("auth_version")).Error; err != nil {
				return err
			}
		}
		var user User
		if err := lockForUpdate(tx).First(&user, candidate.UserID).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("sid = ? AND user_id = ?", sid, candidate.UserID).First(&session).Error; err != nil {
			return err
		}
		storedClient := session.ClientID
		if storedClient == "" {
			storedClient = "xcode-desktop"
		}
		if session.ClientPurpose != ClientPurposeXCode || storedClient != clientID {
			return ErrUserSessionInvalid
		}
		now := time.Now().Unix()
		var operation XCodeRevokeOperation
		err := tx.Where("operation_hash = ?", operationHash).First(&operation).Error
		if err == nil {
			if operation.UserID != session.UserID || operation.SessionID != sid || operation.ClientID != clientID || operation.ExpiresAt <= now || !hmac.Equal([]byte(operation.CredentialHash), []byte(credentialHash)) {
				return ErrUserSessionRefreshInvalid
			}
			if operation.Result != "committed" && operation.Result != "already_revoked" {
				return ErrUserSessionInvalid
			}
			result = operation.Result
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if query {
			return ErrUserSessionRefreshInvalid
		}
		if !xcodeRevokeCredentialAccepted(&session, refreshHash, now) {
			return ErrUserSessionRefreshInvalid
		}
		// 已撤销凭据只能查询既有绑定，不能创建无限个新操作记录。
		if session.Status != UserSessionStatusActive || session.RevokedAt != 0 {
			return ErrUserSessionInactive
		}
		if user.Status != common.UserStatusEnabled || user.AuthVersion != session.UserAuthVersion {
			return ErrUserSessionInactive
		}
		floor, err := getUserAuthVersionFloor(user.Id)
		if err != nil {
			return err
		}
		if floor > session.UserAuthVersion {
			return ErrUserSessionInactive
		}
		// 在缓存 fence 前写入操作记录。键按 SID 隔离，只有同一会话的并发提交会
		// 竞争同一键，而它们已被用户行锁与会话行锁串行化。
		operation = XCodeRevokeOperation{OperationHash: operationHash, CredentialHash: credentialHash, UserID: session.UserID, SessionID: sid, ClientID: clientID, ExpiresAt: min(session.ExpiresAt, now+7*24*60*60), Result: "committed"}
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		// 先取得会话行写锁并验证 refresh 未并发轮换，再发布不可撤回的缓存拒绝栅栏。
		updated := tx.Model(&UserSession{}).Where("sid = ? AND user_id = ? AND status = ? AND revoked_at = 0 AND refresh_hash = ?", sid, session.UserID, UserSessionStatusActive, session.RefreshHash).Updates(map[string]any{"status": UserSessionStatusRevoked, "revoked_at": now, "revoked_reason": "xcode_logout"})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrUserSessionInactive
		}
		if err := writeUserSessionDenyFence(&session, UserSessionStatusRevoking, now, "xcode_logout"); err != nil {
			return err
		}
		if err := disableXCodeManagedTokensWithTx(tx, session.UserID, []string{sid}); err != nil {
			return err
		}
		session.Status, session.RevokedAt, session.RevokedReason = UserSessionStatusRevoked, now, "xcode_logout"
		result = "committed"
		return nil
	})
	if err != nil {
		// 事务报错（含提交失败）时结果可能已持久化，也可能没有；本次提交一律
		// 返回错误，由客户端用 query 确认，绝不据此伪称 committed。
		return "", err
	}
	if result == "committed" && !query {
		if err := writeUserSessionCache(session.cacheEntry(), time.Time{}); err != nil {
			common.SysLog("failed to finalize xcode session revoke tombstone")
		}
	}
	return result, nil
}
