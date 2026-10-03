package server

import (
	"fmt"
	"testing"
)

func TestLevelOfEnglishVocabulary(t *testing.T) {
	for _, word := range []string{"fatal", "panic", "error", "err:", "failed", "failure", "denied", "dropped", "rejected", "refused", "unreachable", "✕", "FAILED", "retry failed"} {
		if got := levelOf("operation " + word); got != "error" {
			t.Errorf("levelOf(%q)=%s, want error", word, got)
		}
	}
	for _, word := range []string{"warn", "disabled", "skip", "stopped", "⚠", "retry", "retrying", "RETRYING"} {
		if got := levelOf("operation " + word); got != "warn" {
			t.Errorf("levelOf(%q)=%s, want warn", word, got)
		}
	}
	if got := levelOf("Server started"); got != "info" {
		t.Errorf("ordinary message level=%s", got)
	}
}

func TestDefaultConversationTitleCompatibility(t *testing.T) {
	for _, title := range []string{"", "New conversation", "新对话"} {
		if !isDefaultConversationTitle(title) {
			t.Errorf("default title %q would not be renamed", title)
		}
	}
	if isDefaultConversationTitle("Custom conversation") {
		t.Fatal("custom title would be overwritten")
	}
}

func TestMentionWireWordsAndLegacyAliases(t *testing.T) {
	for _, tc := range []struct{ current, legacy, kind string }{
		{"finding", "漏洞", "finding"}, {"asset", "资产", "asset"}, {"company", "企业", "company"},
		{"api", "接口", "endpoint"}, {"ip", "IP", "ip"}, {"app", "应用", "app"},
		{"domain", "域名", "root_domain"}, {"subdomain", "子域名", "subdomain"}, {"service", "服务", "service"},
	} {
		t.Run(tc.current, func(t *testing.T) {
			refs, err := parseChatMentions(fmt.Sprintf("@[%s#12 Current] @[%s#12 Legacy]", tc.current, tc.legacy))
			if err != nil || len(refs) != 1 || refs[0].Kind != tc.kind || refs[0].ID != 12 {
				t.Fatalf("legacy/current identity mismatch: %+v %v", refs, err)
			}
		})
	}
}
