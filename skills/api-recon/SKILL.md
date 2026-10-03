---
name: api-recon
description: Invoke this skill when collecting a website's API endpoints.
---

# API Recon (frontend endpoint reconnaissance)

Under an **authorized** engagement, discover as completely as possible: the **backend API** (paths, methods, parameters, response bodies), the **frontend routes**, and the **UI feature triggers** (tabs, dialogs, table actions, etc.).

---

## Boundaries and prohibitions (Agent must-read · crossing the line is out of scope)

This skill does **API / parameter-surface reconnaissance only**; it is not the vulnerability-hunting or exploitation stage.

### Task boundaries

| Scope | Allowed | Prohibited |
|---|---|---|
| **Goal** | Enumerate paths, methods, parameters, routes, UI triggers | SQLi/XSS/privilege-escalation/brute-force/fuzz vulnerabilities, packet-tampering attacks, destructive operations |
| **Auth** | Hook + stub/mock to bypass the **client-side** login gate | Asking the user for or guessing credentials; attempting a real login-form submission |
| **Runtime** | Hook endpoints without credentials, using mock responses to push the SPA into its logged-in shell | Flows that require a real backend session to proceed |

### Credential-free dynamic analysis (Phase 3 default)

1. Use `preload.js` / `runtime_harvest.js` to **intercept and stub** bootstrap endpoints such as login, permissions, and menus;
2. Return a mock body for business query endpoints that is **structurally correct, carries a success business code, and may hold empty data**;
3. Let the frontend render its logged-in pages even with no backend or a 401, so it triggers more XHR/fetch/WebSocket calls;
4. **Empty data, blank tables, and placeholder UI are all expected** — do not switch to a real login or vulnerability testing because of them.

**In one line**: use mocks to prop up the frontend routes and component mounting, and **record outbound requests only**; what the backend returns does not matter, what matters is **which further endpoints the frontend will call**.

### Hard process prohibitions

| Prohibited | Do instead |
|---|---|
| grep/curl/Read the main entry `index-*.js` to extract API paths before Phase 1 is done | Run `OUTDIR/harvest_static.py` |
| Hand-writing scripts like `extract_apis.py` that replace harvest | Edit `OUTDIR/harvest_static.py` and rerun |
| Repeating the same grep/command after it has failed ≥2 times | Switch strategy: read tool_logs, edit harvest, check the reference |
| Skipping gates A/B and running the original `scripts/` directly | Copy into OUTDIR and adapt to the target |
| Real usernames/passwords, OTP, OAuth, or other authentication | stub/mock (see above) |
| Skipping the stub to "get real data" and doing privilege-escalation/injection testing | Record outbound only; that is the recon boundary |
| Deleting, exporting sensitive data, bulk writes, or other irreversible operations | Same applies to coverage clicks |
| Claiming you have every page and endpoint without finishing runtime + dynamic enumeration | See "Definition of done" or note the limitation |
| Claiming you have mastered every parameter without finishing the parameter-trigger matrix + diff | Phase 3b matrix + Phase 5 diff |
| Inferring required/optional from a single runtime sample | Multi-sample diff, or validation rules / error-based inference |

---

## Two-layer model + run modes

| Layer | Output | Ceiling |
|---|---|---|
| **Static** (JS bundle) | Full endpoint paths, route draft, request-assembly field candidates | No HTTP methods; parameters need Phase 1b; misses URLs assembled at runtime |
| **Runtime** (live session) | Method + body + response + dynamic URL + WS/SSE; multi-sample diff to complete parameters | The page must actually render before it sends requests; a single sample cannot decide required/optional |

| Run mode | Engine | Fits |
|---|---|---|
| **depth** | `runtime_harvest.js` (Puppeteer) | API inventory, METHOD/params/response body, WS/SSE, reproducible batch runs |
| **coverage** | browser + `preload.js` | Click tabs/dialogs/tables for deeper feature-point coverage |
| **both** | depth first, then coverage | Most complete, takes the longest |

**Parameter methodology** (no general-purpose script): find paths with harvest/regex; find parameters with **anchor window expansion + UI binding chain + multi-sample diff + error-based inference** (grep recipes in section J of [reference.md](reference.md)).

---

## Definition of done

Recon may be declared complete only when all of these hold:

- [ ] **Static**: Phase 1 harvest produces `api_static.txt`, `routes.txt`, `js/`
- [ ] **Runtime**: at least one of depth or coverage; coverage/both require **working hooks + a dynamic enumeration loop**
- [ ] **Into the shell**: visiting a business path lands somewhere other than `/login` (watch for hash routing)
- [ ] **Parameters**: coverage/both finish the parameter-trigger matrix + `param_samples.json`; Phase 5 merges `params_merged.json`
- [ ] **Depth** (if module pages are blank): Phase 4 permission-tree reconstruction and a rerun, until **module-level APIs** appear (not just locale/bootstrap)
- [ ] **Delivery**: Phase 5 outputs are complete (see the Phase 5 output table); `insert_assets` writes the service and endpoint assets

---

## Scripts and gates

`scripts/` are reference templates only; running the originals verbatim and treating that as the final result is **prohibited**.

**Rule**: read first → adapt to the target → write to `OUTDIR` (e.g. `recon/`) → log in `CHANGES.md`; if nothing matches, rewrite per the methodology and borrow only the structure.

| Gate | When | Reference script → OUTDIR copy | Common must-change items |
|---|---|---|---|
| **A (static)** | After Phase 0, before the **first** harvest/spider run | `harvest_static.py` / `spider_mpa.py` | **The default regex runs as-is on most sites**; only when the manifest/dialect does not match do you change the endpoint regex, the webpack/Vite `publicPath`, or MPA exclude/cookie |
| **B (runtime)** | After Phase 2, before running depth/coverage | `runtime_harvest.js` / `preload.js` + `config.json` | Cookie/localStorage keys, neutralize success value, stubs, login regex, api prefix, hash/history |

**Mandatory SPA order** (not interchangeable; Phase numbering takes priority over "explore first, then script"):

| Step | Must | Prohibited |
|---|---|---|
| After Phase 0 is done | Next Bash = `python3 OUTDIR/harvest_static.py <URL> OUTDIR` | curl/grep/Read the main entry `index-*.js` (usually >500KB) |
| Gate A | Copy the script → small tweaks as needed → **run immediately** | Manually extracting APIs first and only then deciding whether to harvest |
| Before Phase 1 is done | `wc -l` to validate the output; on 404 edit harvest and retry | Hand-written extract scripts; repeatedly grepping URLs you have not downloaded |
| From Phase 1b on | grep only `OUTDIR/js/*.js` | Using the main bundle instead of harvest |

- ✅ Copy `harvest_static.py` → (optionally) change the regex → **run immediately**
- ❌ curl the main bundle → grep many times → write a throwaway extract → harvest only at the end
- **MPA**: after Phase 0 the next Bash = `python3 OUTDIR/spider_mpa.py ...`

---

## Tool and output constraints

| Constraint | Detail |
|---|---|
| Large files | An `index-*.js` >100KB **must not** be Read/grepped into context; batch-process it with an OUTDIR script |
| grep output | Always `\| head -20` or `-m 5`; keep only a path summary in the conversation, do not paste bundle fragments |
| Validation | Use `wc -l`, `ls \| wc -l`; do not Read a whole directory |
| regex first look | Optional, ≤1 time, only on a ≤50KB small chunk or HTML; the authoritative static pass is harvest |
| reference | Recipes/templates/troubleshooting in [reference.md](reference.md); do not repeat the full text inline |

---

## Execution roadmap

```
Phase 0 classification + OUTDIR
  → Gate A → Phase 1 harvest (★ run immediately ★)
  → Phase 1b parameter reverse-engineering
  → Phase 2 three auth gates → config.json
  → Gate B → Phase 3 runtime + parameter matrix
  → Phase 4 permission tree (when needed) → rerun Phase 3
  → Phase 5 merge report + insert_assets to bulk-insert every discovered service and endpoint API asset; no matter what, the insert must not drop any discovered asset
```

Check off in order; **you may not enter the next Phase until the previous item is done**.

1. [ ] **Phase 0**: first look at SPA/MPA; create `OUTDIR` → [Phase 0](#phase-0--classification)
2. [ ] **Gate A + Phase 1**: copy the script → **immediately** harvest → `wc -l` validation → [Phase 1](#phase-1--static)
3. [ ] **Phase 1b**: anchor window expansion + binding layer → `param_candidates.json` → [Phase 1b](#phase-1b--parameter-reverse-engineering)
4. [ ] **Phase 2**: three auth gates → `config.json` → [Phase 2](#phase-2--three-auth-gates)
5. [ ] **Gate B**: adjust the runtime script → [Phase 3](#phase-3--runtime)
6. [ ] **Phase 3**: depth / coverage / both; confirm you are into the shell; parameter-trigger matrix → `param_samples.json`
7. [ ] **Phase 4** (if needed): permission tree → patch stubs → rerun Phase 3 → [Phase 4](#phase-4--permission-tree-reconstruction)
8. [ ] **Phase 5**: merge outputs + report + `insert_assets` → [Phase 5](#phase-5--merge-and-report)

---

## Phase 0 — Classification

Fetch the entry HTML and **create `OUTDIR`** (do not edit the skill's own `scripts/`):

- **SPA**: empty shell + `<div id=app>` + chunks → Phase 1–5
- **MPA**: SSR + `<form>`, no endpoint bundle → after Gate A:

```bash
python3 recon/spider_mpa.py <BASE_URL> <OUTDIR> [--cookie "session=..."] [--max 300] [--depth 5] [--exclude "logout|delete|destroy"]
```

Produces `forms.txt`, `links.txt`, `api_inline.txt`. For an SPA, if forms ≈ 0 → switch to Phase 1.

---

## Phase 1 — Static

Follow [Scripts and gates](#scripts-and-gates) · [Tool and output constraints](#tool-and-output-constraints).

```bash
python3 recon/harvest_static.py <BASE_URL> <OUTDIR>
```

harvest: parse the HTML scripts → webpack/Vite manifest → download every lazy chunk → produce `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt`.

```bash
wc -l OUTDIR/api_static.txt OUTDIR/routes.txt
ls OUTDIR/js | wc -l
```

- chunk count vs manifest: on a 404 you must edit harvest and retry, do not curl chunks one by one by hand
- `api_static.txt` too small → loosen the endpoint regex in OUTDIR and rerun (see reference)

### Phase 1b — Parameter reverse-engineering

Paths come from Phase 1; parameter fields need their own recon. See [Tool and output constraints](#tool-and-output-constraints) for grep rules.

**Completion standard**: for every important endpoint you can answer — field name, transport location, inferred type, whether required, sample value, confidence.

#### 1b.0 — Transport form

| Form | Where the parameters are | Look first (static) |
|---|---|---|
| REST JSON | body + query | `(params\|data\|body)\s*:\s*\{` next to the path anchor |
| GraphQL | `variables` | gql templates, `$page: Int` |
| Classic form | urlencoded | `<form>`, `FormData` |
| File upload | multipart | `FormData.append` |
| Path parameter | `/user/:id` | route table + `useParams` / `$route.params` |
| Encrypted/signed | wrapped into `sign`/`data` | Hook the encryption function's arguments (reference section D) |

Output: tag each endpoint with `transport: query|json|form|graphql|encrypted`.

#### 1b.1 — Anchor window expansion

Using a known path as the anchor, expand the window to find the request-assembly object:

```bash
grep -n '"/api/user/list"' OUTDIR/js/*.js | head -20
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' OUTDIR/js/*.js | head -20
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' OUTDIR/js/*.js | head -20
```

| Wrapper layer | Parameter clue |
|---|---|
| axios instance | `data` / `params` |
| Unified request | interceptor injects global fields |
| OpenAPI client | generated method signature |
| React Query / SWR | hook's second argument |
| Vue composable | composable's arguments |

Type residue: `yup`/`zod`/rules, `Form.Item name=`, embedded Swagger.

→ `param_candidates.json`: `{ path, fields[], source: "static-callsite", confidence }`

#### 1b.2 — Binding layer

```
Form field → onFinish/handleSubmit → transform → API payload
```

| Binding source | Technique |
|---|---|
| Form submit | follow submit → transform → API |
| Table search | `getFieldsValue()` → `params` |
| Route | `:id` / `?tab=` |
| Interceptor | global `tenantId`, pagination, sign |
| Enum select | `options` → API enum values |

From the DevTools call stack, trace upward from `fetch`/`XHR.send` to the request-assembly function.

#### 1b.3 — Three request-assembly questions (≠ the Phase 2 three auth gates)

| Question | What to answer |
|---|---|
| **Assembly** | where the payload is built, traces of transform |
| **Validation** | required, pattern, enum |
| **Transport** | path / query / body / multipart / headers |

The interceptor gate (Phase 2) also reads global injected fields along the way (Authorization, `X-Tenant-Id`, sign).

#### 1b.4 — Handoff to Phase 3

Candidate fields come from the static/binding layer; **required/optional/conditional dependencies** need the Phase 3 parameter matrix + diff + the Phase 5 error-based inference.

---

## Phase 2 — Three auth gates

grep in `OUTDIR/js/` (with `head`) and write to `config.json` (recipes in the reference):

| Gate | Question | Keywords |
|---|---|---|
| **Render gate** | How is "logged in" decided? | `isLogin`, `getToken`, Cookie/localStorage |
| **Interceptor gate** | What triggers the jump to `/login`? | `response_code`, `errno`, axios interceptor |
| **Content gate** | Where do the menu/permissions come from? | `menu`, `permission`, `role`, `acl`, `routes` |

Do not treat a localStorage key name as a credential — you must confirm it from the chunk/request chain.

**Exit = Gate B**: land the conclusions in `config.json`, and edit `OUTDIR/runtime_harvest.js` / `preload.js`.

### Phase 2b — API observation (optional)

Use `preload.js` in OUTDIR to confirm session key names, Authorization, and nested API URLs:

| Config | Output |
|---|---|
| `recordDetail: true` | `__API_RECON_DETAIL__` |
| `observe.xhrHeaders: true` | header observation |
| `extractUrlsFromResponse: true` | sub-APIs inside responses |
| `observe.storageReads/cookieReads: true` | backfill config |
| `neutralizeVueRouter: true` | `__API_RECON_ROUTES__` |

coverage exports each round: `__API_RECON_LOG__`, `__API_RECON_DETAIL__`, `__API_RECON_ROUTES__`, `__API_RECON_OBSERVE__`.

---

## Phase 3 — Runtime

Gate B must already be passed; follow [Boundaries and prohibitions](#boundaries-and-prohibitions-agent-must-read--crossing-the-line-is-out-of-scope) · the credential-free mock strategy.

Set `"runtimeMode": "depth" | "coverage" | "both"` in `config.json` (template in the reference).

### Hooks and stubs (shared by depth + coverage)

| Layer | Scope | Purpose |
|---|---|---|
| L1 exact | auth/permission/bootstrap stub | pass the first-screen auth |
| L2 negative correction | all JSON responses | not-logged-in code → success |
| L3 fallback | `/api` etc. not matched by L1 | empty success body, prop up the UI |

- **depth**: fake auth + `forward` to rewrite the business code + `stubs`; traverse `routes` (hash/history); produce `runtime_api.json`
- **coverage**: inject `preload.js` at **document-start** (CDP `addScriptToEvaluateOnNewDocument` or a userscript)

Verify: `window.__API_RECON_PRELOAD__` exists; a business path does not bounce back to `/login`.

```bash
cd recon && npm install
node runtime_harvest.js config.json
```

### 3b — coverage dynamic enumeration (required)

1. Main nav/sidebar — click each item, wait 1–3s for the network
2. Tabs — `role=tab`, `.ant-tabs-tab`
3. Tables — first row view/edit/detail
4. Toolbar — export, filter, create (**avoid irreversible deletes**)
5. Each time you enter a module — merge APIs/routes
6. SPA — controlled `pushState` for paths in `routes.txt` not yet covered (prohibited for MPA)

**Parameter-trigger matrix** (required): record once per operation type in each module, and **diff multiple samples**:

| Operation | Parameters usually added |
|---|---|
| List first screen | pagination + default filters |
| Click search | keyword, filter |
| Advanced filter | more optional |
| Create/edit | full entity |
| Bulk/export/sort | `ids[]`, `exportType`, `sortField` |

**Under a stub the outbound body/headers are still real** — go by the request. Record → `scan_raw.json`, `param_samples.json`, `api_detail.json`.

- **Vue**: `neutralizeVueRouter: true` + document-start preload
- **React**: `routes.txt` + sidebar clicks + `pushState`
- **both**: 3a depth first, then 3b coverage

---

## Phase 4 — Permission tree reconstruction

**Trigger**: blank module pages / only bootstrap per route (e.g. locale) → the content gate has not passed.

| Symptom | Meaning |
|---|---|
| Into the shell successfully | render gate + interceptor gate already passed |
| Missing sidebar items / blank on click | stub shape or permission codes incomplete |
| The same, very few APIs on every route | `v-if permission` not passing |
| `routes.txt` far smaller than the bundle | must be completed from the auth module |

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl)[^"]*"' OUTDIR/js/*.js | sort -u | head -30
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|menuList|authList' OUTDIR/js/*.js | head -20
```

Typical chain: `role_permissions` (flat codes) + `permissions/all` (tree) → `getResultTree` → `userRouteAuth[CODE].url`.

```bash
python3 recon/extract_route_map.py recon/js recon/
python3 recon/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Intermediate outputs: `route_map.json`, `userRouteAuth.json`, `permissions_tree.json`, `*_stub.json`, `perm_codes_all.txt`.

stub check: the outer `response_code` matches the interceptor gate; flat codes align with the tree; `routes` covers every link in `route_map`.

After updating `config.json`, **rerun Phase 3**. For a large SPA you can tune `waitUntil`, `routeTimeout`, `perRouteMs` (see reference sections A3/I).

---

## Phase 5 — Merge and report

### Output table

| File | Stage | Content |
|---|---|---|
| `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt` | 1 | static bundle and paths |
| `param_candidates.json` | 1b | static parameter-field candidates |
| `config.json` | 2 | three gates + runtime config |
| `runtime_api.json` | 3a | depth detailed recording (incl. WS/SSE) |
| `param_samples.json`, `scan_raw.json`, `api_detail.json` | 3b | multiple samples, click log, detail |
| `route_map.json` etc. | 4 | permission-tree intermediate files (if run) |
| `params_merged.json` | 5 | merged parameter fields + confidence |
| `api_merged.txt` | 5 | `METHOD /path [params] [static\|runtime\|both]` |
| `site_map.json` | 5 | routes, APIs, params, feature points, limitations |
| **insert_assets** | 5 | write all service and endpoint assets into the asset store |

### 5b — Parameter merge

Diff from `param_samples.json`; **there is no general-purpose merge script**. Confidence rules in reference J7 (high/medium/low/pending-trigger).

### 5c — Error-based inference

Within the authorized scope you may send incomplete requests and read the 400 (**this is parameter recon, not vulnerability testing**): `field 'x' is required`, enum errors, etc. Watch for the `data` wrapper, `variables`, and the pre-encryption `bizData`.

The report must note: runtimeMode, static/runtime API counts, parameter confidence, uncovered modules, and a `CHANGES.md` summary relative to the reference scripts.

Suggested `site_map.json` structure:

```json
{
  "site": "https://example.com",
  "runtimeMode": "both",
  "appType": "vue-spa",
  "routeGuardStrategy": ["nav-neutralize", "L1-auth", "L2-patch", "forward"],
  "apisFromStatic": [],
  "apisFromRuntime": [],
  "apis": [],
  "params": [{ "method": "POST", "path": "/api/user/list", "transport": "json", "fields": [] }],
  "frontendRoutes": [],
  "routesVerifiedByClick": [],
  "featuresTriggered": [],
  "limitations": ""
}
```

More fields and grep recipes in [reference.md](reference.md).

---

## General notes

- **Framework-agnostic**: webpack/Vite/Angular lazy load use the same method
- **Transport**: REST/JSON, GraphQL, WebSocket, SSE; gRPC-web is out of scope
- **SSR**: client-side fetch is recordable; RSC/Server Actions are not fully enumerable
- **Blind spots**: JSVMP, WASM, strong HMAC/mTLS validation → static + note the limitation
- **Parameter blind spots**: conditional coupling, hidden params, WASM request assembly → "pending trigger" / "unreachable"
- **Static is the safety net**: when runtime is blocked, static can still enumerate endpoints

---

## Additional resources

- Grep recipes, `config.json` template, troubleshooting, hooks, parameter reverse-engineering section J, site_map template: **[reference.md](reference.md)**
- Reference-script paths are in the [Scripts and gates](#scripts-and-gates) table
