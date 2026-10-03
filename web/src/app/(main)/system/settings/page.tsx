"use client";

import * as React from "react";

import { CpuIcon, FlaskConicalIcon, KeyboardIcon, RadioTowerIcon, SearchIcon, ShieldAlertIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { api } from "@/lib/api";
import { CHAT_SEND_MODE_OPTIONS, type ChatSendMode, setChatSendMode, useChatSendMode } from "@/lib/chat-send-mode";
import type { Settings } from "@/lib/types";

import { UpdateCard } from "./_components/update-card";

export default function SystemSettingsPage() {
  const [trafficCapture, setTrafficCapture] = React.useState(false);
  const [agentTrafficBinding, setAgentTrafficBinding] = React.useState(false);
  const [webSearch, setWebSearch] = React.useState(false);
  const [backend, setBackend] = React.useState("ddgs");
  const [braveKeySet, setBraveKeySet] = React.useState(false);
  const [braveKeyInput, setBraveKeyInput] = React.useState("");
  const [tavilyKeySet, setTavilyKeySet] = React.useState(false);
  const [tavilyKeyInput, setTavilyKeyInput] = React.useState("");
  const [savingTavilyKey, setSavingTavilyKey] = React.useState(false);
  const [proxyInput, setProxyInput] = React.useState("");
  const [savingProxy, setSavingProxy] = React.useState(false);
  const [globalProxyInput, setGlobalProxyInput] = React.useState("");
  const [savingGlobalProxy, setSavingGlobalProxy] = React.useState(false);
  const [testing, setTesting] = React.useState(false);
  const [loaded, setLoaded] = React.useState(false);
  const [saving, setSaving] = React.useState(false);
  const [savingKey, setSavingKey] = React.useState(false);
  const [pyInterp, setPyInterp] = React.useState("");
  const [workers, setWorkers] = React.useState("3");
  const [savingWorkers, setSavingWorkers] = React.useState(false);
  // Operation constraint injection scope; all enabled by default.
  const [injectPlanner, setInjectPlanner] = React.useState(true);
  const [injectWorker, setInjectWorker] = React.useState(true);
  // Experimental noa context compression; disabled by default.
  const [noaCompaction, setNoaCompaction] = React.useState(false);
  // Frontend-only preference: read/write localStorage directly, without /api/settings.
  const sendMode = useChatSendMode();

  const apply = React.useCallback((s: Settings) => {
    setTrafficCapture(!!s.traffic_capture);
    setAgentTrafficBinding(!!s.agent_traffic_binding);
    setWebSearch(!!s.web_search_enabled);
    setBackend(s.web_search_backend || "ddgs");
    setBraveKeySet(!!s.brave_key_set);
    setTavilyKeySet(!!s.tavily_key_set);
    setProxyInput(s.web_search_proxy ?? "");
    setGlobalProxyInput(s.global_proxy ?? "");
    setPyInterp(s.python_interpreter ?? "");
    setWorkers(String(s.workers ?? 3));
    setInjectPlanner(s.constraints_inject_planner !== false);
    setInjectWorker(s.constraints_inject_worker !== false);
    setNoaCompaction(!!s.noa_compaction);
  }, []);

  const saveWorkers = () => {
    const n = Number(workers);
    if (!Number.isInteger(n) || n <= 0) {
      toast.error("Concurrency must be a positive integer");
      return;
    }
    setSavingWorkers(true);
    api
      .setSettings({ workers: n })
      .then((s) => {
        apply(s);
        toast.success("Worker concurrency saved; applies to subsequently started tasks");
      })
      .catch((e) => toast.error(`Save failed: ${(e as Error).message}`))
      .finally(() => setSavingWorkers(false));
  };

  const savePython = () => {
    setSaving(true);
    api
      .setSettings({ python_interpreter: pyInterp.trim() })
      .then((s) => {
        apply(s);
        toast.success("Python interpreter settings saved");
      })
      .catch((e) => toast.error(`Save failed: ${(e as Error).message}`))
      .finally(() => setSaving(false));
  };
  const detectPython = () => {
    setSaving(true);
    api
      .detectPython()
      .then((r) => setPyInterp(r.python_interpreter))
      .catch(() => undefined)
      .finally(() => setSaving(false));
  };

  React.useEffect(() => {
    api
      .settings()
      .then(apply)
      .catch(() => undefined)
      .finally(() => setLoaded(true));
  }, [apply]);

  const toggleTraffic = (v: boolean) => {
    setTrafficCapture(v); // optimistic
    setSaving(true);
    api
      .setSettings({ traffic_capture: v })
      .then(apply)
      .catch(() => setTrafficCapture(!v)) // revert on failure
      .finally(() => setSaving(false));
  };

  const toggleInjectPlanner = (v: boolean) => {
    setInjectPlanner(v); // optimistic
    api
      .setSettings({ constraints_inject_planner: v })
      .then(apply)
      .catch(() => setInjectPlanner(!v)); // revert on failure
  };

  const toggleAgentTrafficBinding = (v: boolean) => {
    setAgentTrafficBinding(v);
    setSaving(true);
    api
      .setSettings({ agent_traffic_binding: v })
      .then((s) => {
        apply(s);
        toast.success(v ? "Automatic agent traffic binding enabled" : "Automatic agent traffic binding disabled");
      })
      .catch((e) => {
        setAgentTrafficBinding(!v);
        toast.error(`Save failed: ${(e as Error).message}`);
      })
      .finally(() => setSaving(false));
  };

  const toggleInjectWorker = (v: boolean) => {
    setInjectWorker(v); // optimistic
    api
      .setSettings({ constraints_inject_worker: v })
      .then(apply)
      .catch(() => setInjectWorker(!v)); // revert on failure
  };

  const toggleNoaCompaction = (v: boolean) => {
    setNoaCompaction(v); // optimistic
    api
      .setSettings({ noa_compaction: v })
      .then((s) => {
        apply(s);
        toast.success(
          v
            ? "noa context compression enabled; applies to subsequent runs"
            : "noa context compression disabled; built-in compression restored",
        );
      })
      .catch((e) => {
        setNoaCompaction(!v); // revert on failure
        toast.error(`Save failed: ${(e as Error).message}`);
      });
  };

  // Persist a web-search patch (enable and/or backend). Optimistic with refetch.
  const saveWebSearch = (patch: Partial<Settings>) => {
    setSaving(true);
    api
      .setSettings(patch)
      .then((s) => {
        apply(s);
        toast.success("Web search settings saved");
      })
      .catch((e) => {
        toast.error(`Save failed: ${(e as Error).message}`);
        api
          .settings()
          .then(apply)
          .catch(() => undefined);
      })
      .finally(() => setSaving(false));
  };

  const saveBraveKey = () => {
    setSavingKey(true);
    api
      .setSettings({ brave_search_api_key: braveKeyInput })
      .then((s) => {
        apply(s);
        setBraveKeyInput("");
        toast.success("Brave API key saved");
      })
      .catch((e) => toast.error(`Save failed: ${(e as Error).message}`))
      .finally(() => setSavingKey(false));
  };

  const saveTavilyKey = () => {
    setSavingTavilyKey(true);
    api
      .setSettings({ tavily_search_api_key: tavilyKeyInput })
      .then((s) => {
        apply(s);
        setTavilyKeyInput("");
        toast.success("Tavily API key saved");
      })
      .catch((e) => toast.error(`Save failed: ${(e as Error).message}`))
      .finally(() => setSavingTavilyKey(false));
  };

  const saveProxy = () => {
    setSavingProxy(true);
    api
      .setSettings({ web_search_proxy: proxyInput.trim() })
      .then((s) => {
        apply(s);
        toast.success(proxyInput.trim() ? "Outbound proxy saved" : "Outbound proxy cleared; using direct connections");
      })
      .catch((e) => toast.error(`Save failed: ${(e as Error).message}`))
      .finally(() => setSavingProxy(false));
  };

  const saveGlobalProxy = () => {
    setSavingGlobalProxy(true);
    api
      .setSettings({ global_proxy: globalProxyInput.trim() })
      .then((s) => {
        apply(s);
        toast.success(
          globalProxyInput.trim() ? "Global proxy saved" : "Global proxy cleared; using direct connections",
        );
      })
      .catch((e) => toast.error(`Save failed: ${(e as Error).message}`))
      .finally(() => setSavingGlobalProxy(false));
  };

  // Run a real "test" search ("test") against the CURRENT form values (backend +
  // proxy + entered key), falling back to saved values server-side. Toasts result.
  const runTest = () => {
    setTesting(true);
    api
      .testWebSearch({
        web_search_backend: backend,
        web_search_proxy: proxyInput.trim(),
        brave_search_api_key: braveKeyInput,
        tavily_search_api_key: tavilyKeyInput,
      })
      .then((r) => {
        if (r.ok) toast.success(`Search test passed | ${r.backend} returned ${r.count} results`);
        else toast.error(`Search test failed: ${r.error || "Unknown error"}`);
      })
      .catch((e) => toast.error(`Search test failed: ${(e as Error).message}`))
      .finally(() => setTesting(false));
  };

  // brave-free selected but no key stored and none being entered → tool stays off.
  const braveNeedsKey = webSearch && backend === "brave-free" && !braveKeySet;

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div>
        <h1 className="font-semibold text-xl tracking-tight">Settings</h1>
        <p className="text-muted-foreground text-sm">Global runtime settings</p>
      </div>

      {/* Use columns rather than grid: the web search card is much taller and varies with the selected backend
          because Brave/Tavily key inputs render conditionally. Grid would size the whole row to the tallest card,
          leaving large gaps. Columns balance by content height. Use mb for vertical spacing;
          column-gap controls only horizontal spacing, so children provide their own vertical margins. */}
      <div className="columns-1 gap-4 md:gap-6 lg:columns-2">
        <UpdateCard />

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <RadioTowerIcon className="size-4" />
              Traffic capture
            </CardTitle>
            <CardDescription>
              When enabled, all agent HTTP traffic passes through the recording proxy and is persisted. Agents receive
              traffic_search / traffic_get tools and proxy configuration, including proxy instructions in their prompts.
              <br />
              When disabled (the default), no traffic is recorded. Agents receive <b>no</b> proxy configuration or
              traffic tools, and their prompts contain <b>no</b> proxy instructions. Toggling this immediately rebuilds
              agents to apply the change.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-4">
            <Label htmlFor="traffic-capture" className="font-normal text-muted-foreground text-sm">
              {trafficCapture
                ? "Enabled | Recording traffic and injecting proxy settings"
                : "Disabled | No recording or proxy injection"}
            </Label>
            <Switch
              id="traffic-capture"
              checked={trafficCapture}
              disabled={!loaded || saving}
              onCheckedChange={toggleTraffic}
            />
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <RadioTowerIcon className="size-4" />
              Automatic agent traffic binding
            </CardTitle>
            <CardDescription id="agent-traffic-binding-description">
              Disabled by default. When enabled, the reporter triggered by a new finding reviews recorded HTTP
              requests/responses and links matching traffic before writing the report.
              <b>Reviewing traffic and making additional tool calls increases token usage.</b>
              <br />
              Reporting still works for TCP, uncaptured traffic, or no matching records. This setting does not affect
              traffic capture, manual binding, or saved evidence. It applies to the next agent run; disabling it
              immediately rejects new automatic bindings.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-4">
            <Label htmlFor="agent-traffic-binding" className="font-normal text-muted-foreground text-sm">
              {agentTrafficBinding ? "Enabled | Increases token usage" : "Disabled | Manual binding remains available"}
            </Label>
            <Switch
              id="agent-traffic-binding"
              aria-describedby="agent-traffic-binding-description"
              checked={agentTrafficBinding}
              disabled={!loaded || saving}
              onCheckedChange={toggleAgentTrafficBinding}
            />
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <RadioTowerIcon className="size-4" />
              Global proxy
            </CardTitle>
            <CardDescription>
              All agents send <b>target traffic</b> through this proxy to hide the source IP or use a relay. Supports{" "}
              <b>http / https / socks5</b> with optional <code>user:pass</code> authentication. Leave blank for direct
              connections.
              <br />
              With <b>traffic capture</b> enabled, this is the recording proxy's <b>upstream</b>: traffic is recorded
              before forwarding. With capture disabled, inject it directly into agent bash / WebFetch requests.
              Independent of web search and LLM proxies.
              <br />
              <b>Note</b>: with <b>capture disabled</b>, socks5 depends on each command-line tool supporting{" "}
              <code>ALL_PROXY</code> (curl supports it, while some tools may ignore it). If socks5 is your primary
              proxy, enable traffic capture so MITM establishes the connection directly and applies the proxy
              consistently without tool support.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            <Label htmlFor="global-proxy" className="font-normal text-muted-foreground text-sm">
              Proxy URL
            </Label>
            <div className="flex items-center gap-2">
              <Input
                id="global-proxy"
                autoComplete="off"
                placeholder="socks5://user:pass@host:1080 or http://host:port (blank=direct)"
                value={globalProxyInput}
                disabled={!loaded || savingGlobalProxy}
                onChange={(e) => setGlobalProxyInput(e.target.value)}
              />
              <Button type="button" onClick={saveGlobalProxy} disabled={!loaded || savingGlobalProxy}>
                Save
              </Button>
            </div>
            <p className="text-muted-foreground text-xs">
              {globalProxyInput.trim()
                ? "Configured | All target traffic uses this proxy"
                : "Not configured | Target traffic connects directly"}
            </p>
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <ShieldAlertIcon className="size-4" />
              Operation constraint injection
            </CardTitle>
            <CardDescription>
              When enabled, append each task's <b>operation constraints</b> (allow/deny entries maintained in the task
              overview) to the corresponding agent's system prompt to define exploration boundaries, such as testing
              only the current port or prohibiting brute force.
              <br />
              Control injection separately for the <b>planner</b> and <b>worker</b>. Both are enabled by default.
              Changes are read on the next turn without rebuilding agents. Disabling injection hides constraints from
              that agent.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor="inject-planner" className="font-normal text-muted-foreground text-sm">
                Inject into planner{injectPlanner ? " | Enabled" : " | Disabled"}
              </Label>
              <Switch
                id="inject-planner"
                checked={injectPlanner}
                disabled={!loaded}
                onCheckedChange={toggleInjectPlanner}
              />
            </div>
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor="inject-worker" className="font-normal text-muted-foreground text-sm">
                Inject into worker{injectWorker ? " | Enabled" : " | Disabled"}
              </Label>
              <Switch
                id="inject-worker"
                checked={injectWorker}
                disabled={!loaded}
                onCheckedChange={toggleInjectWorker}
              />
            </div>
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <FlaskConicalIcon className="size-4" />
              Experimental features
            </CardTitle>
            <CardDescription>
              Features under evaluation, disabled by default. They may change agent behavior or stability; enable them
              only after understanding their effects.
              <br />
              <b>noa context compression</b>: model-driven compression of long conversation histories (norma v0.4.0).
              The four integrated agent types (<b>planner / worker / main agent / chat</b>) use noa instead of built-in
              compression. Original content is archived in the task workspace for traceability. Changes apply to
              subsequent runs without rebuilding agents; disabling restores built-in compression immediately.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-4">
            <Label htmlFor="noa-compaction" className="font-normal text-muted-foreground text-sm">
              noa context compression{noaCompaction ? " | Enabled" : " | Disabled"}
            </Label>
            <Switch
              id="noa-compaction"
              checked={noaCompaction}
              disabled={!loaded}
              onCheckedChange={toggleNoaCompaction}
            />
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <SearchIcon className="size-4" />
              Web search
            </CardTitle>
            <CardDescription>
              This is the web search <b>master switch and provider setting</b>. Enable it before choosing whether each
              agent may use
              <b>web_search</b> in its settings. Search returns titles, links, and summaries; WebFetch retrieves full
              content. Search <b>does not use</b>
              the recording proxy and is independent of traffic capture.
              <br />
              Choose <b>DuckDuckGo (ddgs)</b> (no key), <b>Brave (free tier)</b> (Brave API key required), <b>Tavily</b>{" "}
              (Tavily API key required), or <b>DeepSeek</b> (reuses the active LLM profile). When the master switch is
              off, per-agent web search controls are unavailable.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor="web-search" className="font-normal text-muted-foreground text-sm">
                {webSearch
                  ? "Master switch enabled | Configure access per agent"
                  : "Disabled | Agents cannot enable web search"}
              </Label>
              <Switch
                id="web-search"
                checked={webSearch}
                disabled={!loaded || saving}
                onCheckedChange={(v) => {
                  setWebSearch(v); // optimistic
                  saveWebSearch({ web_search_enabled: v });
                }}
              />
            </div>

            {webSearch && (
              <div className="flex items-center justify-between gap-4">
                <Label className="font-normal text-muted-foreground text-sm">Search provider</Label>
                <Select
                  value={backend}
                  disabled={!loaded || saving}
                  onValueChange={(v) => {
                    setBackend(v); // optimistic
                    saveWebSearch({ web_search_backend: v });
                  }}
                >
                  <SelectTrigger className="w-48 shrink-0">
                    <SelectValue placeholder="Select a provider" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="ddgs">DuckDuckGo (ddgs | Free, no key)</SelectItem>
                    <SelectItem value="brave-free">Brave (free tier | Key required)</SelectItem>
                    <SelectItem value="tavily">Tavily (key required)</SelectItem>
                    <SelectItem value="deepseek">DeepSeek (official)</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            )}

            {webSearch && backend === "deepseek" && (
              <div className="flex flex-col gap-2 rounded-md border border-border/60 bg-muted/30 p-3">
                <p className="font-medium text-sm">Official DeepSeek web search</p>
                <p className="text-muted-foreground text-xs leading-relaxed">
                  This provider reuses the <b>currently active LLM profile</b>, so it
                  <b>supports only official DeepSeek models</b>, and the profile <b>must use the anthropic protocol</b>.
                  DeepSeek's OpenAI endpoint does not support server-side search. Changing LLM profiles may make this
                  provider unavailable.
                </p>
                <p className="text-muted-foreground text-xs leading-relaxed">
                  Unlike other providers, search runs <b>on DeepSeek's servers</b>. Each search makes an additional
                  model call, incurring token charges. Search requests <b>bypass the outbound proxy above</b> and{" "}
                  <b>are not recorded as traffic evidence</b>. Results contain <b>only titles and links</b>
                  without summaries; use WebFetch to retrieve full content.
                </p>
                <p className="text-muted-foreground text-xs leading-relaxed">
                  You are responsible for meeting these conditions; the system does not enforce them. Use Test search
                  below to verify with a real request.
                </p>
              </div>
            )}

            {webSearch && backend === "brave-free" && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="brave-key" className="font-normal text-muted-foreground text-sm">
                  Brave Search API Key
                  {braveKeySet && <span className="ml-2 text-emerald-500 text-xs">Configured</span>}
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="brave-key"
                    type="password"
                    autoComplete="off"
                    placeholder={braveKeySet ? "Configured (leave blank to keep)" : "Enter Brave API key"}
                    value={braveKeyInput}
                    disabled={!loaded || savingKey}
                    onChange={(e) => setBraveKeyInput(e.target.value)}
                  />
                  <Button
                    type="button"
                    onClick={saveBraveKey}
                    disabled={!loaded || savingKey || braveKeyInput.trim() === ""}
                  >
                    Save
                  </Button>
                </div>
                {braveNeedsKey && (
                  <p className="text-amber-500 text-xs">
                    Brave is selected but no API key is configured. Search remains disabled until a key is saved.
                  </p>
                )}
                <p className="text-muted-foreground text-xs">
                  The free tier provides about 2,000 requests per month. Get a key at https://brave.com/search/api/.
                </p>
              </div>
            )}

            {webSearch && backend === "tavily" && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="tavily-key" className="font-normal text-muted-foreground text-sm">
                  Tavily Search API Key
                  {tavilyKeySet && <span className="ml-2 text-emerald-500 text-xs">Configured</span>}
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="tavily-key"
                    type="password"
                    autoComplete="off"
                    placeholder={tavilyKeySet ? "Configured (leave blank to keep)" : "Enter Tavily API key (tvly-...)"}
                    value={tavilyKeyInput}
                    disabled={!loaded || savingTavilyKey}
                    onChange={(e) => setTavilyKeyInput(e.target.value)}
                  />
                  <Button
                    type="button"
                    onClick={saveTavilyKey}
                    disabled={!loaded || savingTavilyKey || tavilyKeyInput.trim() === ""}
                  >
                    Save
                  </Button>
                </div>
                {webSearch && backend === "tavily" && !tavilyKeySet && (
                  <p className="text-amber-500 text-xs">
                    Tavily is selected but no API key is configured. Search remains disabled until a key is saved.
                  </p>
                )}
                <p className="text-muted-foreground text-xs">Register at https://tavily.com to get an API key.</p>
              </div>
            )}

            {webSearch && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="ws-proxy" className="font-normal text-muted-foreground text-sm">
                  Outbound proxy (optional)
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="ws-proxy"
                    autoComplete="off"
                    placeholder="http://host:port or socks5://host:port (blank=direct)"
                    value={proxyInput}
                    disabled={!loaded || savingProxy}
                    onChange={(e) => setProxyInput(e.target.value)}
                  />
                  <Button type="button" onClick={saveProxy} disabled={!loaded || savingProxy}>
                    Save
                  </Button>
                </div>
                <p className="text-muted-foreground text-xs">
                  A separate outbound proxy for search endpoints (VPN/SOCKS, etc.), independent of the recording MITM
                  proxy. Use it when direct access is unavailable.
                </p>
              </div>
            )}

            {webSearch && (
              <div className="flex items-center justify-between gap-4 border-t pt-4">
                <p className="text-muted-foreground text-xs">
                  Search for "test" using the current provider, proxy, and key to verify access.
                </p>
                <Button
                  type="button"
                  variant="outline"
                  onClick={runTest}
                  disabled={!loaded || testing}
                  className="shrink-0"
                >
                  {testing ? "Testing..." : "Test search"}
                </Button>
              </div>
            )}
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <RadioTowerIcon className="size-4" />
              Custom scripts | Python interpreter
            </CardTitle>
            <CardDescription>
              Custom <b>script</b> tools use this interpreter for Python. Startup detection prefers python3; enter an
              absolute path to a virtual environment or specific version, or leave blank for runtime detection.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <Input
                className="font-mono text-sm"
                placeholder="/usr/bin/python3 (blank=auto-detect)"
                value={pyInterp}
                disabled={!loaded || saving}
                onChange={(e) => setPyInterp(e.target.value)}
              />
              <Button variant="outline" onClick={detectPython} disabled={!loaded || saving}>
                Detect again
              </Button>
              <Button onClick={savePython} disabled={!loaded || saving}>
                Save
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <CpuIcon className="size-4" />
              Worker concurrency
            </CardTitle>
            <CardDescription>
              Concurrent worker agents per task, default 3. Higher values increase simultaneous exploration and resource
              usage. Changes
              <b>apply to subsequently started tasks</b>; running tasks are unaffected.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min={1}
                className="w-32 font-mono text-sm"
                placeholder="3"
                value={workers}
                disabled={!loaded || savingWorkers}
                onChange={(e) => setWorkers(e.target.value)}
              />
              <Button onClick={saveWorkers} disabled={!loaded || savingWorkers}>
                Save
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <KeyboardIcon className="size-4" />
              Conversation send shortcut
            </CardTitle>
            <CardDescription>
              Shared by Chat and the main-agent composer in task details. Changes apply immediately without saving.
              <br />
              This preference is stored <b>only in this browser</b>, without account synchronization. Set it again after
              switching browsers or clearing site data.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-4">
            <Label htmlFor="chat-send-mode" className="font-normal text-muted-foreground text-sm">
              Send shortcut
            </Label>
            <Select value={sendMode} onValueChange={(v) => setChatSendMode(v as ChatSendMode)}>
              <SelectTrigger id="chat-send-mode" className="w-72">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {CHAT_SEND_MODE_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
