package dto

type ZenWebHandoffResponse struct {
	SchemaVersion int    `json:"schema_version"`
	ServerTime    int64  `json:"server_time"`
	URL           string `json:"url"`
	ExpiresAt     int64  `json:"expires_at"`
}

type ZenPlanDisplay struct {
	GroupKey   string   `json:"group_key"`
	GroupTitle string   `json:"group_title"`
	OfferKind  string   `json:"offer_kind"`
	Highlights []string `json:"highlights"`
	Features   []string `json:"features"`
}

type ZenPlanPresentation struct {
	PlanID           string          `json:"plan_id"`
	Metadata         *ZenPlanDisplay `json:"metadata"`
	Assignable       bool            `json:"assignable"`
	AssignmentReason *string         `json:"assignment_reason"`
	DisplayIssue     *string         `json:"display_issue"`
}

type ZenWebView struct {
	SchemaVersion int                       `json:"schema_version"`
	ServerTime    int64                     `json:"server_time"`
	Plans         *ZenPlansSnapshot         `json:"plans"`
	Entitlements  *ZenAuthorizationSnapshot `json:"entitlements"`
	Display       ZenQuotaDisplay           `json:"display"`
	// ZEN: presentation metadata is isolated from the strict public plan contract.
	PlanPresentations []ZenPlanPresentation `json:"plan_presentations"`
}
