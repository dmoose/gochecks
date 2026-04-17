---
name: go-current
description: Modern Go 1.26 idioms — write these on the first pass so modernize has less to rewrite
paths: "**/*.go"
---

# go-current

You are writing Go against the **1.26 toolchain**. Use modern idioms on
the **first pass**. Every pattern below exists because `go fix
modernize` will otherwise rewrite your code — wasting a turn and
sometimes creating conflicts with other modernize passes.

## Loops

- `for i := range N` for pure counting — **not** `for i := 0; i < N; i++`.
  Only applies when the counter is purely ordinal; if you need a
  non-zero start, step ≠ 1, or to mutate `i` inside the loop, the
  classic form is still correct.
- `for _, v := range slice` — do not index if you don't need the index.
- `for k, v := range map` — map iteration is unordered; never rely on order.

## slices package

- `slices.Contains(xs, x)` — **not** a manual loop with `==`.
- `slices.Index(xs, x)` / `slices.IndexFunc`.
- `slices.Sort`, `slices.SortFunc`, `slices.SortStableFunc`.
- `slices.Equal`, `slices.Clone`, `slices.Concat`.
- `slices.Delete`, `slices.Insert`, `slices.Replace`.
- `slices.Min`, `slices.Max`, `slices.MinFunc`, `slices.MaxFunc`.

## maps package

- `maps.Clone(m)` — **not** a manual copy loop.
- `maps.Keys(m)` / `maps.Values(m)` since Go 1.23 return **`iter.Seq`**,
  not slices (this differs from the old `golang.org/x/exp/maps`
  versions). Range them directly: `for k := range maps.Keys(m)`. Use
  `slices.Collect(maps.Keys(m))` only when you genuinely need a slice.
- `maps.Equal(a, b)`, `maps.Copy(dst, src)`.

## Strings & bytes — splitting, cutting, appending

- `strings.SplitSeq(s, sep)` — range-style, **not** `strings.Split`
  then range. Same for `bytes.SplitSeq`.
- `strings.FieldsSeq(s)` / `bytes.FieldsSeq` for whitespace splitting.
- **`strings.Cut(s, sep)`** — returns `(before, after, found)`.
  Use it instead of `strings.Index` + slicing for the common
  "split on first occurrence" pattern. Same for `strings.CutPrefix`
  and `strings.CutSuffix`.
- **`fmt.Appendf(nil, ...)`** — use this instead of
  `[]byte(fmt.Sprintf(...))`. Same result, one less allocation.
  Same applies to `fmt.Appendln`, `fmt.Append`.

## Builtins

- `min(a, b, c)` / `max(a, b, c)` — variadic, for ordered types. Prefer
  these over `if/else` chains or `math.Min`/`math.Max` (the latter are
  float64-only and slower for ordered types).
- `clear(m)` — clears a map or zeros a slice in place.

## Defaults and comparisons

- `cmp.Or(a, b, c)` — returns the first non-zero value. Use this for
  default fallback chains instead of nested `if v == ""` ladders.
- `cmp.Compare(a, b)` — three-way comparison returning `-1 / 0 / 1`.
  Ideal for `slices.SortFunc` comparators.

## Errors

- `errors.Join(errs...)` — multi-error wrap; do not invent your own.
- `errors.Is(err, target)` / `errors.As(err, &v)` — never compare with `==`
  except for sentinel values you defined.
- `fmt.Errorf("context: %w", err)` — the standard wrap form.

## One-time initialization

- `sync.OnceFunc(func() { ... })` — returns a func that runs its body
  exactly once.
- `sync.OnceValue(func() T { ... })` / `sync.OnceValues(func() (T, U))` —
  same, but the result is memoized and returned on every call. Use
  these instead of the `sync.Once` + package-level var + init-guard
  pattern.

## Atomic primitives

- `atomic.Int64`, `atomic.Int32`, `atomic.Uint64`, `atomic.Bool`,
  `atomic.Pointer[T]` — generic, type-safe atomic types. Use these
  instead of the `atomic.LoadInt64(&x)` / `atomic.StoreInt64(&x, v)`
  free-function style on raw ints. Same behavior, far less noise.

## Logging

- `log/slog` for anything structured.
- `slog.Default().Info(...)` is fine; construct a custom handler only
  when you need it.
- Pass key-value pairs (`slog.Info("msg", "key", value)` or use
  `slog.Attr` helpers) — **do not** hand-concat into the message string.
- Never `fmt.Println` for real logging.

## Context

- Any function that does I/O or calls a context-taking API takes
  `ctx context.Context` as the first parameter.
- Do not stash contexts on structs (outside request-scoped types).

## Removed / replaced

- `io/ioutil` is gone — use `io` and `os` directly
  (`os.ReadFile`, `os.WriteFile`, `io.ReadAll`, `os.CreateTemp`).
- `rand.Seed` is deprecated — the global source auto-seeds since 1.20.
- `interface{}` → `any` (alias since 1.18; use `any`).

## Iterators (1.23+)

- Functions that stream values should return `iter.Seq[T]` or
  `iter.Seq2[K, V]`.
- Consume with `for v := range seq { ... }`.
- Return an iterator instead of a slice when the caller might stop
  early.
- `slices.Collect(seq)` / `maps.Collect(seq2)` materialize when
  needed.

## Embedding

- `//go:embed file.txt` — use this for static assets instead of
  vendoring content as Go string literals.
- Embed targets must be in or below the source file's directory.

## Testing

- `t.Cleanup(fn)` — **not** deferred cleanup that might be skipped on
  early return.
- `t.TempDir()` — **not** `os.MkdirTemp` inside tests (auto-cleaned).
- `t.Setenv(k, v)` — **not** `os.Setenv` (auto-reverted).
- `t.Context()` — **not** `context.Background()` in tests that wire
  context through; it's canceled automatically on test end.
- Subtests with `t.Run(name, func(t *testing.T) { ... })` for table
  tests.

## Go 1.26 specific

- **`new(expr)`** — Go 1.26 lets `new` take an initializer expression.
  `new(int64(42))` allocates and returns `*int64` pointing to `42`.
  Prefer this over the two-step `x := int64(42); p := &x` or
  `p := new(int64); *p = 42` patterns.
- **`//go:fix inline`** — directive on a function declaration that
  tells modernize "callers of this function should inline the call."
  Useful for deprecating a thin wrapper without the churn of a manual
  rename campaign. See `go help fix`.

## What modernize cannot rewrite for you

The analyzer gives up when two modernizations overlap (e.g. rewriting
a loop to `slices.Contains` conflicts with rewriting `strings.Split`
inside the same loop to `SplitSeq`). When that happens, `gocheck fix`
emits a `suggestion`-class finding pointing at the package. Resolve
by hand — pick one transformation, apply, re-run.

## Why this matters here

`gocheck` hooks fire after every edit. Modern idioms on the first
pass means:

1. `gofmt` + `goimports` touch nothing — less silent rewrite noise.
2. `go fix modernize` (on `gocheck fix`, or silently on every hook
   fire if `auto_fix: true`) applies zero changes instead of
   iterating to a fixed point.
3. `go vet` and `staticcheck` have fewer style-adjacent hints to
   emit, so their output stays focused on actual bugs.

Cleaner first draft = tighter hook output = more context for the
actual problem you're solving.
