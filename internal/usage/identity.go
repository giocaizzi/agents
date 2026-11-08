package usage

import (
	"encoding/json"
	"errors"
	"strings"
)

type anthropicProfile struct {
	Account struct {
		UUID  string `json:"uuid"`
		Email string `json:"email"`
	} `json:"account"`
	Organization struct {
		UUID          string `json:"uuid"`
		Type          string `json:"organization_type"`
		RateLimitTier string `json:"rate_limit_tier"`
	} `json:"organization"`
}

// ParseAnthropicProfile parses GET api.anthropic.com/api/oauth/profile. It
// fails when the account uuid is missing. Errors never include body content.
func ParseAnthropicProfile(body []byte) (Profile, error) {
	var p anthropicProfile
	if json.Unmarshal(body, &p) != nil {
		return Profile{}, errors.New("parse anthropic profile: malformed JSON")
	}
	if p.Account.UUID == "" {
		return Profile{}, errors.New("parse anthropic profile: no account uuid")
	}
	out := Profile{AccountID: p.Account.UUID, Email: p.Account.Email, OrgID: p.Organization.UUID}
	switch typ := strings.ToLower(strings.TrimSpace(p.Organization.Type)); typ {
	case "claude_max", "claude_pro", "claude_team", "claude_enterprise":
		out.Plan = AnthropicPlan(strings.TrimPrefix(typ, "claude_"), p.Organization.RateLimitTier)
	}
	return out, nil
}

// ParseGitHubUser parses GET api.github.com/user. It fails when the login is
// missing. Errors never include body content.
func ParseGitHubUser(body []byte) (Profile, error) {
	var u struct {
		Login string `json:"login"`
	}
	if json.Unmarshal(body, &u) != nil {
		return Profile{}, errors.New("parse github user: malformed JSON")
	}
	if u.Login == "" {
		return Profile{}, errors.New("parse github user: no login")
	}
	return Profile{Login: u.Login}, nil
}

// ParseOpenAIIdentity reads the identity fields of GET
// chatgpt.com/backend-api/wham/usage: the top-level account_id and user_id,
// which the Codex client checks against its token's chatgpt_account_id and
// chatgpt_user_id (codex-rs/backend-client/src/types.rs,
// RateLimitStatusWithResetCredits). It fails unless both are present: the
// account id is a workspace shared by every member. Errors never include body
// content.
func ParseOpenAIIdentity(body []byte) (Profile, error) {
	var u struct {
		AccountID string `json:"account_id"`
		UserID    string `json:"user_id"`
		PlanType  string `json:"plan_type"`
	}
	if json.Unmarshal(body, &u) != nil {
		return Profile{}, errors.New("parse openai identity: malformed JSON")
	}
	if u.AccountID == "" || u.UserID == "" {
		return Profile{}, errors.New("parse openai identity: no account or user id")
	}
	out := Profile{AccountID: u.AccountID, UserID: u.UserID}
	if p := OpenAIPlan(u.PlanType); p.Kind != KindUnknown {
		out.Plan = p
	}
	return out, nil
}
