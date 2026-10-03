"use client";

// Shared LLM retry controls: attempt count and interval for each of five layers.
//
// From inner to outer: connection (SDK), empty response (SDK), same-provider safe window, failover circuit breaker, intent rerun.
// The first three are endpoint-specific and support per-profile overrides; the last two are process-wide and global only.
//
// All inputs use blank=unconfigured, matching the backend db.RetryRule:
//   attempts: blank/0=built-in default | -1=disable this layer | >0=use this count
//   interval: blank/0=the layer's default exponential backoff | >0=fixed milliseconds

import * as React from "react";

import { Loader2Icon, SaveIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import type { LLMRetryOverride, LLMRetryPolicy, LLMRetryRule } from "@/lib/types";

export const ZERO_RULE: LLMRetryRule = { attempts: 0, interval_ms: 0 };
export const ZERO_OVERRIDE: LLMRetryOverride = {
  connect: ZERO_RULE,
  empty: ZERO_RULE,
  stream: ZERO_RULE,
};
const ZERO_POLICY: LLMRetryPolicy = {
  ...ZERO_OVERRIDE,
  breaker: ZERO_RULE,
  intent: ZERO_RULE,
};

type LayerMeta = {
  title: string;
  /** Where this retry layer runs and who executes it. */
  where: string;
  /** Errors handled by this layer, with explicit status codes. */
  trigger: string;
  /** Similar errors that bypass this layer, so ineffective settings are not mistaken for bugs. */
  skips?: string;
  desc: string;
  attemptsLabel: string;
  /** Default attempt count for the placeholder. */
  defAttempts: number;
  /** Default interval policy for the placeholder. */
  defInterval: string;
  /** Meaning of setting attempts to -1. */
  offHint: string;
};

export const RETRY_LAYERS = {
  connect: {
    title: "Connection retries",
    where: "SDK | Before HTTP 200",
    trigger:
      "Before HTTP 200: connection resets, read/write timeouts, DNS failures, other network errors, and HTTP 408, 429, 500, 502, 503, or 504.",
    skips:
      "Other codes (400 / 401 / 403 / 404 / 413 / 422, etc.) are deterministic rejections; retrying would fail again, so propagate them immediately.",
    desc: "Resend the identical request. Once the stream starts after HTTP 200, disconnections are handled by other layers.",
    attemptsLabel: "Retry attempts",
    defAttempts: 3,
    defInterval: "0.5s, 1s, 2s exponential backoff (8s cap)",
    offHint: "-1=no retries; propagate failures immediately",
  },
  empty: {
    title: "Empty-response retries",
    where: "SDK | OpenAI format only",
    trigger:
      "HTTP 200 with finish_reason=stop, but no content blocks. This can result from empty gateway frames, dropped reasoning fields, or sampling glitches.",
    skips:
      "Excludes empty responses caused by max_tokens truncation. Raise the output limit; retrying would hit the same limit.",
    desc: "Resends the entire prompt, which can be expensive with long contexts. Keep the attempt count modest.",
    attemptsLabel: "Retry attempts",
    defAttempts: 2,
    defInterval: "0.5s, 1s, 2s exponential backoff (8s cap)",
    offHint: "-1=return empty responses unchanged",
  },
  stream: {
    title: "Same-provider safe-window retries",
    where: "ARTEX | Before output is delivered",
    trigger:
      "Failures after HTTP 200, including interrupted connections, provider overloaded errors, or in-stream 429/5xx events, before any token has reached the caller.",
    skips:
      "No retries for exhausted quota (402 / insufficient_quota, handled by failover), oversized context (413 / context length, handled by compression), or deterministic 400 / 401 / 403 / 404 / 422 rejections.",
    desc: "Replay the same request on the same profile. No output has been delivered, so replay does not duplicate model output or tool execution.",
    attemptsLabel: "Retry attempts",
    defAttempts: 2,
    defInterval: "0.5s, 1s exponential backoff (4s cap)",
    offHint: "-1=delegate interrupted streams directly to the outer intent-rerun layer",
  },
  breaker: {
    title: "Failover circuit breaker",
    where: "ARTEX | Process-wide, global only",
    trigger:
      "Consecutive transient failures (429, 5xx, network errors) open the circuit at the threshold. Deterministic failures such as exhausted credit (402), invalid keys (401/403), or missing models (404) open it immediately.",
    skips: "A successful call clears the counter, so occasional failures do not accumulate indefinitely.",
    desc: "An open circuit starts a cooldown during which failover skips the profile. State is persisted across restarts.",
    attemptsLabel: "Consecutive failures before opening",
    defAttempts: 3,
    defInterval: "1min, 5min, 30min stepped cooldown",
    offHint: "-1=transient failures never open the circuit; deterministic failures still do",
  },
  intent: {
    title: "Intent reruns",
    where: "ARTEX | Process-wide, global only",
    trigger:
      "The worker ends with model_error after inner retries are exhausted, or the stream breaks after delivering output, when replay is unsafe and the entire intent must restart.",
    skips:
      "Quota exhaustion is handled by profile failover, not reruns here. Pausing, stopping, or entering task wrap-up interrupts backoff immediately.",
    desc: "Rerun the entire intent from the start. This outermost layer repeats all inner retry budgets, multiplying the total attempts.",
    attemptsLabel: "Rerun attempts",
    defAttempts: 2,
    defInterval: "Fixed 3s",
    offHint: "-1=no reruns; mark the intent blocked immediately",
  },
} satisfies Record<string, LayerMeta>;

type LayerKey = keyof typeof RETRY_LAYERS;

/** Readable milliseconds shown beside inputs to avoid counting zeros. */
function humanMs(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) return "";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${Number((ms / 1000).toFixed(2))}s`;
  return `${Number((ms / 60_000).toFixed(2))}min`;
}

/** Controlled numeric input: empty string maps to 0; keep partial values such as "-" or "1e" local. */
function NumField({
  id,
  value,
  onChange,
  placeholder,
  min,
}: {
  id: string;
  value: number;
  onChange: (n: number) => void;
  placeholder: string;
  min: number;
}) {
  const [text, setText] = React.useState(value === 0 ? "" : String(value));
  // Follow parent updates when a policy loads or the profile changes. Local typing does not enter this path
  // because value already matches the parsed local text.
  React.useEffect(() => {
    const incoming = value === 0 ? "" : String(value);
    setText((cur) => (Number(cur || 0) === value ? cur : incoming));
  }, [value]);
  return (
    <Input
      id={id}
      type="number"
      min={min}
      className="w-28 shrink-0"
      value={text}
      placeholder={placeholder}
      onChange={(e) => {
        setText(e.target.value);
        const n = Number(e.target.value);
        onChange(e.target.value.trim() === "" || !Number.isFinite(n) ? 0 : Math.trunc(n));
      }}
    />
  );
}

/** Two controls for one retry layer. idPrefix keeps labels associated when multiple instances share a page. */
export function RetryRuleFields({
  layer,
  idPrefix,
  value,
  onChange,
  compact,
}: {
  layer: LayerKey;
  idPrefix: string;
  value: LLMRetryRule;
  onChange: (r: LLMRetryRule) => void;
  /** true=compact profile-drawer view; show only which errors this layer handles. */
  compact?: boolean;
}) {
  const meta = RETRY_LAYERS[layer];
  const human = humanMs(value.interval_ms);
  return (
    <div className={compact ? "grid gap-2" : "grid gap-3 rounded-lg border p-3"}>
      <div className="grid gap-0.5">
        <div className="flex flex-wrap items-baseline gap-2">
          <Label className="text-sm">{meta.title}</Label>
          <span className="text-muted-foreground text-xs">{meta.where}</span>
        </div>
        {/* State exact handled errors and status codes; ineffective settings often mean the error belongs to another layer. */}
        <p className="text-muted-foreground text-xs">
          <span className="font-medium text-foreground">Triggers</span>: {meta.trigger}
        </p>
        {!compact && meta.skips && (
          <p className="text-muted-foreground text-xs">
            <span className="font-medium text-foreground">Bypasses this layer</span>: {meta.skips}
          </p>
        )}
        {!compact && <p className="text-muted-foreground text-xs">{meta.desc}</p>}
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="flex items-center gap-2">
          <Label htmlFor={`${idPrefix}-${layer}-n`} className="text-muted-foreground text-xs">
            {meta.attemptsLabel}
          </Label>
          <NumField
            id={`${idPrefix}-${layer}-n`}
            min={-1}
            value={value.attempts}
            placeholder={`Default ${meta.defAttempts}`}
            onChange={(n) => onChange({ ...value, attempts: n })}
          />
        </div>
        <div className="flex items-center gap-2">
          <Label htmlFor={`${idPrefix}-${layer}-ms`} className="text-muted-foreground text-xs">
            Interval (ms)
          </Label>
          <NumField
            id={`${idPrefix}-${layer}-ms`}
            min={0}
            value={value.interval_ms}
            placeholder="Default backoff"
            onChange={(n) => onChange({ ...value, interval_ms: n })}
          />
          <span className="text-muted-foreground text-xs">{human ? `Fixed ${human}` : meta.defInterval}</span>
        </div>
      </div>
      {!compact && <p className="text-muted-foreground text-xs">Blank=default; {meta.offHint}.</p>}
    </div>
  );
}

/** The three endpoint-specific overrides in the model profile drawer. */
export function ProfileRetryFields({
  value,
  onChange,
}: {
  value: LLMRetryOverride;
  onChange: (o: LLMRetryOverride) => void;
}) {
  return (
    <div className="grid gap-3 rounded-lg border p-3">
      <div className="grid gap-0.5">
        <Label className="text-sm">Retry overrides</Label>
        <p className="text-muted-foreground text-xs">
          Applies only to this profile, overriding global Retries and backoff settings. Blank fields inherit global
          values; attempts=-1 disables a layer. A specified interval replaces exponential backoff with a fixed delay.
          Circuit breaker and intent rerun settings are process-wide and can only be changed globally.
        </p>
      </div>
      {(["connect", "empty", "stream"] as const).map((k) => (
        <div key={k} className="border-t pt-3 first:border-t-0 first:pt-0">
          <RetryRuleFields
            compact
            layer={k}
            idPrefix="pf"
            value={value[k]}
            onChange={(r) => onChange({ ...value, [k]: r })}
          />
        </div>
      ))}
    </div>
  );
}

/** Retries and backoff tab: global defaults for all five layers. */
export function RetryPolicyPanel() {
  const [policy, setPolicy] = React.useState<LLMRetryPolicy>(ZERO_POLICY);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const p = await api.llmRetryPolicy();
      setPolicy({ ...ZERO_POLICY, ...p });
    } catch (e) {
      toast.error(`Could not load retry policy: ${(e as Error).message}`);
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void load();
  }, [load]);

  async function save() {
    if (saving) return;
    setSaving(true);
    try {
      // The backend clamps out-of-range values and returns them; refresh from that response to show the saved values.
      const saved = await api.saveLLMRetryPolicy(policy);
      setPolicy({ ...ZERO_POLICY, ...saved });
      toast.success("Saved and effective immediately. The current call keeps its previous parameters.");
    } catch (e) {
      toast.error(`Save failed: ${(e as Error).message}`);
    } finally {
      setSaving(false);
    }
  }

  const set = (k: LayerKey) => (r: LLMRetryRule) => setPolicy((p) => ({ ...p, [k]: r }));

  if (loading) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-dashed p-10 text-muted-foreground text-sm">
        <Loader2Icon className="size-4 animate-spin" /> Loading retry policy...
      </div>
    );
  }

  return (
    <div className="grid gap-4">
      <div className="rounded-lg border bg-muted/30 p-3 text-muted-foreground text-xs leading-relaxed">
        A failed model call passes through five retry layers, from inner to outer:
        <span className="text-foreground">
          {" "}
          Connection to empty response to same-provider safe window to circuit breaker to intent rerun
        </span>
        . Outer layers run only after inner layers are exhausted, so attempt counts
        <span className="text-foreground">multiply</span>; maximizing every layer can turn one transient failure into
        dozens of requests. Leave all fields blank to preserve the existing default behavior. The first three layers can
        be overridden in each model profile.
      </div>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {(Object.keys(RETRY_LAYERS) as LayerKey[]).map((k) => (
          <RetryRuleFields key={k} layer={k} idPrefix="gl" value={policy[k]} onChange={set(k)} />
        ))}
      </div>

      <div className="flex gap-2">
        <Button onClick={save} disabled={saving}>
          {saving ? <Loader2Icon className="animate-spin" /> : <SaveIcon />}
          Save
        </Button>
        <Button variant="outline" onClick={() => setPolicy(ZERO_POLICY)} disabled={saving}>
          Reset all to defaults
        </Button>
      </div>
    </div>
  );
}
