"use client";

// Shared pieces for LLM retry config: each of the five retry layers' "attempts + interval".
//
// The five layers, inner to outer: connect (SDK) -> empty response (SDK) -> same-provider safe window -> pool circuit breaker -> intent re-run.
// The first three follow the endpoint, so each model profile can override the global default; the last two are process-level, with a single global copy.
//
// Every input follows the same "empty = not configured" semantics, matching the backend db.RetryRule:
//   attempts   empty/0 = use the built-in default | -1 = disable this retry layer | >0 = use this count
//   interval   empty/0 = use this layer's own exponential backoff | >0 = use this fixed interval in milliseconds

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
  /** Where this retry happens and who runs it */
  where: string;
  /** What kind of error reaches this layer -- down to the status code, so nobody has to guess */
  trigger: string;
  /** Errors that look similar but do NOT go through this layer, so a setting that has no effect isn't mistaken for a bug */
  skips?: string;
  desc: string;
  attemptsLabel: string;
  /** Default used when attempts is empty, for the placeholder */
  defAttempts: number;
  /** Default strategy used when interval is empty, for the placeholder */
  defInterval: string;
  /** Meaning of setting attempts to -1 */
  offHint: string;
};

export const RETRY_LAYERS = {
  connect: {
    title: "Connect retry",
    where: "SDK · before a 200 is received",
    trigger:
      "Can't connect or no 200 yet: connection reset / read-write timeout / DNS failure and other network-layer errors, plus HTTP 408, 429, 500, 502, 503, 504.",
    skips:
      "Other status codes (400 / 401 / 403 / 404 / 413 / 422, etc.) are deterministic rejections that would fail the same way on resend, so they're thrown straight up.",
    desc: "Resend the same request as-is. Once the stream has started (a 200 is received), a mid-stream disconnect is no longer this layer's concern.",
    attemptsLabel: "Retry attempts",
    defAttempts: 3,
    defInterval: "0.5s->1s->2s exponential (capped at 8s)",
    offHint: "-1 = no retries at all, throw immediately on failure",
  },
  empty: {
    title: "Empty response retry",
    where: "SDK · openai format only",
    trigger:
      "HTTP 200, finish_reason is a normal stop, but the whole response has no content block at all -- a gateway empty frame, a dropped thinking-field frame, or a sampling hiccup all look like this.",
    skips:
      "Content missing due to a max_tokens cutoff doesn't count (that's solved by raising the output limit; resending would just hit it again).",
    desc: "Resends the whole prompt, so it's expensive on long context -- don't set the count high.",
    attemptsLabel: "Retry attempts",
    defAttempts: 2,
    defInterval: "0.5s->1s->2s exponential (capped at 8s)",
    offHint: "-1 = hand back the empty response as-is",
  },
  stream: {
    title: "Same-provider safe-window retry",
    where: "This project · before any output is delivered",
    trigger:
      "Something goes wrong only after the stream is established (a 200 was received): a mid-stream disconnect, the provider being overloaded, or 429 / 5xx error events within the stream -- and not a single token has been handed to the caller yet.",
    skips:
      "Quota exhausted (402 / insufficient_quota, left to the pool to switch profiles), context too long (413 / context length, left to compaction), and 400 / 401 / 403 / 404 / 422 deterministic rejections are all not retried.",
    desc: "Replays the same request on the same profile. Because no output has been delivered yet, the replay doesn't duplicate model output or tool execution.",
    attemptsLabel: "Retry attempts",
    defAttempts: 2,
    defInterval: "0.5s->1s exponential (capped at 4s)",
    offHint: "-1 = hand a broken stream straight to the outer intent re-run",
  },
  breaker: {
    title: "Pool circuit breaker",
    where: "This project · process-level, a single global copy",
    trigger:
      "Trips when transient failures (429, 5xx, network errors) accumulate consecutively to the threshold; deterministic failures like insufficient balance (402), invalid key (401 / 403), and model not found (404) ignore the threshold and trip on the first occurrence.",
    skips:
      "A single success resets the count, so an occasionally flaky profile isn't slowly accumulated into tripping.",
    desc: "After tripping it enters a cooldown, during which the pool skips this profile outright. The state is persisted, so it survives a restart.",
    attemptsLabel: "Consecutive failures before tripping",
    defAttempts: 3,
    defInterval: "1min->5min->30min gradient",
    offHint: "-1 = transient failures never trip (deterministic failures still trip)",
  },
  intent: {
    title: "Intent re-run",
    where: "This project · process-level, a single global copy",
    trigger:
      "None of the earlier layers caught it: the worker ends in model_error -- the inner retries are all exhausted, or the stream broke only after it had started delivering output (replay isn't safe then, so the whole thing must re-run).",
    skips:
      "Quota exhaustion is already handled by the pool switching profiles, so it isn't re-run here; when a task is paused / stopped / entering wrap-up it yields immediately and doesn't consume backoff time.",
    desc: "Re-runs the whole intent from the start. It's the outermost layer, so one re-run means the counts of the inner layers are multiplied again.",
    attemptsLabel: "Re-run attempts",
    defAttempts: 2,
    defInterval: "fixed 3s",
    offHint: "-1 = no re-run, the intent is judged blocked directly",
  },
} satisfies Record<string, LayerMeta>;

type LayerKey = keyof typeof RETRY_LAYERS;

/** Human-readable milliseconds, only echoed next to the input so you don't have to count zeros. */
function humanMs(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) return "";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${Number((ms / 1000).toFixed(2))}s`;
  return `${Number((ms / 60_000).toFixed(2))}min`;
}

/** Controlled number input: empty string <-> 0, intermediate states ("-", "1e") stay local as-is without disturbing the parent. */
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
  // Catch up when the parent swaps in a whole new set of values (policy loaded, profile switched); typing yourself doesn't reach here,
  // because by then value already equals the parsed result of the local text.
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

/** The two knobs for one retry layer. idPrefix keeps label htmlFor unique when this appears multiple times on one page. */
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
  /** true = the compact version in the config drawer: drops the expanded explanation, keeping only the "what errors reach this layer" line */
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
        {/* Which errors reach this layer, down to the status code -- if a knob has no visible effect, the error most likely doesn't fall into this layer at all. */}
        <p className="text-muted-foreground text-xs">
          <span className="font-medium text-foreground">Trigger</span>: {meta.trigger}
        </p>
        {!compact && meta.skips && (
          <p className="text-muted-foreground text-xs">
            <span className="font-medium text-foreground">Not this layer</span>: {meta.skips}
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
            Interval ms
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
      {!compact && <p className="text-muted-foreground text-xs">Empty = use the default; {meta.offHint}.</p>}
    </div>
  );
}

/** The three-layer override in the model config drawer (the three that follow the endpoint). */
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
        <Label className="text-sm">Retry override</Label>
        <p className="text-muted-foreground text-xs">
          Applies only to this profile, overriding the global defaults under "Retry and backoff". Leave a field empty =
          follow global; attempts -1 = turn off this retry layer; an interval, once set, replaces the exponential
          backoff with a fixed interval. The circuit breaker and intent re-run are process-level and can only be tuned
          on the global page.
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

/** The "Retry and backoff" tab: the global defaults for all five layers. */
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
      toast.error(`Failed to load retry policy: ${(e as Error).message}`);
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
      // The backend clamps out-of-range values back into range and returns them; refresh straight from the returned value, so what you see is what's stored.
      const saved = await api.saveLLMRetryPolicy(policy);
      setPolicy({ ...ZERO_POLICY, ...saved });
      toast.success("Saved, effective immediately (the call round currently running still uses the old parameters)");
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
        <Loader2Icon className="size-4 animate-spin" /> Loading retry policy…
      </div>
    );
  }

  return (
    <div className="grid gap-4">
      <div className="rounded-lg border bg-muted/30 p-3 text-muted-foreground text-xs leading-relaxed">
        A failed model call passes through five retry layers in turn, inner to outer:
        <span className="text-foreground">
          {" "}
          connect → empty response → same-provider safe window → pool circuit breaker → intent re-run
        </span>
        . The outer layer only takes over once the inner one is exhausted, so the counts
        <span className="text-foreground">multiply</span>
        -- max out every layer and a single hiccup can burn dozens of requests. Leaving everything empty is the current
        default, behaving exactly as if this page didn't exist. The first three layers can be overridden per model
        profile.
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
          Restore all defaults
        </Button>
      </div>
    </div>
  );
}
