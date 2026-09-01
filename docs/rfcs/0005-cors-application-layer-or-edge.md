---
status: exploring
date: "2026-08-28"
discussion: "https://github.com/neicnordic/sensitive-data-archive/pull/2555"
authors:
  - "@jhagberg"
consulted: []
informed: []
---

# Where to handle CORS for browser clients: application layer or edge

## Context and Problem Statement

An archive operator wants to run web UIs against the SDA API services from
other origins, and wants an answer that covers all services. Sprint planning
leaned towards the ingress but did not settle it. This RFC lays out the
options and what the current code does to each of them.

Only `sda-auth` handles CORS today. It registers
`iris-contrib/middleware/cors` when `cors.origins` is set
([main.go#L409-L417][auth-cors]), [auth.md][auth-md-cors] documents a
credentialed browser login via `/oidc/cors_login`, and the chart has
`global.auth.corsOrigins`, `corsMethods` and `corsCreds`
([values.yaml#L192-L197][values-auth-cors]). Those values never reach the
service, though: what the template renders
([auth-secrets.yaml#L18-L23][auth-secrets]) does not match the keys the
service reads ([config.go#L427-L440][config-cors]). The service also accepts
`*` and `null` origins. No other service sends `Access-Control-*` headers.

The one browser client we have, [sda-download-ui][ui-repo], does not need
CORS. Both of its download modes, the tar stream ([tar-download.md][ui-tar])
and the [File System Access batch download][ui-fsa], go through its own
origin, and that proxy ([`/api/files/:fileId`][ui-proxy]) does things CORS
cannot: it keeps the bearer and the recipient key on the server, gives the
browser only a session cookie, and stitches the per-request Crypt4GH header
onto the stable `/content` stream so the browser sees one resumable file.
The CORS prerequisite in its older [decision document][ui-design] is
outdated, according to its authors. This RFC leaves that proxy alone. What a
direct browser path to v2 would need is open question 8.

So the driver is general. Other browser applications should be able to call
the API directly, at any site, without each of them building a proxy. That
raises two questions: which layer implements CORS, and which services need
it.

## Decision Drivers

* **Federated correctness** — origins are deployment knowledge; the header
  contract (allowed request headers, exposed response headers, preflight) is
  API knowledge. Both must be right at every node and never set in two
  layers, since browsers reject a duplicated `Access-Control-Allow-Origin`.
* **Security** — no reflection, no wildcards, no `null`, and never `*`
  together with credentials.
* **Preflight must not reach authentication** — an `OPTIONS` preflight
  carries no `Authorization`.
* **Portability** — across ingress classes (the operator chooses
  [`ingressClassName`][values-ingressclass]) and to non-Kubernetes
  deployments.
* **Testability in CI** — including the chart plumbing; the `sda-auth`
  values shipped broken because nothing tests them.
* **Backwards compatibility** — sda-cli, htsget-rs and curl are unaffected,
  and nothing changes unless a site opts in.
* **Small footprint** — reviewable on one screen.

## Considered Options

1. **Application-layer middleware** with a configured origin allowlist,
   surfaced through Helm values
2. **Ingress / reverse-proxy layer**, per site via annotations or middleware
   resources, possibly with chart-shipped per-class defaults
3. **Do nothing**: browser applications proxy through their own origin
   (backend-for-frontend)
4. **Same-origin routing at the edge**: publish the API under the UI's origin
5. **Chart-bundled gateway or sidecar** (Envoy, nginx) owning CORS for every
   service

## Open Questions

1. **Which layer?** Does Option 1's edge-error gap (ingress `502`/`503`/`504`
   reach browsers without CORS headers) outweigh keeping the header contract
   with the code? Does "one mechanism for every language" matter if v1 and
   `doa` are out of scope? Who owns the header contract: code, chart, or
   site?
2. **Download v1 and `doa`.** v1 is still the chart default
   ([values.yaml#L278-L300][values-v1-default]) and neither is deprecated,
   yet the only browser client targets v2. Is "a browser UI means enable v2"
   acceptable, and what is the `doa` timeline?
3. **`sda-api`.** Is a submission or curation UI planned? Its clients today
   are `sda-admin` and the [validator orchestrator][validator].
4. **`sda-auth` login path.** Does a browser application call
   `/oidc/cors_login` from the browser, or does its own server complete the
   OIDC exchange and hand the browser a session? Only the first needs CORS
   in `sda-auth`, and it is the endpoint the contract would be tested
   against.
5. **Browser authentication against download v2.** Bearer on every request,
   or the session cookie with `credentials: 'include'`? This decides whether
   `Allow-Credentials` is needed (see the header contract).
6. **`sda-auth` migration.** Direct deployments may have
   `cors.origins: "*"`, patterns or `cors.methods`. Reject at startup, which
   turns an upgrade into a login outage, or warn for one release? Should
   `cors.methods` stay configurable?
7. **Configuration encoding.** `sda-auth` reads a comma-separated string via
   `viper.GetString`, which returns empty for a YAML list. Is that the
   canonical shape everywhere?
8. **Direct browser downloads.** CORS alone does not give a browser a plain
   file download from v2. The client must fetch header and content
   separately, stitch them itself and resume on `/content`'s `ETag`, which
   is why sda-download-ui proxies. The resume half is already written: the
   File System Access download in [PR #173][ui-pr-173] sends `Range` and
   `If-Range`, reads `ETag` and `Content-Range` and handles `206`/`416`, but
   against the proxy's stitched stream. Pointing it at v2 would need the
   stitch on the client or a stable header per file and recipient. Does a
   direct path need API changes first, and is it worth designing for?

## Pros and Cons of the Options

### Option 1: Application-layer middleware

Each service that needs CORS registers a middleware ahead of authentication
and reads the allowlist from configuration. The chart exposes it as values,
like the download session cookie
([`global.downloadV2.session.*`][values-session]). An empty allowlist means no
headers and no change.

* Good, because the header contract changes in the same PR as the API. The
  PR that added `SDA-Content-ETag` would also have updated `Expose-Headers`.
* Good, because preflight is answered before authentication, so it neither
  fails nor writes audit events.
* Good, because it is independent of ingress class and of Kubernetes.
* Good, because the Go integration suite and Helm unit tests cover it.
  Nothing in CI exercises ingress annotations.
* Good, because `sda-auth` already does CORS, CSP, `Referrer-Policy` and
  `X-Frame-Options` in the application ([addCSPheaders][csp]); this
  generalises that.
* Bad, because responses generated at the edge (`502`/`503`/`504`, size
  rejections, TLS failures) never pass the middleware and reach the browser
  as opaque failures. Avoiding that means also configuring the edge, which
  is the two-layer setup the drivers rule out.
* Bad, because enabling CORS on a further service is a release, not an
  operations change.
* Bad, because changing the allowlist needs a restart. No service watches
  its config, and `SIGHUP` only triggers shutdown ([main.go#L46][sighup]).
* Bad, because stricter rules for `sda-auth` are a compatibility change for
  direct deployments (open question 6).
* Neutral, because there are three HTTP stacks (iris, [gin][gin-dep],
  `net/http`). A shared `func(http.Handler) http.Handler` with thin adapters
  covers them, in about 40 lines or from a library.
* Neutral, because bearer versus cookie is a UI-side choice; the contract
  supports both.

### Option 2: Ingress / reverse-proxy layer

Each site configures CORS on its ingress (`nginx.ingress.kubernetes.io/cors-*`
annotations, a Traefik `Middleware`) through the `*.ingressAnnotations`
escape hatches the chart already has. A stronger variant ships per-class
profiles in the chart and sites supply only origins.

* Good, because CORS is conventionally an edge concern; an operator can turn
  it on or off without a code release.
* Good, because the edge can stamp CORS headers on the errors it generates,
  so a browser sees a real `503`, though that depends on the controller.
  Option 1 cannot do this at all.
* Good, because it is one mechanism regardless of language, if v1 or `doa`
  need CORS (open question 2). If they do not, the scope is two Go services
  and this advantage is small.
* Good, because preflight never reaches the application.
* Bad, because `sda-auth` already has application CORS. Ingress CORS is
  immediately two layers for it unless its middleware is removed, and then
  `cors_login`'s credential handling moves to every site.
* Bad, because the header lists are API knowledge copied into every site's
  ingress. When the API adds a header, N sites go stale silently. The
  chart-shipped variant moves the copy into the chart, versioned apart from
  the API, with one template per controller.
* Bad, because there is no portable default. Each operator picks
  `ingressClassName` and annotation syntax differs per controller, so
  uncovered sites write their own, and the shortcut under pressure is
  reflecting `Origin`.
* Bad, because CI cannot test it without running each controller.
* Bad, because it splits `sda-auth`'s browser policy across layers, with CSP
  in code and CORS at the edge.
* Neutral, because it is possible to get right. The concern is that it will
  not stay right across sites, and that the drift is invisible until a
  browser breaks.
* Neutral, because non-Kubernetes deployments solve it again.

### Option 3: Do nothing, browser applications proxy

Each browser application relays API calls through its own origin, as
sda-download-ui does. A more deliberate form is a dedicated streaming
backend-for-frontend.

* Good, because it works today with no SDA change.
* Good, because the bearer never reaches the browser, which is the strongest
  defence against token theft via XSS, and the proxy can do what the API
  does not: stitch header and content into one resumable stream.
* Bad, because every byte crosses the network twice and the proxy pod is the
  throughput bottleneck. Presigned storage URLs do not help, because archive
  objects are header-stripped ciphertext and every access is audited.
* Bad, because every future browser application must build its own proxy,
  which does not answer the operator's ask.
* Bad, because it leaves `sda-auth` CORS documented, exposed in values and
  broken through the chart.

### Option 4: Same-origin routing at the edge

Publish the API under the UI's origin (`https://portal.example.org/api/…`)
and route it to the download service, so no request is cross-origin.

* Good, because it removes CORS and the proxy hop. Streaming, ranges and
  backpressure stay at the edge.
* Good, because plain path routing is native to every ingress, so any site
  can do it today.
* Bad, because the API gets a second public URL per UI. Service info, DRS
  URLs, docs, sda-cli and htsget-rs use the canonical host, and the two
  surfaces must stay consistent.
* Bad, because prefix rewriting again differs per controller, and the
  session cookie and absolute URLs are scoped to the alias.
* Bad, because each UI origin needs its own route, and non-Kubernetes
  deployments need a reverse proxy.
* Neutral, because it is compatible with Option 1. A site that prefers it
  leaves the allowlist empty.

### Option 5: Chart-bundled gateway or sidecar

* Good, because it works regardless of service language, the policy is
  versioned with the chart, and preflight never reaches the application.
* Bad, because it adds a component, a hop, resource cost and a failure mode
  per pod or site for what is about 40 lines per service.
* Bad, because it still misses ingress-generated errors, and non-Kubernetes
  deployments get nothing.
* Bad, because the header contract lives in chart templates apart from the
  handlers and is tested by Helm unit tests rather than against them.
* Bad, because the team runs no mesh today, and this would be the first
  piece of one.

## More Information

### Suggested direction

I lean towards Option 1, with Option 4 open to any site that prefers it, and
with v1 and `doa` excluded so that "we want a browser UI" means "enable v2".
What tips it for me is that `sda-auth` already works this way, that the
header contract is the part that drifts when it lives apart from the code,
and that the chart cannot ship a working ingress default. The edge-error gap
and the release dependency are the counter-arguments I take most seriously.
Nothing below is a decision. The header contract holds whichever layer is
chosen; the acceptance criteria are written for Option 1 and say which of
them carry over to the edge.

### Which services need CORS

A service gets CORS only when a browser is meant to make scripted
cross-origin requests (`fetch`/XHR) to it. Service-to-service APIs do not
need it, and neither do navigation flows (redirects, form posts, links).

| Service                | Browser fetch?                         | CORS                         |
|------------------------|----------------------------------------|------------------------------|
| `download` (v2)        | requested; the UI proxies by design    | **now**                      |
| `auth`                 | yes (`/oidc/cors_login`)               | **has; bring in line**       |
| `api`                  | plausibly later                        | **design for, do not build** |
| `s3inbox`              | only if a UI uploads directly          | **defer**                    |
| `syncapi`              | no                                     | no                           |
| `download` (v1), `doa` | no current client                      | **no** (open question 2)     |

* `download` (v2): no cross-origin browser client today, since the UI
  proxies. It is the service the operator's ask is about and the one the
  header contract below is written for, so it goes first.
* `auth`: `/oidc/cors_login` ([main.go#L455][cors-login]) is called from a
  browser with `credentials: 'include'` and [auth.md][auth-md-cors]
  documents it, so `sda-auth` keeps CORS. What changes is the chart wiring,
  a validated allowlist, and method and header lists moved into code.
* `api`: RBAC admin API ([routes.go][api-routes]); no browser calls it.
* `s3inbox`: S3 semantics (`PUT`, multipart, `ETag`, per-part preflight)
  need their own contract once a browser uploader exists, and none does today.
* `syncapi`: node-to-node with basic auth
  ([syncapi.go#L78-L80][syncapi-auth]).

### Header contract for download v2

The lists come from what the v2 handlers and middleware read and set. They
are complete for the current code and hold regardless of layer.

| Header                             | Value                                                                                                |
|------------------------------------|------------------------------------------------------------------------------------------------------|
| `Access-Control-Allow-Methods`     | `GET, HEAD, OPTIONS` (v2 is read-only)                                                               |
| `Access-Control-Allow-Headers`     | `Authorization, X-Amz-Security-Token, X-C4GH-Public-Key, Htsget-Context-Public-Key, Range, If-Range` |
| `Access-Control-Expose-Headers`    | `Accept-Ranges, Content-Disposition, Content-Range, ETag, SDA-Content-ETag, X-Correlation-ID`        |
| `Access-Control-Allow-Credentials` | `true` only when configured per site, and only with an exact-origin `Allow-Origin`                   |
| `Access-Control-Max-Age`           | configurable, bounded                                                                                |
| `Vary`                             | `Origin`                                                                                             |

* `/files/:fileId` and `/files/:fileId/header` require `X-C4GH-Public-Key`
  or `Htsget-Context-Public-Key` as a header, with no other way to pass it
  ([resolveFileForDownload][resolve-download],
  [publickey.go#L5-L22][publickey]). `/content`, the dataset endpoints and
  DRS take no key ([resolveFileForContent][resolve-content]). A navigation
  cannot set request headers, so decrypting in a browser means `fetch()`.
  That is the reason v2 needs CORS ([download.md][download-md-session]).
* `Cache-Control`, `Content-Length` and `Content-Type` are
  [CORS-safelisted][safelisted]. The six exposed headers are invisible to
  page code unless listed, and without them a client cannot resume, name the
  file, check the `ETag` or quote a correlation ID. The resume logic in
  [PR #173][ui-pr-173] is a live example: it reads `ETag` and
  `Content-Range` and sends `Range` and `If-Range`, which works today only
  because the proxy is same-origin.
* The contract leaves tracing headers out for now, since v2 reads none and
  always mints a correlation ID ([problem.go#L45-L54][correlation]). The PR
  that brings [ADR-0006](../decisions/0006-metrics-and-tracing.md) tracing
  to v2 should extend `Allow-Headers`.
* A browser can send the bearer on every request
  ([auth.go#L606-L614][gettoken]) from any allowlisted origin. It can also
  rely on the session cookie v2 sets after the first bearer call
  ([auth.go#L577-L590][session-cookie]). That cookie is always
  `SameSite=Lax` ([config.go#L393-L430][download-config-session]): it is sent
  on same-site `fetch()` with `credentials: 'include'` and on top-level
  navigations, never from another site such as a local UI talking to a
  deployed API, and it needs an exact-origin `Allow-Origin` plus
  `Allow-Credentials: true`. [download.md][download-md-session]
  recommends keeping the bearer either way. I suggest credentials off by
  default with per-site opt-in (open question 5).
* CORS only controls whether page code can read a response; the browser
  sends a simple `GET` regardless. Unauthenticated floods already write an
  audit `denied` per `401` ([`auditDenied`][audit-denied]) and no option
  changes that. With credentials on, a sibling host of the same site can
  trigger cookie-authenticated `GET`s that stream data, write audit events
  ([files.go#L243-L275][audit-get]) and churn caches
  ([cache.go#L182-L205][cache-get]) without being able to read the response,
  which `Lax` already allows through navigation. A site that enables
  credentials trusts its sibling hosts.
* `SDA-Client-Version` is a v1 header and not part of the v2 contract.

### Configuration surface under Option 1

`sda-auth`'s `cors.origins` and `cors.credentials` as read today, plus a new
`cors.max-age`, the same keys in every service:

```yaml
cors:
  origins: ""          # comma-separated origins; empty (default): no CORS
  credentials: false   # send Access-Control-Allow-Credentials: true
  max-age: 600         # Access-Control-Max-Age in seconds, 0-86400
```

The service trims each entry, requires `http(s)://host[:port]` with nothing
after the port, and lowercases before comparing. Methods and header lists
are API knowledge and stay in code, which deprecates `sda-auth`'s
`cors.methods` and `cors.headers` (open question 6).

### Acceptance criteria

These are written against Option 1, the only option with service code to
test. Criteria 1 to 3, 7 and the header lists in 8 describe the observable
contract and apply to Option 2 as well, with the edge answering in place of
the service; 4 to 6 and 9 to 10 are Option 1 and the `sda-auth` cleanup.
Under Option 4 there is no CORS to test.

1. With no origins configured the service sends no `Access-Control-*` or
   `Vary: Origin` header and every response is otherwise unchanged.
2. With origins configured every application response carries
   `Vary: Origin`, also for requests without `Origin` or from a disallowed
   one, and an existing `Vary` is extended. This matters for the public
   `/service-info` ([serviceinfo.go#L29-L33][serviceinfo]).
3. A request without `Origin`, or from a disallowed origin, gets no
   `Access-Control-*` headers and is otherwise processed as before. CORS
   never yields a `401` or `403`. sda-cli, htsget-rs and curl send no
   `Origin`.
4. Origins match exactly. There are no wildcards, patterns, subdomain rules
   or reflection, and `*`, `null` and unparsable entries fail at startup
   with a message naming the key and the entry (for `sda-auth`, after the
   window open question 6 settles on).
5. `Allow-Credentials: true` is sent only when configured and only with an
   exact-origin `Allow-Origin`. In v2 it also requires `session.secure` and
   `session.http-only` to be on. `sda-auth` has no such keys; its Iris
   session cookie is configured in code ([main.go#L407][auth-session]).
6. A preflight from an allowed origin gets `204` before authentication and
   writes no audit event; any other preflight gets no `Access-Control-*`
   headers. In v2 that is a middleware on the gin engine ahead of the
   authenticated groups ([handlers.go#L96-L118][group-mw]) with no `OPTIONS`
   route inside them. Today there are no `OPTIONS` routes, so a preflight
   falls through to gin's `404 page not found` ([gin.go#L759][gin-404]) and
   group middleware never runs.
7. Every application error to an allowed origin, including
   [RFC 9457][rfc9457] `problem+json` ([Error Format][error-format]),
   carries the CORS headers. Edge-generated responses are out of reach for
   Option 1 and covered by Options 2 and 4.
8. v2 `Allow-Headers` and `Expose-Headers` are exactly the lists above, and
   `max-age` outside `0-86400` is rejected.
9. For `sda-auth`, the chart renders `cors.origins` and `cors.credentials`
   at the top level, a Helm unit test asserts the keys, and an integration
   test covers a credentialed preflight and call to `/oidc/cors_login`.
10. `downloadV2.ingressAnnotations` ([values.yaml#L733-L735][values-ingress])
    and `auth.ingressAnnotations` stay and are documented as not for CORS.
    `download.md` and `auth.md` document the keys, the restart, and that a
    local UI such as `http://localhost:3000` talking to a deployed API is
    cross-site: it must be listed explicitly and cannot use the session
    cookie.

### Review triggers

Revisit this RFC when a browser client calls download v2 directly; when v1
or `doa` is retired or gets a browser client; when a browser uploader for
`s3inbox` is planned; when v2 gains a mutating endpoint or tracing; when the
chart adopts a gateway or mesh; or when the htsget-rs topology changes.

### Related

* [auth.md][auth-md-cors]: the existing `sda-auth` CORS implementation and
  its browser login example
* [The session cookie][download-md-session] in download.md, merged from
  [PR #2541][pr-2541] on 2026-08-31: cookie semantics and their CORS
  interaction
* [ADR-0006](../decisions/0006-metrics-and-tracing.md): tracing; when it
  reaches v2 it changes `Allow-Headers`
* [sda-download-ui bulk download architecture][ui-tar]: the proxy model
* [sda-download-ui File System Access download][ui-fsa] and its resume
  support ([PR #173][ui-pr-173]): the browser-side half of a direct path
* htsget-rs is a third-party project. CORS for a browser talking to it is
  umccr's configuration, not ours.

[ui-repo]: https://github.com/NBISweden/sda-download-ui
[ui-proxy]: https://github.com/NBISweden/sda-download-ui/blob/4103342f/frontend/src/app/api/files/%5BfileId%5D/route.ts
[ui-tar]: https://github.com/NBISweden/sda-download-ui/blob/4103342f/docs/tar-download.md#11-what-we-have-today
[ui-design]: https://github.com/NBISweden/sda-download-ui/blob/4103342f/docs/dataset_download.md#47-backend-prerequisites
[ui-fsa]: https://github.com/NBISweden/sda-download-ui/blob/4103342f/frontend/src/app/components/FileSystemAccessBatchDownloadActions.tsx
[ui-pr-173]: https://github.com/NBISweden/sda-download-ui/pull/173
[pr-2541]: https://github.com/neicnordic/sensitive-data-archive/pull/2541
[download-md-session]: https://github.com/neicnordic/sensitive-data-archive/blob/102c2c7f/sda/cmd/download/download.md#the-session-cookie
[validator]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda-validator/orchestrator/README.md
[auth-cors]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/auth/main.go#L409-L417
[auth-md-cors]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/auth/auth.md#running-with-cross-origin-resource-sharing-cors
[auth-secrets]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/charts/sda-svc/templates/auth-secrets.yaml#L18-L23
[config-cors]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/internal/config/config.go#L427-L440
[cors-login]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/auth/main.go#L455
[csp]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/auth/main.go#L359-L370
[auth-session]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/auth/main.go#L407
[api-routes]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/api/routes.go
[syncapi-auth]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/syncapi/syncapi.go#L78-L80
[publickey]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/handlers/publickey.go#L5-L22
[resolve-download]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/handlers/files.go#L126-L135
[resolve-content]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/handlers/files.go#L317-L329
[correlation]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/handlers/problem.go#L45-L54
[gettoken]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/middleware/auth.go#L606-L614
[session-cookie]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/middleware/auth.go#L577-L590
[download-config-session]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/config/config.go#L393-L430
[audit-get]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/handlers/files.go#L243-L275
[cache-get]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/database/cache.go#L182-L205
[serviceinfo]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/handlers/serviceinfo.go#L29-L33
[group-mw]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/handlers/handlers.go#L96-L118
[audit-denied]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/middleware/auth.go#L424-L431
[gin-404]: https://github.com/gin-gonic/gin/blob/v1.12.0/gin.go#L759
[sighup]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/main.go#L46
[gin-dep]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/go.mod#L17
[error-format]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/sda/cmd/download/download.md#error-format
[values-ingressclass]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/charts/sda-svc/values.yaml#L41
[values-auth-cors]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/charts/sda-svc/values.yaml#L192-L197
[values-v1-default]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/charts/sda-svc/values.yaml#L278-L300
[values-session]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/charts/sda-svc/values.yaml#L315-L325
[values-ingress]: https://github.com/neicnordic/sensitive-data-archive/blob/60389bd1/charts/sda-svc/values.yaml#L733-L735
[safelisted]: https://fetch.spec.whatwg.org/#cors-safelisted-response-header-name
[rfc9457]: https://www.rfc-editor.org/rfc/rfc9457
