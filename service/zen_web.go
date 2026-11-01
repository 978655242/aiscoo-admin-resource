package service

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func ZenWebCookieName(origin string) string {
	if strings.HasPrefix(origin, "https://") {
		return "__Secure-new_api_xcode_web"
	}
	return "new_api_xcode_web"
}

var ErrZenWebOriginForbidden = errors.New("xcode web request origin is not permitted")

type zenWebHandoffState struct {
	AuthIdentity
	ClientPurpose string `json:"client_purpose"`
}

// ValidateZenWebRequest uses configured authority, never forwarded host headers.
func ValidateZenWebRequest(request *http.Request) (string, error) {
	origin, err := common.GetZenPublicOrigin()
	if err != nil {
		return "", err
	}
	configured, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	if request == nil || !strings.EqualFold(request.Host, configured.Host) {
		return "", ErrZenWebOriginForbidden
	}
	origins := request.Header.Values("Origin")
	if len(origins) > 1 {
		return "", ErrZenWebOriginForbidden
	}
	if len(origins) == 1 {
		actual, err := common.NormalizeOrigin(origins[0])
		expected, expectedErr := common.NormalizeOrigin(origin)
		if err != nil || expectedErr != nil || actual != expected {
			return "", ErrZenWebOriginForbidden
		}
	}
	sites := request.Header.Values("Sec-Fetch-Site")
	if len(sites) > 1 {
		return "", ErrZenWebOriginForbidden
	}
	if len(sites) == 1 && sites[0] != "same-origin" && sites[0] != "none" {
		return "", ErrZenWebOriginForbidden
	}
	// A system-browser navigation has no Origin; a legacy browser may use Referer.
	if len(origins) == 0 && request.Header.Get("Referer") != "" {
		referers := request.Header.Values("Referer")
		if len(referers) != 1 {
			return "", ErrZenWebOriginForbidden
		}
		referer, err := url.Parse(referers[0])
		if err != nil || referer.User != nil || referer.Host == "" {
			return "", ErrZenWebOriginForbidden
		}
		actual, err := common.NormalizeOrigin(referer.Scheme + "://" + referer.Host)
		expected, expectedErr := common.NormalizeOrigin(origin)
		if err != nil || expectedErr != nil || actual != expected {
			return "", ErrZenWebOriginForbidden
		}
	}
	return origin, nil
}

func CreateZenWebHandoff(identity AuthIdentity) (*dto.ZenWebHandoffResponse, error) {
	if identity.UserID <= 0 || identity.SessionID == "" || identity.UserAuthVersion <= 0 || identity.SessionVersion <= 0 {
		return nil, ErrLoginSessionInvalid
	}
	origin, err := common.GetZenPublicOrigin()
	if err != nil {
		return nil, err
	}
	if model.DB == nil {
		return nil, ErrZenBackendUnavailable
	}
	var response dto.ZenWebHandoffResponse
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		_, session, err := model.ValidateZenSourceSessionWithTx(tx, identity.UserID, identity.SessionID,
			identity.UserAuthVersion, identity.SessionVersion, model.ClientPurposeZen)
		if err != nil {
			return zenSourceError(err, ErrLoginSessionRevoked)
		}
		state := zenWebHandoffState{
			AuthIdentity: AuthIdentity{UserID: session.UserID, SessionID: session.SID,
				UserAuthVersion: session.UserAuthVersion, SessionVersion: session.Version},
			ClientPurpose: session.ClientPurpose,
		}
		payload, err := common.Marshal(state)
		if err != nil {
			return err
		}
		expiresAt := time.Unix(min(time.Now().Add(time.Minute).Unix(), session.ExpiresAt), 0)
		code, flow, err := model.CreateAuthFlowWithTx(tx, model.AuthFlowCreate{
			Purpose: model.AuthFlowPurposeZenWebHandoff, UserId: session.UserID, SessionId: session.SID,
			Payload: string(payload), ExpiresAt: expiresAt,
		})
		if err != nil {
			return err
		}
		if _, _, err := model.ValidateZenSourceSessionWithTx(tx, state.UserID, state.SessionID,
			state.UserAuthVersion, state.SessionVersion, model.ClientPurposeZen); err != nil {
			return zenSourceError(err, ErrLoginSessionRevoked)
		}
		response = dto.ZenWebHandoffResponse{SchemaVersion: 1, ServerTime: time.Now().UnixMilli(),
			URL: origin + "/xcode/plans?" + url.Values{"handoff": {code}}.Encode(), ExpiresAt: flow.ExpiresAt.UnixMilli()}
		return nil
	})
	if err != nil {
		return nil, zenFlowError(err, ErrZenAuthCodeInvalid)
	}
	return &response, nil
}

func zenWebHandoffBinding(flow *model.AuthFlow) (zenWebHandoffState, error) {
	var state zenWebHandoffState
	if flow == nil || flow.Purpose != model.AuthFlowPurposeZenWebHandoff || flow.Provider != "" || flow.Intent != "" ||
		common.UnmarshalJsonStr(flow.Payload, &state) != nil || state.ClientPurpose != model.ClientPurposeZen ||
		state.UserID <= 0 || state.SessionID == "" || state.UserAuthVersion <= 0 || state.SessionVersion <= 0 ||
		state.UserID != flow.UserId || state.SessionID != flow.SessionId {
		return state, ErrZenAuthCodeInvalid
	}
	return state, nil
}

func ConsumeZenWebHandoff(token string) (string, int64, error) {
	if !zenUnreserved(token, 43, 43) {
		return "", 0, ErrZenAuthCodeInvalid
	}
	if model.DB == nil {
		return "", 0, ErrZenBackendUnavailable
	}
	match := model.AuthFlowMatch{Purpose: model.AuthFlowPurposeZenWebHandoff}
	bound, err := model.GetAuthFlow(token, match)
	if err != nil {
		return "", 0, zenFlowError(err, ErrZenAuthCodeInvalid)
	}
	state, err := zenWebHandoffBinding(bound)
	if err != nil {
		return "", 0, err
	}
	var webToken string
	var expiresAt int64
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		// Owner -> source SID -> flow; refresh may rotate the same SID before consumption.
		_, _, err := model.ValidateZenSourceSessionWithTx(tx, state.UserID, state.SessionID,
			state.UserAuthVersion, 0, model.ClientPurposeZen)
		if err != nil {
			return zenSourceError(err, ErrLoginSessionRevoked)
		}
		match.UserId, match.SessionId = state.UserID, state.SessionID
		_, err = model.ConsumeAuthFlowWithActionWithTx(tx, token, match, func(tx *gorm.DB, flow *model.AuthFlow) error {
			if flow.Id != bound.Id || flow.Payload != bound.Payload || !flow.ExpiresAt.Equal(bound.ExpiresAt) {
				return ErrZenAuthCodeInvalid
			}
			current, err := zenWebHandoffBinding(flow)
			if err != nil {
				return err
			}
			_, session, err := model.ValidateZenSourceSessionWithTx(tx, current.UserID, current.SessionID,
				current.UserAuthVersion, 0, model.ClientPurposeZen)
			if err != nil {
				return zenSourceError(err, ErrLoginSessionRevoked)
			}
			if flow.ExpiresAt.After(time.Unix(session.ExpiresAt, 0)) {
				return ErrZenAuthCodeInvalid
			}
			webToken, expiresAt, err = IssueZenWebToken(session)
			if err != nil {
				return err
			}
			_, _, err = model.ValidateZenSourceSessionWithTx(tx, current.UserID, current.SessionID,
				current.UserAuthVersion, session.Version, model.ClientPurposeZen)
			return zenSourceError(err, ErrLoginSessionRevoked)
		})
		return err
	})
	if err != nil {
		return "", 0, zenFlowError(err, ErrZenAuthCodeInvalid)
	}
	return webToken, expiresAt, nil
}

func GetZenWebView(userID int) (*dto.ZenWebView, error) {
	nativePlans, err := loadZenPlanBatch()
	if err != nil {
		return nil, err
	}
	plans, err := projectZenPlans(nativePlans)
	if err != nil {
		return nil, err
	}
	presentations := make([]dto.ZenPlanPresentation, 0, len(nativePlans))
	now := model.GetDBTimestamp()
	for i := range nativePlans {
		plan := &nativePlans[i]
		presentation := dto.ZenPlanPresentation{PlanID: strconv.Itoa(plan.Id)}
		if plan.ZenDisplay != nil && plan.ZenDisplay.IsInvalid() {
			reason, issue := "template_config", "invalid_display"
			presentation.AssignmentReason, presentation.DisplayIssue = &reason, &issue
		} else {
			if plan.ZenDisplay != nil {
				presentation.Metadata = &dto.ZenPlanDisplay{
					GroupKey: plan.ZenDisplay.GroupKey, GroupTitle: plan.ZenDisplay.GroupTitle,
					OfferKind:  plan.ZenDisplay.OfferKind,
					Highlights: plan.ZenDisplay.Highlights,
					Features:   plan.ZenDisplay.Features,
				}
			}
			if err := model.ValidateZenPlanForGrant(plan, now); err == nil {
				presentation.Assignable = true
			} else {
				reason := "template_config"
				presentation.AssignmentReason = &reason
			}
		}
		presentations = append(presentations, presentation)
	}
	entitlements, err := GetZenEntitlements(userID)
	if err != nil {
		return nil, err
	}
	display, err := GetZenQuotaDisplay()
	if err != nil {
		return nil, err
	}
	return &dto.ZenWebView{SchemaVersion: 1, ServerTime: time.Now().UnixMilli(),
		Plans: plans, Entitlements: entitlements, Display: display, PlanPresentations: presentations}, nil
}
