---
name: go-current
description: Modern Go 1.27 idioms, written on the first pass so modernize has less to rewrite
paths: "**/*.go"
---

# go-current

You are writing Go against the 1.27 toolchain. Use the idioms below on
the first pass; `go fix` modernize rewrites anything else, which costs
a turn and sometimes conflicts with another of its passes.

Keep the module's `go` line at the toolchain you target. `go test` now
runs vet's `stdversion` check, which flags standard library symbols
newer than that line.

## Loops

- `for i := range N` for pure counting, not `for i := 0; i < N; i++`.
  Only when the counter is purely ordinal; a non-zero start, step ≠ 1,
  or mutating `i` inside the loop keeps the classic form.
- `for _, v := range slice`, do not index if you don't need the index.
- `for _, v := range slices.Backward(xs)`, not a hand-written
  descending index loop.
- `for k, v := range map`, map iteration is unordered; never rely on order.

## slices package

- `slices.Contains(xs, x)` / `slices.ContainsFunc`, not a manual loop.
- `slices.Index`, `slices.IndexFunc`.
- `slices.Sort`, `slices.SortFunc`, `slices.SortStableFunc`, not
  `sort.Slice` for ordered element types.
- `slices.Equal`, `slices.Clone`, `slices.Concat`, `slices.Clip`.
- `slices.Delete`, `slices.Insert`, `slices.Replace`.
- `slices.Min`, `slices.Max`, `slices.MinFunc`, `slices.MaxFunc`.
- `slices.Collect(seq)`, `slices.Sorted(seq)` to materialize an iterator.

## maps package

- `maps.Clone(m)`, not a manual copy loop.
- `maps.Keys(m)` / `maps.Values(m)` return `iter.Seq`, not slices.
  Range them directly; `slices.Collect(maps.Keys(m))` only when a
  slice is genuinely needed, `slices.Sorted(maps.Keys(m))` for order.
- `maps.Equal(a, b)`, `maps.Copy(dst, src)`, `maps.Collect(seq2)`.

## Strings and bytes

- `strings.SplitSeq(s, sep)` / `strings.FieldsSeq(s)` when ranging,
  not `strings.Split` then range. Same for `bytes`.
- `strings.Cut(s, sep)` returns `(before, after, found)`, not
  `strings.Index` + slicing. `strings.CutPrefix` / `CutSuffix`, not
  `HasPrefix` + `TrimPrefix`. `strings.CutLast` (1.27), not
  `LastIndex` + slicing.
- `strings.Builder` for building in a loop, not `s += piece`.

## Builtins

- `min(a, b, c)` / `max(a, b, c)`, not if/else chains or
  `math.Min`/`math.Max` (float64-only).
- `clear(m)`, clears a map or zeros a slice in place.
- `new(expr)` (1.26), `new(int64(42))` allocates and initializes in one
  step. Not `x := int64(42); p := &x`.

## Generics (1.27)

- Methods may declare their own type parameters:
  `func (r *Rand) N[I intType](n I) I`. Interface methods cannot, and a
  generic method cannot satisfy an interface method.
- Composite literal keys may be field selectors, so a promoted field is
  set directly: `Outer{Inner.X: 1}`, not `Outer{Inner: Inner{X: 1}}`.
- `interface{}` returns `any`.

## Defaults and comparisons

- `cmp.Or(a, b, c)`, first non-zero value, for default chains.
- `cmp.Compare(a, b)`, three-way comparison for `slices.SortFunc`.

## Errors

- `errors.AsType[*PathError](err)` (1.26) returns `(target, ok)`, not
  `errors.As(err, &target)`.
- `errors.Is` / `errors.AsType`, never `==` except for your own sentinels.
- `errors.Join(errs...)` for multi-error; do not invent your own.
- `fmt.Errorf("context: %w", err)` is the wrap form. Do not `%w` a
  `*E` when `E` itself implements `error`; vet now flags it because
  `errors.Is`/`AsType` will not match.

## Concurrency

- `wg.Go(func() { ... })` (1.25), not `wg.Add(1); go func() {
  defer wg.Done(); ... }()`.
- `sync.OnceFunc`, `sync.OnceValue`, `sync.OnceValues`, not
  `sync.Once` + package var + guard.
- `atomic.Int64`, `atomic.Bool`, `atomic.Pointer[T]`, not
  `atomic.LoadInt64(&x)` on raw ints.
- `context.WithCancelCause`, `context.AfterFunc` when they fit.

## Reflection

- `reflect.TypeFor[T]()`, not `reflect.TypeOf((*T)(nil)).Elem()`.
- `reflect.TypeAssert[T](v)` (1.25), not `v.Interface().(T)`.

## JSON

- `omitzero`, not `omitempty` for struct and `time.Time` fields;
  `omitempty` never omits them.
- `encoding/json/v2` and `encoding/json/jsontext` are available
  (1.27), and `encoding/json` now runs on the v2 engine. New code may
  use v2 (`json.MarshalWrite`, `json.UnmarshalRead`); the v1 API is
  still fine.

## Random numbers

- `math/rand/v2`: `rand.IntN(n)`, `rand.N(d)`, not `math/rand`.
  No seeding.

## Logging

- `log/slog` for anything structured. Key-value pairs
  (`slog.Info("msg", "key", value)`), do not concat into the
  message. Never `fmt.Println` for real logging.

## Context

- Any function that does I/O or calls a context-taking API takes
  `ctx context.Context` first. Do not stash contexts on structs
  outside request-scoped types.

## Removed / replaced

- `io/ioutil` is gone, `os.ReadFile`, `os.WriteFile`, `io.ReadAll`,
  `os.CreateTemp`, `os.MkdirTemp`.
- `rand.Seed` is dead; so is hand-rolled seeding.
- `//go:fix inline` on a thin wrapper tells modernize to inline its
  callers, use it instead of a manual rename campaign.

## Iterators

- Stream with `iter.Seq[T]` / `iter.Seq2[K, V]`; consume with `for v
  := range seq`. Return an iterator when the caller may stop early;
  `slices.Collect` / `maps.Collect` when a value is needed.
- Prefer a type's `All()` / `Keys()` style iterator over `Len()`/`At(i)`
  loops when the API offers one.

## Embedding

- `//go:embed file.txt` for static assets, not string literals.
  Targets must be in or below the source file's directory.

## Testing

- `t.Cleanup(fn)`, not deferred cleanup that an early return skips.
- `t.TempDir()`, `t.Setenv(k, v)`, `t.Chdir(dir)`, not the `os`
  calls; all revert automatically.
- `t.Context()`, not `context.Background()`; canceled at test end.
- `t.ArtifactDir()` (1.26) for files worth keeping under `go test
  -artifacts`.
- `testing/synctest.Test` for code that sleeps or waits on timers; it
  runs under a fake clock with `synctest.Wait` / `synctest.Sleep`.
- Table tests as `t.Run(name, func(t *testing.T) { ... })` subtests.

## Linters in play

gochecks runs golangci-lint with a profile tuned for bugs and idiom.
Write to it directly:

- Close response bodies; check `rows.Err()`; close `sql.Rows`.
- Wrap with `%w`, compare with `errors.Is`, never `==` on errors.
- `switch` over an enum covers every value or has a `default`.
- Return `(T, error)` with exactly one of them set; never `nil, nil`.
- `http.MethodGet`, `http.StatusOK`: the stdlib constants, not strings.
- `for i := range n`, not a three-clause loop.
- `strconv.Itoa(n)` and `a + b`, not `fmt.Sprintf` for them.
- Receivers named consistently across a type; error vars `ErrX`/`errX`;
  error strings lowercase, no trailing punctuation; `any`, not
  `interface{}`.
- No unused parameters: name them `_` if an interface forces them.
- Shell commands and file paths built from input are flagged by gosec;
  pin the command and validate the path or annotate with a reason.

## What modernize cannot rewrite for you

The analyzer gives up when two modernizations overlap (e.g. rewriting
a loop to `slices.Contains` conflicts with rewriting `strings.Split`
inside the same loop to `SplitSeq`). When that happens, `gochecks`
emits a `suggestion`-class finding pointing at the package. Resolve
by hand, pick one transformation, apply, re-run.

## Why this matters here

`gochecks` runs after every edit. Modern idioms on the first pass mean:

1. `gofmt` + `goimports` touch nothing, no silent rewrite noise.
2. `go fix` modernize (on `gochecks fix`, or on every edit with
   `auto_fix: true`) applies zero changes instead of iterating to a
   fixed point.
3. `go vet` and `staticcheck` have fewer style-adjacent hints to
   emit, so their output stays on actual bugs.

Cleaner first draft = tighter hook output = more context for the
actual problem you're solving.
