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
where, args, err := pgxcel.Transpile(ast, pgxcel.WithColumns(columns))
// where: ("users"."name" = $1 AND "users"."age" > $2)
// args:  []any{"Alice", int64(30)}
```

### Options

- `pgxcel.WithColumns(map[string]string)` — the path → DB-column
  allow-list. Lookup is **fail-closed**: any identifier the AST
  references that is not in the map causes `Transpile` to return an
  error. When omitted, every ident in the AST errors. **Never feed
  user input as a column name**; the value of each map entry is
  emitted into the SQL after only identifier quoting.
- `pgxcel.WithParamOffset(int)` — the first placeholder number.
  Defaults to `1`. Use a higher value when splicing the fragment into
  a query that already has bound values.

A nil ast returns `("", nil, nil)`. An unchecked ast
(`ast.IsChecked() == false`) returns an error.

## Operator coverage

| CEL                                  | Postgres fragment                       |
| ------------------------------------ | --------------------------------------- |
| `==`, `!=`, `<`, `<=`, `>`, `>=`     | `col op $N` (or `col op col`)           |
| `&&`, `\|\|`                         | `(lhs AND rhs)` / `(lhs OR rhs)`        |
| `!`                                  | `(NOT expr)`                            |
| `timestamp("2025-01-02T03:04:05Z")`  | `$N` bound as `time.Time`               |
| `duration("1h30m")`                  | `$N` bound as `time.Duration`           |
| unary `-<literal>`                   | bound as signed numeric literal         |

For ASTs originating from an einride/aip parser, the AIP-160-only
operators are also supported:

| AIP-160                              | Postgres fragment                       |
| ------------------------------------ | --------------------------------------- |
| `name:"ali"` (has)                   | `"name" ILIKE '%' \|\| $N \|\| '%'`     |
| whitespace AND (`FUZZY`)             | same as `&&`                            |

## Development

```bash
go test ./...
go vet ./...
```

## License

[MIT](LICENSE)
