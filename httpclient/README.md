# httpclient

Retry-aware HTTP transport for Innovedia CLIs. Wraps `net/http.Client` with
exponential backoff, structured error classification (composing with
[`exitcode`](../exitcode)), header injection, and a request/response hook
surface for auth and observability.

The package is intentionally stateless beyond the underlying `http.Client`:
auth tokens, on-disk caches, and per-call mutation stay in the caller and
plug in through hooks. Two CLIs ship on top today —
[platform-cli](https://github.com/innovediatech/platform-cli) (auth
lifecycle + 401-retry) and
[glitchtip-cli](https://github.com/innovediatech/glitchtip-cli) (bearer
auth + Link-header pagination).

## Quick start

```go
package main

import (
    "context"
    "log"
    "net/http"

    "github.com/innovediatech/agent-cli/httpclient"
)

func main() {
    client, err := httpclient.New(httpclient.Config{
        BaseURL: "https://api.example.com",
        Headers: http.Header{"Accept": []string{"application/json"}},
    })
    if err != nil {
        log.Fatal(err)
    }

    var out struct {
        Items []struct{ ID, Name string } `json:"items"`
    }
    if err := client.DoJSON(context.Background(), "GET", "/items", nil, &out); err != nil {
        log.Fatal(err) // typed APIError; composes with exitcode.Classify
    }
}
```

`DoJSON` treats non-2xx responses as a typed `APIError` and strips BOM /
XSSI guard prefixes before decoding. Use `Do` if you want raw `*http.Response`
semantics.

## Bearer auth via RequestHook

The hook surface is how you layer auth without rebuilding the transport.
Token lookup, refresh, and injection all live in the caller:

```go
client, _ := httpclient.New(httpclient.Config{
    BaseURL: apiURL,
    RequestHook: func(ctx context.Context, req *http.Request) error {
        tok, err := tokenStore.Get(ctx)
        if err != nil {
            return err
        }
        req.Header.Set("Authorization", "Bearer "+tok)
        return nil
    },
})
```

## 401 → re-auth → retry via Classifier

Compose your classifier on top of `DefaultClassifier`. The default handles
5xx, 429 with `Retry-After`, and transport errors; layer auth-style behavior
on top:

```go
classifier := func(resp *http.Response, err error) httpclient.Decision {
    if resp != nil && resp.StatusCode == http.StatusUnauthorized {
        tokenStore.Invalidate()
        return httpclient.Retry
    }
    return httpclient.DefaultClassifier(resp, err)
}

client, _ := httpclient.New(httpclient.Config{
    BaseURL: apiURL,
    Retry:   httpclient.RetryPolicy{MaxAttempts: 2},
    Classifier: classifier,
})
```

This is the exact pattern platform-cli uses for session refresh on 401.

## Retry policy

`RetryPolicy{}` zero value resolves to `DefaultRetryPolicy`: 3 attempts,
500ms base, 8s cap, 2× multiplier, jitter on. `Retry-After` headers
(both delta-seconds and HTTP-date forms) are honored when the classifier
returns `RetryAfterHeader` — `DefaultClassifier` does this for 429 and
503 automatically.

Request bodies are buffered into memory once and replayed across retries.
For payloads larger than memory, build the request yourself and call
`client.HTTPClient()` directly.

## Rate limiting

`Config.Limiter` is an optional pacing hook. Any type satisfying:

```go
type Limiter interface {
    Wait(ctx context.Context) error
    OnSuccess()
    OnRateLimit()
}
```

…plugs in. `Wait` runs before each attempt; `OnSuccess` / `OnRateLimit`
feed the adaptive variants. The Printing Press petstore client's adaptive
limiter satisfies this interface unmodified.

## Errors

`APIError` (returned by `Do` on terminal failure and by `DoJSON` on any
non-2xx) implements `exitcode.CodedError`, so:

```go
if err := client.DoJSON(ctx, "GET", "/items", nil, &out); err != nil {
    os.Exit(exitcode.Classify(err)) // 4 on 401/403, 5 on 5xx, 10 on transport, ...
}
```

`APIError.Body` carries up to `MaxErrorBodyBytes` of the response body for
diagnostics.

## Header inspection

When you need response headers (pagination cursors in `Link`, rate-limit
budget in `X-RateLimit-*`), use `DoJSONHeaders` instead of `DoJSON`:

```go
hdr, err := client.DoJSONHeaders(ctx, "GET", "/issues", nil, &out)
if err != nil {
    return err
}
cursor := parseLinkNext(hdr.Get("Link"))
```

`DoJSON` is a thin wrapper that discards headers; the two share a single
implementation.

## See also

- [`exitcode`](../exitcode) — typed exit codes APIError composes with
- [`envelope`](../envelope) — `Meta.NextCursor` pairs with `DoJSONHeaders`
  for paginated agent surfaces
- [`mirror`](../mirror) — local SQLite cache that this transport feeds via
  resource-agnostic sync helpers
