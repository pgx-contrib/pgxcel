# pgxcel

[![Go Reference](https://pkg.go.dev/badge/github.com/pgx-contrib/pgxcel.svg)](https://pkg.go.dev/github.com/pgx-contrib/pgxcel)
[![License](https://img.shields.io/github/license/pgx-contrib/pgxcel)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)

`pgxcel` converts a checked [CEL](https://github.com/google/cel-go) AST into
a Postgres `WHERE` fragment with positional bind placeholders. It is
deliberately small: one walker over the standard CEL expression
protobuf, a fail-closed identifier allow-list, and `time.Time` /
`time.Duration` bindings for the `timestamp(...)` / `duration(...)`
literals.

The package accepts any `*cel.Ast` regardless of how it was produced.
That includes ASTs translated back from an [AIP-160](https://google.aip.dev/160)
filter via `cel.CheckedExprToAst`, so the same transpiler powers both
direct CEL and AIP filtering on top of Postgres.

## Installation

```bash
go get github.com/pgx-contrib/pgxcel
```

## Usage

```go
env, _ := cel.NewEnv(
    cel.Variable("name", cel.StringType),
    cel.Variable("age", cel.IntType),
)
ast, iss := env.Compile(`name == "Alice" && age > 30`)
if iss.Err() != nil {
    return iss.Err()
}

columns := map[string]string{
    "name": "users.name",
    "age":  "users.age",
}
where, args, err := pgxcel.Where(ast, pgxcel.WithColumns(columns))
// where: ("users"."name" = $1 AND "users"."age" > $2)
// args:  []any{"Alice", int64(30)}
```

### Options

- `pgxcel.WithColumns(map[string]string)` — the path → DB-column
  allow-list. Lookup is **fail-closed**: any identifier the AST
  references that is not in the map causes `Where` to return an
  error. When omitted, every ident in the AST errors. **Never feed
  user input as a column name**; the value of each map entry is
  emitted into the SQL after only identifier quoting.
- `pgxcel.WithFunctions(map[string]string)` — alias → canonical
  function-name map applied before dispatch. Use it to feed in ASTs
  produced by parsers other than cel-go (for example einride/aip-go
  emits `"="` / `"AND"` / `"NOT"` instead of the cel-go operator
  names). Function names absent from the map are used unchanged;
  aliases are not chained.
- `pgxcel.WithParamOffset(int)` — the first placeholder number.
  Defaults to `1`; values below `1` return an error. Use a higher
  value when splicing the fragment into a query that already has
  bound values.

A nil ast returns `("", nil, nil)`. An unchecked ast
(`ast.IsChecked() == false`) or one whose output type is not `bool`
returns an error. Errors are prefixed with `pgxcel:`.

## Operator coverage

| CEL                                  | Postgres fragment                       |
| ------------------------------------ | --------------------------------------- |
| `==`, `!=`, `<`, `<=`, `>`, `>=`     | `col op $N` (or `col op col`)           |
| `&&`, `\|\|`                         | `(lhs AND rhs)` / `(lhs OR rhs)`        |
| `!`                                  | `(NOT expr)`                            |
| `x in [a, b, c]`                     | `x IN ($1, $2, $3)` (empty → `FALSE`)   |
| `s.contains(x)`                      | `s LIKE '%' \|\| $N \|\| '%'`           |
| `s.startsWith(x)`                    | `s LIKE $N \|\| '%'`                    |
| `s.endsWith(x)`                      | `s LIKE '%' \|\| $N`                    |
| `s.matches(re)`                      | `s ~ $N` (POSIX regex)                  |
| `timestamp("2025-01-02T03:04:05Z")`  | `$N` bound as `time.Time` in UTC        |
| `duration("1h30m")`                  | `$N` bound as `time.Duration`           |
| unary `-<literal>`                   | bound as signed numeric literal         |

The `contains` / `startsWith` / `endsWith` argument has its LIKE
metacharacters (`%`, `_`, `\`) escaped so it matches literally, as in
CEL. Comparison, `IN`, `LIKE` and `~` predicates nested as operands of
another operator are parenthesized to preserve CEL precedence. When
both sides of a comparison (or every operand of `in`) are literals,
the left-hand placeholder gets an explicit cast (e.g. `$1::bigint`)
so Postgres can infer the parameter types.

### Limitations

- Anything not listed above is rejected with an error, including
  `has()`, comprehension macros (`all`, `exists`, ...), arithmetic,
  the ternary operator, `null` and bytes literals, and negating a
  non-literal.
- `in` requires a list literal on the right-hand side.
- Both sides of a comparison, and every `in` element, must have the
  same CEL type. Mixed types (`age in [1, 2.5]`, or `age < 2.5` with
  `cel.CrossTypeNumericComparisons`) are rejected, since Postgres
  would coerce the bound value to the column type. A literal compared
  with a `dyn` (or `google.protobuf.Any`) operand is instead cast to
  its CEL type, so Postgres either compares as CEL would or errors.
- `timestamp(...)` and `duration(...)` literals must be whole
  microseconds, the precision of Postgres `timestamp` / `interval`.
- Literals compared with a typed column are bound without a cast, so
  a value outside the column's range (e.g. `3000000000` against an
  `integer` column) fails at query time rather than comparing as CEL
  would.
- String ordering (`<`, `>`, ...) follows the column's collation,
  whereas CEL compares by code point.
- `matches` uses Postgres POSIX regular expressions, not RE2; patterns
  relying on RE2-only syntax behave differently.
- SQL three-valued logic applies: a predicate on a `NULL` column is
  `NULL`, so rows with `NULL` values are excluded by both `x == v` and
  `!(x == v)`.

## Development

```bash
go test ./...
go vet ./...
```

The Postgres integration tests run each filter through `Where` against
a real database and check that it selects the same rows cel-go
evaluates as true. They are skipped unless `PGX_DATABASE_URL` is set.
The devcontainer provides Postgres and sets it; from the host, start
the devcontainer and run the tests inside `nix develop`, which exports
`PGX_DATABASE_URL` rewritten to the mapped host port:

```bash
devcontainer up --workspace-folder .
nix develop --command go test ./...
```

## License

[MIT](LICENSE)
