package herdrx

import (
	"encoding/json"
	"testing"
)

// liveExplainResponse is the agent.explain payload captured from a live herdr
// 0.9.0 daemon, trimmed to one evaluated rule. Keeping a real response rather
// than a hand-written one is the point: the narrowing below has to survive the
// fields this package deliberately ignores, and a fabricated fixture would
// only ever contain the fields the author remembered.
const liveExplainResponse = `{
  "explain": {
    "agent": "claude",
    "cached_remote_version": "2026.09.04.1",
    "evaluated_rules": [
      {"id": "osc_title_working", "matched": false, "priority": 1100, "region": "osc_title", "state": "working",
       "evidence": {"all_count": 0, "any_count": 0, "region_bytes": 40, "region_preview": "* a title"}}
    ],
    "fallback_reason": null,
    "local_override_shadowing_remote": false,
    "manifest_source": "remote:/x/claude.toml",
    "manifest_version": "2026.09.04.1",
    "matched_rule": {"id": "bash_permission_prompt", "priority": 850, "region": "prompt_box_body", "state": "blocked"},
    "remote_update_error": null,
    "remote_update_status": "current",
    "screen_detection_skipped": false,
    "skip_state_update": false,
    "skipped_update_reason": null,
    "state": "blocked",
    "visible_blocker": true,
    "visible_idle": false,
    "visible_working": false,
    "warning": null
  }
}`

func TestExplainNarrowsTheLiveResponse(t *testing.T) {
	var res explainResult
	if err := json.Unmarshal([]byte(liveExplainResponse), &res); err != nil {
		t.Fatal(err)
	}
	if res.Explain.MatchedRule == nil {
		t.Fatal("matched_rule did not decode")
	}
	if got := res.Explain.MatchedRule.ID; got != "bash_permission_prompt" {
		t.Errorf("rule = %q", got)
	}
	if got := res.Explain.MatchedRule.Region; got != "prompt_box_body" {
		t.Errorf("region = %q", got)
	}
	if got := res.Explain.State; got != "blocked" {
		t.Errorf("state = %q", got)
	}
	if got := res.Explain.ManifestVersion; got != "2026.09.04.1" {
		t.Errorf("manifest = %q", got)
	}
	// null, not absent — the live daemon sends both of these as JSON null on
	// a confident classification, and a string field must take that as empty
	// rather than failing the whole decode.
	if res.Explain.Warning != "" || res.Explain.FallbackReason != "" {
		t.Errorf("warning=%q fallback=%q, want both empty", res.Explain.Warning, res.Explain.FallbackReason)
	}
}

// TestExplainSummaryOmitsState is the done-row contradiction, pinned.
//
// herdr's detection rules only ever produce working, blocked, idle or unknown;
// `done` is derived above them as "idle with unseen work". So a done row's
// explanation says idle, and a preview line that repeated it would read as a
// contradiction of the badge two lines above.
func TestExplainSummaryOmitsState(t *testing.T) {
	e := Explain{State: "idle", RuleID: "live_prompt_box"}
	if got := e.Summary(); got != "live_prompt_box" {
		t.Errorf("Summary() = %q, want the rule alone", got)
	}
}

// TestExplainSummaryEmptyWithoutARule: a caller prints this unconditionally,
// so "herdr matched nothing" must render as silence, not as a stray separator.
func TestExplainSummaryEmptyWithoutARule(t *testing.T) {
	if got := (Explain{State: "idle"}).Summary(); got != "" {
		t.Errorf("Summary() = %q, want empty", got)
	}
}
