package notify

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Capability boundaries for generic Webhook templates.
//
// This is the only place in the package that evaluates user-provided text as code.
// Tests must establish what it can do; otherwise a new context method or FuncMap
// readFile helper could silently expand capabilities behind an innocuous-looking diff.

// TestTemplateContextHasNoMethods is the central invariant.
//
// text/template calls exported methods: {{.Foo}} can access a field or invoke a
// method. Any reachable type with exported methods exposes those capabilities to
// template authors. The context deliberately contains only exported data fields
// and no methods.
//
// A failure means webhookTemplateData or webhookItem gained a method. Before
// allowing it, assess whether templates could use it to read unintended data.
func TestTemplateContextHasNoMethods(t *testing.T) {
	for _, v := range []any{webhookTemplateData{}, webhookItem{}} {
		typ := reflect.TypeOf(v)
		if n := typ.NumMethod(); n != 0 {
			var names []string
			for i := 0; i < n; i++ {
				names = append(names, typ.Method(i).Name)
			}
			t.Fatalf("%s exposes %d methods (%s): text/template can call them, "+
				"exposing their capabilities to template authors", typ.Name(), n, strings.Join(names, ", "))
		}
	}
}

// TestTemplateFuncsAreMinimal fixes the exposed function set.
//
// Each FuncMap entry grants a capability. Currently json / jsons only serialize
// values into JSON fragments; they cannot read files, send requests, or run commands.
func TestTemplateFuncsAreMinimal(t *testing.T) {
	var got []string
	for name := range webhookTemplateFuncs {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{"json", "jsons"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Template function set changed: got %v, expected %v. Before adding functions, verify they do not expand capabilities "+
			"(no file access, network requests, or command execution)", got, want)
	}
}

// TestTemplateCannotReachUnknownData checks out-of-bounds template access:
// nonexistent data must fail without echoing content or exposing internal data.
func TestTemplateCannotReachUnknownData(t *testing.T) {
	_, err := renderWebhookBody(`{"x": {{.Environment}}, "y": {{.Env}}}`, singleMsg())
	if err == nil {
		t.Fatal("Accessing a nonexistent field must fail")
	}
	// Errors must not contain actual finding titles or summaries from the template context.
	for _, leak := range []string{"SQL injection", "Parameter id"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("Template error exposes message content %q: %v", leak, err)
		}
	}
}

// TestTemplateRenderFailsPermanently treats invalid templates as configuration
// errors that retries cannot fix, avoiding three wasted backoffs per delivery.
func TestTemplateRenderFailsPermanently(t *testing.T) {
	cfg := map[string]any{
		"url":           "https://example.com/hook",
		"body_template": `{{.Items.`,
	}
	if err := (webhookChannel{}).Validate(cfg); err == nil {
		t.Fatal("Template syntax errors must be rejected when saving")
	}
	// Even if validation is bypassed, delivery must fail permanently rather than retry.
	_, err := (webhookChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("Invalid templates must fail permanently, got %v", err)
	}
}

// TestTemplateCanOnlyProduceJSON requires valid JSON output, also preventing
// templates from generating plaintext intended to trigger other protocols.
func TestTemplateCanOnlyProduceJSON(t *testing.T) {
	// Valid templates pass.
	ok := map[string]any{"url": "https://example.com/hook", "body_template": `{"t":{{json .Title}}}`}
	if err := (webhookChannel{}).Validate(ok); err != nil {
		t.Fatalf("Valid template must pass validation: %v", err)
	}
	// Reject non-JSON rendered output rather than sending it unchanged.
	bad := map[string]any{"url": "http://127.0.0.1:1/hook", "body_template": `not json {{.Count}}`}
	_, err := (webhookChannel{}).Send(context.Background(), bad, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("Rendering non-JSON must fail permanently, got %v", err)
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Errorf("Error must identify the JSON issue, got %v", err)
	}
}
