---
name: api-recon
description: Use this skill to collect a website's API endpoints.
---

# API recon (frontend API reconnaissance)

With **authorization**, discover as much as possible of the **backend API** (paths, methods, parameters, response bodies), **frontend routes**, and **UI feature triggers** (tabs, dialogs, table actions, and similar controls).

---

## Boundaries and prohibitions (required reading)

This skill performs **API and parameter-surface reconnaissance only**, not vulnerability discovery or exploitation.

### Task boundaries

| Area | Allowed | Prohibited |
|---|---|---|
| **Objective** | Enumerate paths, methods, parameters, routes, and UI triggers | SQLi/XSS/access-control/brute-force/fuzz vulnerability testing, request-tampering attacks, destructive operations |
| **Authentication** | Use hooks and stubs/mocks to bypass **client-side** login gates | Ask for or guess credentials; submit a real login form |
| **Runtime** | Hook APIs without credentials and use mock responses to render the authenticated SPA shell | Workflows that require a real backend session to proceed |

### Dynamic analysis without credentials (Phase 3 default)

1. **Intercept and stub** login, permission, menu, and other bootstrap APIs with `preload.js` / `runtime_harvest.js`.
2. Return mock bodies for business queries with **the correct structure, a successful business status, and optionally empty data**.
3. Let the frontend render authenticated pages without a backend or despite 401 responses, triggering more XHR/fetch/WebSocket requests.
4. **Empty data, blank tables, and placeholder UI are expected**. Do not switch to real authentication or vulnerability testing because of them.

Use mocks to expose frontend routes and mount components, and **record outbound requests only**. The important question is which additional APIs the frontend calls, not what the backend returns.

### Strict workflow prohibitions

| Prohibited | Required alternative |
|---|---|
| Before Phase 1 finishes, use grep/curl/Read on the main `index-*.js` entry to extract API paths | Run `OUTDIR/harvest_static.py` |
| Write `extract_apis.py` or another replacement for harvest | Modify and rerun `OUTDIR/harvest_static.py` |
| Repeat the same grep/command after at least two failures | Change strategy: read tool_logs, modify harvest, or consult reference |
| Skip gates A/B and run original `scripts/` templates directly | Copy them to OUTDIR and adapt them to the target |
| Use real usernames/passwords, OTP, OAuth, or other authentication | Use stubs/mocks as described above |
| Skip stubs and test access-control bypass or injection to obtain real data | Record outbound requests only, within the reconnaissance boundary |
| Delete, export sensitive data, perform bulk writes, or take other irreversible actions | This prohibition also applies to coverage clicks |
| Claim all pages and APIs were found without runtime analysis and dynamic enumeration | Meet the definition of done or state the limitations |
| Claim all parameters are understood without the trigger matrix and diff | Complete the Phase 3b matrix and Phase 5 diff |
| Infer required/optional fields from a single runtime sample | Compare multiple samples or infer from validation rules/errors |

---

## Two-layer model and runtime modes

| Layer | Output | Limitations |
|---|---|---|
| **Static** (JS bundles) | All endpoint paths, draft routes, candidate request fields | No HTTP methods; parameters need Phase 1b; runtime-composed URLs can be missed |
| **Runtime** (live session) | Methods, bodies, responses, dynamic URLs, WS/SSE; multi-sample diffs refine parameters | Pages must actually render to issue requests; one sample cannot establish required/optional fields |

| Mode | Engine | Best suited to |
|---|---|---|
| **depth** | `runtime_harvest.js` (Puppeteer) | API inventory, METHOD/params/responses, WS/SSE, reproducible batch runs |
| **coverage** | browser + `preload.js` | Deeper feature coverage through tabs, dialogs, and table actions |
| **both** | depth, then coverage | The most complete coverage, with the highest time cost |

**Parameter methodology** (no universal script): use harvest/regex for paths; use **expanded anchor context, UI binding chains, multi-sample diffs, and inference from errors** for parameters. Grep recipes are in section J of [reference.md](reference.md).

---

## Definition of done

Claim reconnaissance is complete only when all of these hold:

- [ ] **Static**: Phase 1 harvest produced `api_static.txt`, `routes.txt`, and `js/`.
- [ ] **Runtime**: at least depth or coverage ran; coverage/both requires **working hooks and the dynamic enumeration loop**.
- [ ] **Authenticated app shell**: business paths stay outside `/login`, including hash routes.
- [ ] **Parameters**: coverage/both completed the trigger matrix and `param_samples.json`; Phase 5 produced `params_merged.json`.
- [ ] **Depth**: if module pages are blank, reconstruct the permission tree in Phase 4 and rerun until **module-level APIs** appear, beyond locale/bootstrap calls.
- [ ] **Delivery**: every Phase 5 deliverable exists; `insert_assets` stored the service and endpoint assets.

---

## Scripts and gates

`scripts/` contains reference templates only. **Never** run them unchanged and treat their output as final.

**Rule**: read first, adapt to the target, write copies into `OUTDIR` (such as `recon/`), and record changes in `CHANGES.md`. If a template does not fit, rewrite it using the methodology and borrow only its structure.

| Gate | When | Reference script copied to OUTDIR | Typical adaptations |
|---|---|---|---|
| **A (static)** | After Phase 0, before the **first** harvest/spider run | `harvest_static.py` / `spider_mpa.py` | **Default regexes work for most sites**; change endpoint regexes, webpack/Vite `publicPath`, or MPA exclude/cookie only when the manifest or dialect does not match |
| **B (runtime)** | After Phase 2, before depth/coverage | `runtime_harvest.js` / `preload.js` + `config.json` | Cookie/localStorage keys, neutralize success values, stubs, login regex, API prefixes, hash/history routing |

**Mandatory SPA order**: do not reorder these steps. Phase numbers take precedence over general advice to explore before scripting.

| Step | Required | Prohibited |
|---|---|---|
| After Phase 0 | The next Bash command is `python3 OUTDIR/harvest_static.py <URL> OUTDIR` | curl/grep/Read of the main `index-*.js` entry, often larger than 500KB |
| Gate A | Copy the script, make small adaptations if needed, and **run immediately** | Manually extract APIs before deciding whether to harvest |
| Before Phase 1 finishes | Validate output with `wc -l`; fix harvest and retry on 404 | Write extraction scripts or repeatedly grep URLs that were never downloaded |
| Phase 1b onward | Grep only `OUTDIR/js/*.js` | Substitute the main bundle for harvest |

- Correct: copy `harvest_static.py`, optionally adjust regexes, and **run immediately**.
- Incorrect: curl the main bundle, grep repeatedly, write a temporary extractor, and only then harvest.
- **MPA**: the next Bash command after Phase 0 is `python3 OUTDIR/spider_mpa.py ...`.

---

## Tool and output constraints

| Constraint | Requirement |
|---|---|
| Large files | **Never** Read/grep an `index-*.js` larger than 100KB into context; batch-process it with OUTDIR scripts |
| Grep output | Always use `\| head -20` or `-m 5`; retain only path summaries in the conversation, not bundle excerpts |
| Validation | Use `wc -l` and `ls \| wc -l`; do not Read entire directories |
| Initial regex probe | Optional, at most once, and only on HTML or a small chunk no larger than 50KB; harvest remains the authoritative static pass |
| Reference | Recipes, templates, and troubleshooting are in [reference.md](reference.md); do not duplicate the full reference inline |

---

## Execution roadmap

```
Phase 0: classify the application and create OUTDIR
  -> Gate A -> Phase 1: harvest (run immediately)
  -> Phase 1b: reverse-engineer parameters
  -> Phase 2: identify the three authentication gates -> config.json
  -> Gate B -> Phase 3: runtime analysis and parameter matrix
  -> Phase 4: reconstruct permissions when needed -> rerun Phase 3
  -> Phase 5: merge the report and use insert_assets to batch-insert every discovered service and API endpoint; omit none
```

Check these off in order. **Do not enter the next phase until the previous item is complete.**

1. [ ] **Phase 0**: distinguish SPA/MPA and create `OUTDIR` -> [Phase 0](#phase-0---classification).
2. [ ] **Gate A + Phase 1**: copy scripts, harvest **immediately**, validate with `wc -l` -> [Phase 1](#phase-1---static-analysis).
3. [ ] **Phase 1b**: expanded anchor context and binding layers -> `param_candidates.json` -> [Phase 1b](#phase-1b---parameter-reverse-engineering).
4. [ ] **Phase 2**: three authentication gates -> `config.json` -> [Phase 2](#phase-2---three-authentication-gates).
5. [ ] **Gate B**: adapt runtime scripts -> [Phase 3](#phase-3---runtime-analysis).
6. [ ] **Phase 3**: depth / coverage / both; verify that the authenticated app shell renders; trigger matrix -> `param_samples.json`.
7. [ ] **Phase 4**, if needed: permissions, patched stubs, and a Phase 3 rerun -> [Phase 4](#phase-4---permission-tree-reconstruction).
8. [ ] **Phase 5**: merge outputs, report, and `insert_assets` -> [Phase 5](#phase-5---merge-and-report).

---

## Phase 0 - Classification

Fetch the entry HTML and **create `OUTDIR`**. Do not edit the skill's `scripts/` directory.

- **SPA**: an empty shell, `<div id=app>`, and chunks -> Phases 1-5.
- **MPA**: SSR and `<form>`, without an endpoint bundle -> after gate A:

```bash
python3 recon/spider_mpa.py <BASE_URL> <OUTDIR> [--cookie "session=..."] [--max 300] [--depth 5] [--exclude "logout|delete|destroy"]
```

Outputs: `forms.txt`, `links.txt`, `api_inline.txt`. For an SPA with approximately zero forms, switch to Phase 1.

---

## Phase 1 - Static analysis

Follow [Scripts and gates](#scripts-and-gates) and [Tool and output constraints](#tool-and-output-constraints).

```bash
python3 recon/harvest_static.py <BASE_URL> <OUTDIR>
```

Harvest parses HTML scripts and webpack/Vite manifests, downloads all lazy chunks, and produces `js/`, `api_static.txt`, `routes.txt`, and `chunkmap.txt`.

```bash
wc -l OUTDIR/api_static.txt OUTDIR/routes.txt
ls OUTDIR/js | wc -l
```

- Compare chunk counts with the manifest. On 404, fix and retry harvest; do not curl chunks manually one by one.
- If `api_static.txt` is too small, broaden the endpoint regex in OUTDIR and rerun; see the reference.

### Phase 1b - Parameter reverse engineering

Paths come from Phase 1; parameter fields require a separate pass. Follow [Tool and output constraints](#tool-and-output-constraints) for grep.

**Completion criterion**: for important APIs, identify field names, transport locations, inferred types, required/optional status, example values, and confidence.

#### 1b.0 - Transport shapes

| Shape | Parameter location | First static source to inspect |
|---|---|---|
| REST JSON | body + query | `(params\|data\|body)\s*:\s*\{` near the path anchor |
| GraphQL | `variables` | gql templates, `$page: Int` |
| Traditional forms | urlencoded | `<form>`, `FormData` |
| File upload | multipart | `FormData.append` |
| Path parameters | `/user/:id` | Route tables and `useParams` / `$route.params` |
| Encryption/signatures | Wrapped in `sign`/`data` | Hook encryption-function arguments; reference section D |

Annotate each API with `transport: query|json|form|graphql|encrypted`.

#### 1b.1 - Expand around anchors

Use known paths as anchors and widen the context to locate request-building objects:

```bash
grep -n '"/api/user/list"' OUTDIR/js/*.js | head -20
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' OUTDIR/js/*.js | head -20
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' OUTDIR/js/*.js | head -20
```

| Wrapper layer | Parameter clues |
|---|---|
| axios instance | `data` / `params` |
| Shared request wrapper | Global fields injected by interceptors |
| OpenAPI client | Generated method signatures |
| React Query / SWR | Second hook argument |
| Vue composable | Composable arguments |

Look for remaining type information in `yup`/`zod`/rules, `Form.Item name=`, and embedded Swagger.

Write `param_candidates.json`: `{ path, fields[], source: "static-callsite", confidence }`.

#### 1b.2 - Binding layers

```
Form field -> onFinish/handleSubmit -> transform -> API payload
```

| Binding source | Technique |
|---|---|
| Form submit | Follow submit -> transform -> API |
| Table search | `getFieldsValue()` -> `params` |
| Route | `:id` / `?tab=` |
| Interceptor | Global `tenantId`, pagination, sign |
| Enum select | `options` -> API enum values |

In the DevTools call stack, trace upward from `fetch`/`XHR.send` to the request builder.

#### 1b.3 - Three request-building questions

These are distinct from the three authentication gates in Phase 2.

| Question | Required answer |
|---|---|
| **Assembly** | Where the payload is built and transformed |
| **Validation** | required, pattern, enum |
| **Transport** | path / query / body / multipart / headers |

When inspecting the interceptor gate in Phase 2, also inspect injected global fields such as Authorization, `X-Tenant-Id`, and sign.

#### 1b.4 - Connect to Phase 3

Static and binding analysis supplies candidate fields. **Required/optional status and conditional dependencies** require the Phase 3 parameter matrix, diffs, and Phase 5 error inference.

---

## Phase 2 - Three authentication gates

Grep `OUTDIR/js/` with bounded output and write the results to `config.json`; recipes are in the reference.

| Gate | Question | Keywords |
|---|---|---|
| **Rendering** | How is the logged-in state determined? | `isLogin`, `getToken`, Cookie/localStorage |
| **Interceptor** | What triggers a redirect to `/login`? | `response_code`, `errno`, axios interceptor |
| **Content** | Where do menus and permissions come from? | `menu`, `permission`, `role`, `acl`, `routes` |

Never treat a localStorage key name as a credential; confirm its meaning from chunks and request chains.

**Exit condition = gate B**: record the conclusions in `config.json` and adapt `OUTDIR/runtime_harvest.js` / `preload.js`.

### Phase 2b - API observation (optional)

Use the OUTDIR copy of `preload.js` to identify session keys, Authorization, and nested API URLs:

| Setting | Output |
|---|---|
| `recordDetail: true` | `__API_RECON_DETAIL__` |
| `observe.xhrHeaders: true` | Header observations |
| `extractUrlsFromResponse: true` | Nested APIs in responses |
| `observe.storageReads/cookieReads: true` | Findings to incorporate into config |
| `neutralizeVueRouter: true` | `__API_RECON_ROUTES__` |

Export `__API_RECON_LOG__`, `__API_RECON_DETAIL__`, `__API_RECON_ROUTES__`, and `__API_RECON_OBSERVE__` after each coverage iteration.

---

## Phase 3 - Runtime analysis

Gate B must be complete. Follow [Boundaries and prohibitions](#boundaries-and-prohibitions-required-reading) and the credential-free mock strategy.

Set `"runtimeMode": "depth" | "coverage" | "both"` in `config.json`; see the reference template.

### Hooks and stubs (shared by depth and coverage)

| Layer | Scope | Purpose |
|---|---|---|
| L1 exact | auth/permission/bootstrap stubs | Pass initial authentication gates |
| L2 negative-response correction | All JSON responses | Replace unauthenticated business codes with success |
| L3 fallback | `/api` and similar calls not matched by L1 | Return empty success bodies so the UI can render |

- **depth**: fake auth, `forward` business-code correction, and `stubs`; visit `routes` with hash/history routing; produce `runtime_api.json`.
- **coverage**: inject `preload.js` at **document-start** through CDP `addScriptToEvaluateOnNewDocument` or a userscript.

Verify that `window.__API_RECON_PRELOAD__` exists and business paths do not redirect to `/login`.

```bash
cd recon && npm install
node runtime_harvest.js config.json
```

### 3b - Dynamic coverage enumeration (required)

1. Click every main-navigation/sidebar item; allow 1-3 seconds for network activity.
2. Visit tabs, including `role=tab` and `.ant-tabs-tab`.
3. Use the first table row's view/edit/detail actions.
4. Trigger toolbar export, filter, and create controls, **avoiding irreversible deletion**.
5. Merge APIs and routes after entering each module.
6. For SPAs, use controlled `pushState` for uncovered paths in `routes.txt`; never do this for MPAs.

**Required parameter trigger matrix**: record each operation type once per module and **diff multiple samples**:

| Operation | Parameters commonly added |
|---|---|
| Initial list | Pagination and default filters |
| Search | keyword, filter |
| Advanced filters | Additional optional fields |
| Create/edit | Complete entity |
| Bulk/export/sort | `ids[]`, `exportType`, `sortField` |

**Outbound bodies and headers remain real under stubbing**; use requests as the evidence. Record `scan_raw.json`, `param_samples.json`, and `api_detail.json`.

- **Vue**: `neutralizeVueRouter: true` and document-start preload.
- **React**: `routes.txt`, sidebar clicks, and `pushState`.
- **both**: run 3a depth before 3b coverage.

---

## Phase 4 - Permission-tree reconstruction

**Trigger**: blank module pages or only bootstrap calls, such as locale requests, on every route mean the content gate has not been passed.

| Observation | Meaning |
|---|---|
| Shell renders | Rendering and interceptor gates have been passed |
| Missing sidebar items or blank pages after clicks | Incomplete stub shape or permission codes |
| Few identical APIs on every route | `v-if permission` conditions remain unsatisfied |
| Far fewer routes in `routes.txt` than the bundle | Recover additional routes from auth modules |

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl)[^"]*"' OUTDIR/js/*.js | sort -u | head -30
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|menuList|authList' OUTDIR/js/*.js | head -20
```

Typical chain: `role_permissions` (flat codes) + `permissions/all` (tree) -> `getResultTree` -> `userRouteAuth[CODE].url`.

```bash
python3 recon/extract_route_map.py recon/js recon/
python3 recon/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Intermediate outputs: `route_map.json`, `userRouteAuth.json`, `permissions_tree.json`, `*_stub.json`, `perm_codes_all.txt`.

Check stubs: the outer `response_code` agrees with the interceptor gate; flat codes and tree align; `routes` covers every link in `route_map`.

Update `config.json` and **rerun Phase 3**. For large SPAs, adjust `waitUntil`, `routeTimeout`, and `perRouteMs`; see reference sections A3/I.

---

## Phase 5 - Merge and report

### Deliverables

| File | Phase | Contents |
|---|---|---|
| `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt` | 1 | Static bundles and paths |
| `param_candidates.json` | 1b | Candidate static parameter fields |
| `config.json` | 2 | Three gates and runtime settings |
| `runtime_api.json` | 3a | Detailed depth recordings, including WS/SSE |
| `param_samples.json`, `scan_raw.json`, `api_detail.json` | 3b | Multiple samples, click logs, details |
| `route_map.json` and related files | 4 | Permission-tree intermediates, when needed |
| `params_merged.json` | 5 | Merged parameter fields and confidence |
| `api_merged.txt` | 5 | `METHOD /path [params] [static\|runtime\|both]` |
| `site_map.json` | 5 | Routes, APIs, params, features, limitations |
| **insert_assets** | 5 | Store every service and endpoint asset in the asset inventory |

### 5b - Parameter merge

Diff `param_samples.json`; **there is no universal merge script**. Confidence rules are in reference J7: high, medium, low, and awaiting trigger.

### 5c - Inference from errors

Within the authorized scope, incomplete requests may be sent to inspect 400 responses such as `field 'x' is required` or enum errors. **This is parameter reconnaissance, not vulnerability testing.** Account for `data` wrappers, `variables`, and pre-encryption `bizData`.

The report must state runtimeMode, static/runtime API counts, parameter confidence, uncovered modules, and a `CHANGES.md` summary of adaptations from the reference scripts.

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

More fields and grep recipes are in [reference.md](reference.md).

---

## General notes

- **Framework-independent**: webpack/Vite/Angular lazy loading uses the same approach.
- **Transports**: REST/JSON, GraphQL, WebSocket, and SSE; gRPC-web is out of scope.
- **SSR**: client fetch calls can be recorded; RSC/Server Actions cannot be enumerated completely.
- **Blind spots**: JSVMP, WASM, and strict HMAC/mTLS checks require static analysis plus explicit limitations.
- **Parameter blind spots**: conditional dependencies, hidden parameters, and WASM request builders should be marked awaiting trigger or unreachable.
- **Static analysis is the fallback**: it can still enumerate endpoints when runtime access is blocked.

---

## Additional resources

- Grep recipes, `config.json` templates, troubleshooting, hooks, parameter reverse engineering in section J, and a site_map template: **[reference.md](reference.md)**.
- Reference script paths are listed under [Scripts and gates](#scripts-and-gates).
