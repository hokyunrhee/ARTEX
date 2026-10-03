# api-recon - Reference

Grep recipes, `config.json` templates, and troubleshooting. Run all grep commands against `js/`. For single-line bundles, optionally use `js-beautify` or `sed 's/}/}\n/g'` first; raw grep with bounded context is usually sufficient.

## Script notes

Every file under `scripts/` is a **reference template** and must be adapted to the target before execution. Common adaptations:

| Script | Common adaptations |
|---|---|
| `harvest_static.py` | Endpoint regexes, webpack/Vite manifest parsing, microfrontend publicPath, retries/concurrency |
| `runtime_harvest.js` | neutralize field names and success values, stub matching and body shape, route sources, WS recording, `waitUntil`/`routeTimeout`/`proxy` |
| `preload.js` | `loginPathRe`, L1 stubs, `neutralize.fields`, `apiPattern`, L3 enablement, `recordDetail`, `observe.*`, `neutralizeVueRouter` |
| `spider_mpa.py` | `--exclude` for destructive links, cookie, depth/max, same-domain filtering |
| `extract_route_map.py` | `routeMap` / `routeLink` regexes and KEY naming patterns |
| `build_perm_tree.py` | `userRouteAuth` parsing, `ROOTS`/`PREFIX_PARENT` hierarchy heuristics, outer stub field names |
| `config.json` | Central configuration for all target-specific settings above |

Keep adapted files in the task workspace, such as `recon/`, and document the specific changes from the reference scripts in the report.

---

## A. Reverse-engineer the three gates

### A1. Rendering gate: how is login state determined?

```bash
grep -rhoaE '.{0,40}(isLogin|isAuthenticated|loggedIn|hasLogin|requireAuth)\b.{0,80}' js | head
grep -rhoaE 'function (getUser|getToken|getAuth)[0-9]?\([^)]*\)\{.{0,200}' js | head
grep -rhoaE '(localStorage|sessionStorage)\.getItem\("[^"]+"\)' js | sort -u
grep -rhoaE '(Cookies?|cookie)\.(get|load)\("[^"]+"\)' js | sort -u
grep -rhoaE '\batob\(|JSON\.parse\(|jwt|decode' js | head
```

Trace `isLogin = f(getUser())` -> `getUser = decode(storage.read(KEY))` to identify the **storage key**, **container** (Cookie versus localStorage), and **encoding**:

| Encoding | Simulated value in config |
|---|---|
| Plain string / `"1"` / token | `"value": "anything-truthy"` |
| `JSON.parse(x)` | `"value": "json:{\"id\":1,\"username\":\"admin\"}"` |
| `JSON.parse(atob(x))` | `"value": "b64json:{\"id\":1,\"username\":\"admin\"}"` |
| JWT | Unsigned/`alg:none` JWT, or sign with a key embedded in the bundle |
| Encryption (SM2/AES/RSA) | Look for hardcoded keys; forge a blob if the rendering gate only needs decodable content; otherwise fall back to static analysis |

Record the result in `cookies` / `localStorage`.

### A2. Interceptor gate: what redirects to /login?

```bash
grep -rhoaE '.{0,60}(interceptors\.response|axios|request\.use).{0,120}' js | head
grep -rhoaE '.{0,40}(response_code|errcode|errno|\bcode\b|\bret\b|\bstatus\b)\s*[=!]==?\s*[\-0-9]{1,4}.{0,60}' js | head -20
grep -rhoaE '.{0,40}(未登录|请重新登录|登录已过期|unauthorized|登录失效|授权|token.{0,10}invalid).{0,40}' js | head
grep -rhoaE '.{0,30}(location\.href|router\.(push|replace)|navigate)\([^)]*login[^)]*\)' js | head
```

Identify **field names**, **success values** (usually `0` or `200`), and **failure values that trigger redirects**. Verify with a junk session:

```bash
curl -sk -X POST -H 'Cookie: <fakekey>=junk' https://target/api/<protected> -d '{}' -H 'Content-Type: application/json'
```

Record `neutralize.fields` and `neutralize.success`.

### A3. Content gate: where do menus and permissions come from?

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl|resource|nav)[^"]*"' js | sort -u
grep -rhoaE '.{0,30}(menus|permissions|menuList|routeList|authList|role_permissions)\b.{0,120}' js | head
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|hasPermission|checkAuth' js | head
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head
```

**Two data layers**, common in business administration interfaces:

| API | Typical payload | Consumer |
|---|---|---|
| `.../role_permissions` | `{ permissions: string[], role_type }` | Route guards and button-level ACL |
| `.../permissions/all` | `tree[{ code, position, children }]` | Sidebar menu rendering |
| `userRouteAuth` in bundles | `{ CODE: { url, name? } }` | Map codes to frontend paths |
| `routeMap` in bundles | `{ KEY: { name, link } }` | Alias resolution, such as webpack `o.DASHBOARD` |

Inspect consumer code to determine how `getResultTree(tree, permissions)` filters entries and which fields `v-if` / `hasAuth(code)` checks.

**Manual simulation for small sites**: build a permissive payload and add it to `stubs`.

For **complete permission-tree reconstruction** on large sites with blank sidebars/submodules, see **section I**.

---

## B. config.json template

```json
{
  "baseUrl": "https://target/",
  "runtimeMode": "both",
  "chromium": "/usr/bin/chromium",

  "cookies": [
    { "name": "auth", "value": "b64json:{\"id\":1,\"username\":\"admin\",\"role\":\"admin\",\"func\":{},\"permissions\":[\"*\"]}" }
  ],
  "localStorage": { "token": "faketoken", "isLogin": "1" },

  "neutralize": {
    "fields": ["response_code", "code", "errno", "ret", "status"],
    "success": 0,
    "flags": { "success": true, "message": "ok" }
  },
  "forward": true,
  "loginUrlPattern": "/login",
  "apiPattern": "/api/|/rest/|/graphql",

  "mockTier": "L1+L2",
  "recordDetail": true,
  "observe": {
    "storageReads": false,
    "cookieReads": false,
    "xhrHeaders": true
  },
  "neutralizeVueRouter": true,
  "stubs": [
    {
      "match": "permissions/all|/menu|role_permissions",
      "body": {
        "response_code": 0, "code": 0,
        "data": {
          "permissions": ["*"],
          "menus": [
            { "name": "dashboard", "path": "/dashboard", "show": true, "children": [] },
            { "name": "alert", "path": "/alert", "show": true, "children": [] }
          ]
        }
      }
    }
  ],

  "explore": {
    "clickTabs": true,
    "clickTables": true,
    "pushStateFallback": true,
    "maxMenuItems": 50
  },

  "routes": ["/dashboard", "/alert", "/asset", "/device", "/report", "/config", "/system"],
  "waitMs": 1500, "perRouteMs": 900, "headless": true,
  "waitUntil": "domcontentloaded",
  "routeTimeout": 12000,
  "proxy": "",

  "captureResponses": true, "recordWs": true, "respMax": 600
}
```

Field descriptions:
- `runtimeMode`: `depth` (Puppeteer), `coverage` (browser MCP), or `both`.
- `cookies[].value` prefixes: `b64json:` means base64(JSON); `json:` means raw JSON; no prefix means a literal value.
- `forward: true` forwards real requests and rewrites status-code fields; `false` uses fully offline stubs.
- `mockTier`: preload layers enabled for coverage, such as `L1+L2` or `L1+L2+L3`.
- `routes` comes from `routes.txt`; after menu simulation, the harness automatically adds `<a href>` routes.
- `captureResponses` / `recordWs` apply only to depth mode.
- `waitUntil`: use `domcontentloaded` for large SPAs to avoid hanging on `networkidle2`.
- `routeTimeout`: per-route `page.goto` timeout in milliseconds.
- `proxy`: Puppeteer `--proxy-server`; `HTTP_PROXY` / `HTTPS_PROXY` are also supported.

### B1. Two-stub template (role_permissions + permissions/all)

```json
"stubs": [
  {
    "match": "role_permissions",
    "body": {
      "response_code": 0,
      "data": {
        "permissions": ["MONITOR", "MONITOR_ALERT", "THREAT", "ASSETS_RISK"],
        "role_type": "SUPER_ADMIN"
      }
    }
  },
  {
    "match": "permissions/all",
    "body": {
      "response_code": 0,
      "data": [
        {
          "code": "MONITOR",
          "position": 1,
          "children": [
            { "code": "MONITOR_ALERT", "position": 1, "children": [] }
          ]
        }
      ]
    }
  }
]
```

Outer field names (`response_code` / `code` / `data`) must match the A2 interceptor gate; `permissions` must cover every leaf code in the tree.

---

## C. Coverage mode: preload configuration

Edit the `CONFIG` object at the top of your copy of `scripts/preload.js`, or replace it before CDP injection:

```javascript
const CONFIG = {
  loginPathRe: /\/(login|signin)(\/|$|\?)/i,
  mockTier: 'L1+L2',
  forward: true,
  recordDetail: true,
  extractUrlsFromResponse: true,
  neutralizeVueRouter: true,
  observe: { storageReads: false, cookieReads: false, xhrHeaders: true },
  neutralize: { fields: ['response_code', 'code'], success: 0 },
  stubs: [ /* Same as config.json stubs. */ ],
  apiPattern: /\/(api|apis|v\d+|dev|internal|graphql)\//i,
};
```

Verify `window.__API_RECON_PRELOAD__ === true` and a stable pathname.

Export recordings:

```javascript
JSON.stringify({
  apis: [...window.__API_RECON_LOG__],
  detail: window.__API_RECON_DETAIL__,
  routes: [...(window.__API_RECON_ROUTES__ || [])],
  observe: window.__API_RECON_OBSERVE__,
}, null, 2)
```

---

## D. Preload and runtime hook capabilities

Browser hooks built into preload (coverage) and runtime_harvest (depth):

| Hook | Value for API discovery | Coverage |
|---|---|---|
| fetch / XHR.open | Record request URLs and methods | Supported: `recordDetail` + `__API_RECON_LOG__` |
| XHR.setRequestHeader | Discover Authorization and other headers | Supported: `observe.xhrHeaders` |
| localStorage/cookie reads | Identify session keys | Optional: `observe.storageReads/cookieReads` |
| Vue route discovery | Complete frontendRoutes | Supported: `__API_RECON_ROUTES__` for loaded routes |
| Vue route-guard neutralization / login-redirect blocking | Render modules so they trigger APIs | Supported: `neutralizeVueRouter` + native redirect neutralization |
| React route discovery | Add routes | Static analysis and clicks; no dedicated hook |
| Page-navigation blocking for login paths | Stay on the page for analysis | Block login paths only; do not block business navigation |
| Encryption libraries such as CryptoJS/SM | Convert encrypted parameters into plaintext API bodies | Manual encryption-argument hooks required; record conclusions in config |
| Anti-debugging bypass | Allow runtime API recording | Manual handling required; static analysis remains available |

---

## E. Endpoint extraction regexes (when static results are sparse)

Broaden `extract_endpoints` in `harvest_static.py`, or inspect manually:

```bash
grep -rhoaE '"/[a-z][A-Za-z0-9_/\-]{3,}"' js | sort -u
grep -rhoaE '/api/[a-zA-Z0-9_./-]+' js | sort -u
```

---

## F. Troubleshooting

| Symptom | Cause and action |
|---|---|
| Few static APIs | Endpoint dialect mismatch; broaden regexes in section E |
| Far fewer chunks than the manifest | CSS-only or undeployed chunks; 404s have already been retried |
| Runtime still shows the login page | Incorrect rendering gate; review A1 keys, container, encoding, and domain |
| Shell renders but modules are blank | Content gate: simulate menus using A3; `routes` paths may be wrong |
| Only bootstrap/locale calls per route | Incomplete permissions; reconstruct the tree in section I and check both `role_permissions` and `permissions/all` stubs |
| Sidebar items appear but subpages are blank | Missing intermediate tree nodes or codes inconsistent with `userRouteAuth` |
| Every API redirects to login | Check interceptor-gate `neutralize`; extend the traversal for nested fields |
| Zero WS frames | Subscription may require interaction; increase `perRouteMs` |
| Empty response bodies | Real responses are available only with `forward: true` |
| Chromium missing | Install chromium or set `config.chromium` / `CHROMIUM` |
| Many mocks but still redirected to login | Hooks run too late or lack a `location.href` setter; use document-start preload |
| All lists are empty | Empty L3 arrays are expected; continue through tabs, settings, and details |
| Redux actions mistaken for routes | Filter internal paths containing get/set/change/clear/toggle/upload |
| Vue still redirects to login | Inject preload at document-start; if `neutralizeVueRouter: false`, clear guards manually |
| Response URLs missing from logs | Enable `extractUrlsFromResponse` or extract them manually from `__API_RECON_DETAIL__` |
| Authorization header name unknown | Enable `observe.xhrHeaders` or inspect request headers in DevTools |
| Runtime is very slow or times out | Use `waitUntil: domcontentloaded`, reduce `routeTimeout`, and avoid `networkidle2` |
| Proxy connection fails | Check `proxy` and environment variables; use the same proxy port for Puppeteer and curl |

---

## G. Hardened targets

When the server validates the session at each step, such as unforgeable signed cookies or server-rendered menus that cannot be stubbed, runtime may stall at the shell. Expected behavior:

- **Static analysis is sufficient for endpoint enumeration**: module paths are present in the code.
- When authorization permits, run the same harness with a **real session**: use `forward: true` without neutralization to capture real methods, parameters, and responses.

---

## H. Per-task checklist

1. Confirm the authorized scope.
2. **Read** `scripts/harvest_static.py`, adapt it to the target, run it, and review `api_static.txt` and `routes.txt`.
3. **Phase 1b**: expand context around path anchors and trace binding layers into `param_candidates.json`; see section J.
4. Reverse-engineer A1/A2/A3 and write target-specific `config.json`.
5. **Read and adapt** `runtime_harvest.js` / `preload.js` before execution.
6. For `runtimeMode=depth`, run `npm install`, then the adapted harvest script.
7. For `runtimeMode=coverage/both`, inject adapted preload at document-start, then use browser MCP for dynamic enumeration and the **parameter trigger matrix**.
8. If modules do not render, **reconstruct the permission tree in section I**, patch stubs, and rerun.
9. Compare parameter samples and infer from errors to produce `params_merged.json`.
10. Merge into `site_map.json` and `api_merged.txt`; accurately state coverage, gaps, and script adaptations.

---

## I. Permission-tree reconstruction (extended Phase 4)

Use this when a simple simulated `menus: [{ path, show: true }]` does not work and submodules still do not mount.

### I1. Locate auth modules

```bash
grep -l 'userRouteAuth' js/*.js
grep -l 'routeMap\|routeLink' js/*.js
grep -rhoaE 'getResultTree|role_permissions|permissions/all' js | head
```

Record **permission API paths**, **response field names**, and **consumer chunk filenames**.

### I2. Extract routeMap

```bash
python3 scripts/extract_route_map.py recon/js recon/
# Produces recon/route_map.json.
```

If `[!] no routeMap pattern found` appears, broaden regexes in `extract_route_map.py` or grep manually:

```bash
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head -20
```

### I3. Build the permission tree and stubs

```bash
python3 scripts/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Script behavior:
1. Parse `userRouteAuth={MONITOR:{url:...},...}`, including webpack aliases such as `He=o.DASHBOARD`.
2. Resolve aliases to real paths using `route_map.json`.
3. Infer parents from code prefixes, such as `MONITOR_ALERT` -> `MONITOR`.
4. Produce `permissions_tree.json`, `permissions_all_stub.json`, and `role_permissions_stub.json`.
5. With `--config`, update `stubs` in `config.json` and extend `routes` automatically.

**Adapt these settings to the target** at the top of the script:
- `DEFAULT_ROOTS`: top-level module codes.
- `DEFAULT_PREFIX_PARENT`: mappings from `PREFIX_` to parent.
- `DEFAULT_EXTRA_PARENT`: orphan nodes whose relationships do not follow prefixes.

### I4. Validate stub consistency

```bash
# Permission count should approximately match the number of userRouteAuth entries.
wc -l recon/perm_codes_all.txt
# routes should cover every link in route_map.
python3 -c "import json; m=json.load(open('recon/route_map.json')); r=set(json.load(open('recon/config.json'))['routes']); print('missing', [v['link'] for v in m.values() if v['link'] not in r])"
```

### I5. Rerun runtime analysis and compare

```bash
node recon/runtime_harvest.js recon/config.json
# Compare runtime_api.json counts before and after simulation; check for module APIs under /attack, /asset, and similar paths.
```

| Before simulation | After successful simulation |
|---|---|
| The same 3-5 bootstrap calls on every route | Different routes trigger different module APIs |
| Only `/api/locale/language` | Module endpoints under `/api/web/...` appear |
| Single-digit route count in `routes.txt` | `routes` contains 80-110 or more entries from route_map |

### I6. If it still fails

- **Coverage mode**: click sidebar items and tabs; permission-gated requests may require interaction.
- **Stub fields**: compare nesting between the real API (curl with a real session) and the stub.
- **Additional guards**: grep button-level checks such as `hasPermission|checkRole|func.` and extend `role_permissions.permissions`.
- **Static fallback**: module API paths remain in `api_static.txt`; runtime only adds METHOD/body. Retain `param_candidates.json` and collected parameter samples.

---

## J. Parameter reverse engineering (Phases 1b / 5b / 5c)

**A methodology, not a universal script.** Find paths with regexes; find parameters through expanded anchor context, UI binding chains, multi-sample diffs, and error inference.

### J1. Expand anchor context: locate request builders from paths

```bash
# Use paths already discovered in Phase 1 as anchors.
grep -n '"/api/user/list"' js/*.js
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' js | head
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' js | head
grep -rhoaE '(get|post|put|delete|patch)\([^,]+,\s*\{' js | head
```

### J2. Wrappers and transport shapes

```bash
# axios / shared request wrapper
grep -rhoaE '(axios|request)\.(get|post|put|delete|patch)\(' js | head
grep -rhoaE 'interceptors\.(request|response)' js | head

# GraphQL
grep -rhoaE '(query|mutation)\s+\w+|gql`|graphql\(' js | head
grep -rhoaE '\$[a-zA-Z_]+\s*:\s*(Int|String|Boolean|\[)' js | head

# FormData / multipart
grep -rhoaE 'FormData|\.append\(' js | head

# Path parameters
grep -rhoaE 'path:\s*"/[^"]*:[^"]+"' js | head
grep -rhoaE 'useParams|route\.params|\$route\.params' js | head
```

### J3. Validation gate: required fields, formats, and enums

```bash
grep -rhoaE '(required|message|pattern|enum|validator)\s*:' js | head
grep -rhoaE 'yup\.|zod\.|async-validator|Form\.Item|a-form-item|el-form-item' js | head
grep -rhoaE 'rules\s*:\s*\[|name:\s*["\'][a-zA-Z_]+["\']' js | head
grep -rhoaE 'label.*value|options\s*:\s*\[' js | head
```

### J4. Binding layers: forms to APIs

```bash
grep -rhoaE 'onFinish|handleSubmit|getFieldsValue|validateFields' js | head
grep -rhoaE '(pick|omit|transform|dayjs|moment)\(' js | head
```

Supplement with runtime analysis: in DevTools -> Network -> request -> **Initiator**, follow the call stack upward from `fetch`/`send` to the request builder.

### J5. Encrypted parameters

```bash
grep -rhoaE 'encrypt|decrypt|sign|CryptoJS|sm2|sm3|sm4|RSA|AES' js | head
```

**Do not guess fields from ciphertext.** Hook encryption-function **arguments** and record plaintext payloads before encryption; record conclusions in `config.json` / `param_candidates.json`.

### J6. Parameter trigger matrix (required in Phase 3)

Record each operation once per module and diff request bodies/queries:

| Operation | Inspect |
|---|---|
| Initial list | Pagination defaults |
| Search | keyword, filters |
| Advanced filters | Optional fields |
| Create/edit | Complete entity |
| Bulk/export | `ids[]`, `exportType` |
| Sort/paginate | `sortField`, `order` |

Produce `param_samples.json`: `[{ "path", "method", "action": "search", "body", "query", "headers" }]`.

### J7. Confidence rules

| Confidence | Condition |
|---|---|
| **High** | Static callsite agrees with at least two runtime samples |
| **Medium** | Static evidence only, or one runtime sample |
| **Low** | Inferred from responses/errors without a second verification |
| **Awaiting trigger** | Static field known, but its UI/permission path has not been reached |

### J8. Scenario shortcuts

| Scenario | Sequence |
|---|---|
| REST list page | J1 request builder -> four J6 diffs -> J3 rules |
| Create/edit form | J3 Form name -> J4 submit chain -> runtime submission, then intentionally omit fields to inspect 400 responses |
| GraphQL | J2 variables declaration -> record variables for each runtime operation |
| Encrypted body | J5 argument hook -> pre-encryption fields are the actual parameters |

### J9. Mapping to api-recon phases

| api-recon | Parameter reconnaissance |
|---|---|
| Phase 1 static | J1 expanded anchor context |
| Phase 2 A2 interceptor | Globally injected fields such as tenantId and sign |
| Phase 3 runtime | J6 trigger matrix and `param_samples.json` |
| Phase 4 permission tree | Modules have different forms; sufficient permissions are needed to trigger every field |
| Phase 5 merge | `params_merged.json` and confidence; never infer required fields from a single sample |

### J10. Troubleshooting

| Symptom | Action |
|---|---|
| Static field never appears at runtime | Mark awaiting trigger; complete permissions, open advanced filters, and try each dependent select option |
| Same path, different body shapes | Expected: record separate entries by `action`; do not force schemas together |
| Stub responses are synthetic but parameters are needed | **Inspect outbound request** bodies/headers; never infer them from stub responses |
| 400 identifies a nested field | Account for outer `data`/`bizData`/`variables` wrappers |
| GraphQL shows only an operation name | Expand `variables` JSON and find `$var: Type` statically |

---
