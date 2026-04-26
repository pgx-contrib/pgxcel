// Package pgxcel converts checked CEL expressions into PostgreSQL WHERE
// fragments with positional bind placeholders.
package pgxcel

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/overloads"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// comparisonSQL maps cel-go comparison operator names to their SQL
// rendering. The cel-go parser emits "_==_" / "_<_" / etc., which are
// not valid SQL on their own.
var comparisonSQL = map[string]string{
	operators.Equals:        "=",
	operators.NotEquals:     "!=",
	operators.Less:          "<",
	operators.LessEquals:    "<=",
	operators.Greater:       ">",
	operators.GreaterEquals: ">=",
}

// Option configures a Transpile call.
type Option func(*config)

type config struct {
	columns     map[string]string
	functions   map[string]string
	paramOffset int
}

// WithColumns supplies the fail-closed AIP-path → DB-column allow-list.
// Any ident in the AST that is absent from columns causes an error.
// Dotted paths (e.g. "address.city") are looked up by their full path.
// When omitted, every ident in the AST errors.
func WithColumns(columns map[string]string) Option {
	return func(c *config) { c.columns = columns }
}

// WithFunctions registers aliases that are normalized to canonical CEL
// function names before dispatch. Use this to feed in ASTs produced by
// parsers other than cel-go — for example einride/aip-go emits "=" /
// "AND" / "NOT" instead of operators.Equals / LogicalAnd / LogicalNot.
//
// Each map entry is alias → canonical, where canonical is one of the
// names recognized by Transpile (typically a value from the cel-go
// operators package). Unknown aliases are passed through unchanged.
func WithFunctions(functions map[string]string) Option {
	return func(c *config) { c.functions = functions }
}

// WithParamOffset sets the number of the first emitted placeholder.
// Placeholders are then numbered offset, offset+1, ... so callers can
// splice the fragment into a query that already has earlier bound
// values. Defaults to 1.
func WithParamOffset(n int) Option {
	return func(c *config) { c.paramOffset = n }
}

// Transpile turns ast into a Postgres WHERE fragment (no enclosing
// parentheses) plus the bound positional args.
//
// ast must be checked (ast.IsChecked() == true). An unchecked AST
// returns an error. A nil ast returns ("", nil, nil) — the caller
// decides whether to omit the WHERE keyword.
//
// Configure resolution and placeholder numbering via WithColumns and
// WithParamOffset.
func Transpile(ast *cel.Ast, opts ...Option) (string, []any, error) {
	if ast == nil {
		return "", nil, nil
	}
	checked, err := cel.AstToCheckedExpr(ast)
	if err != nil {
		return "", nil, fmt.Errorf("pgxcel: %w", err)
	}
	cfg := config{paramOffset: 1}
	for _, opt := range opts {
		opt(&cfg)
	}
	t := &transpiler{
		columns:     cfg.columns,
		functions:   cfg.functions,
		paramOffset: cfg.paramOffset,
	}
	sql, err := t.transpile(checked.GetExpr())
	if err != nil {
		return "", nil, err
	}
	return sql, t.args, nil
}

type transpiler struct {
	args        []any
	columns     map[string]string
	functions   map[string]string
	paramOffset int
}

func (t *transpiler) placeholder(v any) string {
	t.args = append(t.args, v)
	return fmt.Sprintf("$%d", t.paramOffset+len(t.args)-1)
}

func (t *transpiler) transpile(e *exprpb.Expr) (string, error) {
	switch v := e.ExprKind.(type) {
	case *exprpb.Expr_ConstExpr:
		return t.transpileConst(v.ConstExpr)
	case *exprpb.Expr_IdentExpr, *exprpb.Expr_SelectExpr:
		return t.transpileIdent(e)
	case *exprpb.Expr_CallExpr:
		return t.transpileCall(v.CallExpr)
	default:
		return "", fmt.Errorf("unsupported expression kind %T", v)
	}
}

func (t *transpiler) transpileIdent(e *exprpb.Expr) (string, error) {
	path, ok := identPath(e)
	if !ok {
		return "", fmt.Errorf("unsupported identifier expression %T", e.ExprKind)
	}
	col, ok := t.columns[path]
	if !ok {
		return "", fmt.Errorf("unknown field %q", path)
	}
	return quoteIdent(col), nil
}

// identPath reconstructs a dotted path (e.g. "address.city") from a
// chain of Ident/Select expressions. Returns false if the expression is
// not a pure identifier chain.
func identPath(e *exprpb.Expr) (string, bool) {
	switch v := e.ExprKind.(type) {
	case *exprpb.Expr_IdentExpr:
		return v.IdentExpr.Name, true
	case *exprpb.Expr_SelectExpr:
		op, ok := identPath(v.SelectExpr.Operand)
		if !ok {
			return "", false
		}
		return op + "." + v.SelectExpr.Field, true
	default:
		return "", false
	}
}

func (t *transpiler) transpileConst(c *exprpb.Constant) (string, error) {
	switch v := c.ConstantKind.(type) {
	case *exprpb.Constant_StringValue:
		return t.placeholder(v.StringValue), nil
	case *exprpb.Constant_Int64Value:
		return t.placeholder(v.Int64Value), nil
	case *exprpb.Constant_Uint64Value:
		return t.placeholder(v.Uint64Value), nil
	case *exprpb.Constant_DoubleValue:
		return t.placeholder(v.DoubleValue), nil
	case *exprpb.Constant_BoolValue:
		return t.placeholder(v.BoolValue), nil
	default:
		return "", fmt.Errorf("unsupported constant kind %T", v)
	}
}

func (t *transpiler) transpileCall(call *exprpb.Expr_Call) (string, error) {
	if alias, ok := t.functions[call.Function]; ok {
		call = &exprpb.Expr_Call{
			Target:   call.Target,
			Function: alias,
			Args:     call.Args,
		}
	}
	switch call.Function {
	case operators.Equals, operators.NotEquals,
		operators.Less, operators.LessEquals,
		operators.Greater, operators.GreaterEquals:
		return t.transpileComparison(call)

	case operators.LogicalAnd:
		return t.transpileBinary(call, "AND")

	case operators.LogicalOr:
		return t.transpileBinary(call, "OR")

	case operators.LogicalNot:
		if len(call.Args) != 1 {
			return "", fmt.Errorf("NOT expects 1 argument, got %d", len(call.Args))
		}
		operand, err := t.transpile(call.Args[0])
		if err != nil {
			return "", err
		}
		return "(NOT " + operand + ")", nil

	case overloads.Contains:
		return t.transpileLike(call, true, true)
	case overloads.StartsWith:
		return t.transpileLike(call, false, true)
	case overloads.EndsWith:
		return t.transpileLike(call, true, false)
	case overloads.Matches:
		return t.transpileMatches(call)

	case overloads.TypeConvertTimestamp:
		return t.transpileTimeFunc(call, parseTimestamp)

	case overloads.TypeConvertDuration:
		return t.transpileTimeFunc(call, parseDuration)

	case operators.Negate:
		return t.transpileUnaryMinus(call)

	default:
		return "", fmt.Errorf("unsupported function %q", call.Function)
	}
}

// transpileComparison handles =, !=, <, <=, >, >=. Each side may be an
// identifier (resolved through the column map) or a literal (bound as a
// placeholder). Column-to-column comparisons are supported.
func (t *transpiler) transpileComparison(call *exprpb.Expr_Call) (string, error) {
	op, ok := comparisonSQL[call.Function]
	if !ok {
		return "", fmt.Errorf("unsupported comparison %q", call.Function)
	}
	if len(call.Args) != 2 {
		return "", fmt.Errorf("%s expects 2 arguments, got %d", op, len(call.Args))
	}
	lhs, err := t.transpile(call.Args[0])
	if err != nil {
		return "", err
	}
	rhs, err := t.transpile(call.Args[1])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s %s", lhs, op, rhs), nil
}

func (t *transpiler) transpileBinary(call *exprpb.Expr_Call, op string) (string, error) {
	if len(call.Args) != 2 {
		return "", fmt.Errorf("%s expects 2 arguments, got %d", op, len(call.Args))
	}
	lhs, err := t.transpile(call.Args[0])
	if err != nil {
		return "", err
	}
	rhs, err := t.transpile(call.Args[1])
	if err != nil {
		return "", err
	}
	return "(" + lhs + " " + op + " " + rhs + ")", nil
}

// transpileLike renders the cel-go string membership functions
// (string.contains, string.startsWith, string.endsWith) as SQL LIKE
// patterns. leadingPct/trailingPct control the wildcard placement:
//
//	contains:   leading=true  trailing=true   →  LIKE '%' || rhs || '%'
//	startsWith: leading=false trailing=true   →  LIKE rhs || '%'
//	endsWith:   leading=true  trailing=false  →  LIKE '%' || rhs
//
// Accepts both the method-style call (`s.contains(x)` → Target=s,
// Args=[x]) and the function-style call (`contains(s, x)` → Target=nil,
// Args=[s, x]) so callers that synthesize ASTs from non-cel-go parsers
// (via WithFunctions aliases) work without an extra rewrite step.
func (t *transpiler) transpileLike(call *exprpb.Expr_Call, leadingPct, trailingPct bool) (string, error) {
	lhsExpr, rhsExpr, err := stringMethodArgs(call)
	if err != nil {
		return "", err
	}
	lhs, err := t.transpile(lhsExpr)
	if err != nil {
		return "", err
	}
	rhs, err := t.transpile(rhsExpr)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(lhs)
	sb.WriteString(" LIKE ")
	if leadingPct {
		sb.WriteString("'%' || ")
	}
	sb.WriteString(rhs)
	if trailingPct {
		sb.WriteString(" || '%'")
	}
	return sb.String(), nil
}

// transpileMatches renders string.matches(re) as POSIX regex (`~`).
func (t *transpiler) transpileMatches(call *exprpb.Expr_Call) (string, error) {
	lhsExpr, rhsExpr, err := stringMethodArgs(call)
	if err != nil {
		return "", err
	}
	lhs, err := t.transpile(lhsExpr)
	if err != nil {
		return "", err
	}
	rhs, err := t.transpile(rhsExpr)
	if err != nil {
		return "", err
	}
	return lhs + " ~ " + rhs, nil
}

// stringMethodArgs unpacks a CEL string method call into (receiver,
// argument) regardless of whether the AST uses the method form (Target
// set, one Arg) or the function form (no Target, two Args).
func stringMethodArgs(call *exprpb.Expr_Call) (lhs, rhs *exprpb.Expr, err error) {
	if call.Target != nil {
		if len(call.Args) != 1 {
			return nil, nil, fmt.Errorf("%s expects 1 argument, got %d", call.Function, len(call.Args))
		}
		return call.Target, call.Args[0], nil
	}
	if len(call.Args) != 2 {
		return nil, nil, fmt.Errorf("%s expects 2 arguments, got %d", call.Function, len(call.Args))
	}
	return call.Args[0], call.Args[1], nil
}

// transpileTimeFunc binds timestamp("...") / duration("...") literals
// as concrete time.Time / time.Duration values so drivers can marshal
// them natively. The CEL argument must be a string constant.
func (t *transpiler) transpileTimeFunc(
	call *exprpb.Expr_Call,
	parse func(string) (any, error),
) (string, error) {
	if len(call.Args) != 1 {
		return "", fmt.Errorf("%s expects 1 argument, got %d", call.Function, len(call.Args))
	}
	s, ok := stringLiteral(call.Args[0])
	if !ok {
		return "", fmt.Errorf("%s argument must be a string literal", call.Function)
	}
	v, err := parse(s)
	if err != nil {
		return "", fmt.Errorf("%s: %w", call.Function, err)
	}
	return t.placeholder(v), nil
}

// transpileUnaryMinus folds -<literal> into a single signed
// placeholder. Anything else (e.g. -column) is rejected; CEL's type
// checker normally blocks those before reaching us.
func (t *transpiler) transpileUnaryMinus(call *exprpb.Expr_Call) (string, error) {
	if len(call.Args) != 1 {
		return "", fmt.Errorf("unary minus expects 1 argument, got %d", len(call.Args))
	}
	c, ok := call.Args[0].ExprKind.(*exprpb.Expr_ConstExpr)
	if !ok {
		return "", fmt.Errorf("unary minus requires a numeric literal")
	}
	switch v := c.ConstExpr.ConstantKind.(type) {
	case *exprpb.Constant_Int64Value:
		return t.placeholder(-v.Int64Value), nil
	case *exprpb.Constant_DoubleValue:
		return t.placeholder(-v.DoubleValue), nil
	default:
		return "", fmt.Errorf("unary minus requires a numeric literal")
	}
}

func stringLiteral(e *exprpb.Expr) (string, bool) {
	c, ok := e.ExprKind.(*exprpb.Expr_ConstExpr)
	if !ok {
		return "", false
	}
	s, ok := c.ConstExpr.ConstantKind.(*exprpb.Constant_StringValue)
	if !ok {
		return "", false
	}
	return s.StringValue, true
}

func parseTimestamp(s string) (any, error) {
	return time.Parse(time.RFC3339, s)
}

func parseDuration(s string) (any, error) {
	return time.ParseDuration(s)
}

// quoteIdent quotes each dot-separated segment of a column path as a
// Postgres identifier. Embedded double quotes are escaped by doubling,
// matching pg_quote_ident semantics.
func quoteIdent(path string) string {
	parts := strings.Split(path, ".")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
	}
	return strings.Join(out, ".")
}
