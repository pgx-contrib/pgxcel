package pgxcel

import (
	"time"

	"github.com/google/cel-go/cel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

var _ = Describe("Transpile", func() {
	It("emits an equality predicate with a bound arg", func() {
		ast := mustCompile(`name == "Alice"`, cel.Variable("name", cel.StringType))
		where, args, err := Transpile(ast, WithColumns(map[string]string{"name": "name"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`"name" = $1`))
		Expect(args).To(Equal([]any{"Alice"}))
	})

	It("maps AIP paths to their backing DB columns", func() {
		ast := mustCompile(`title == "The Go Programming Language"`, cel.Variable("title", cel.StringType))
		where, args, err := Transpile(ast, WithColumns(map[string]string{"title": "book_title"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`"book_title" = $1`))
		Expect(args).To(Equal([]any{"The Go Programming Language"}))
	})

	It("returns empty when ast is nil", func() {
		where, args, err := Transpile(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(BeEmpty())
		Expect(args).To(BeEmpty())
	})

	It("rejects an unchecked AST", func() {
		env, err := cel.NewEnv()
		Expect(err).NotTo(HaveOccurred())
		ast, iss := env.Parse(`1 == 1`)
		Expect(iss.Err()).NotTo(HaveOccurred())
		_, _, err = Transpile(ast)
		Expect(err).To(MatchError(ContainSubstring("unchecked ast")))
	})

	It("returns rhs error from a comparison", func() {
		ast := mustCompile(`id == other`,
			cel.Variable("id", cel.IntType),
			cel.Variable("other", cel.IntType))
		_, _, err := Transpile(ast, WithColumns(map[string]string{"id": "id"}))
		Expect(err).To(MatchError(ContainSubstring(`unknown field "other"`)))
	})

	It("combines AND with parentheses per branch", func() {
		ast := mustCompile(`name == "Alice" && age > 30`,
			cel.Variable("name", cel.StringType),
			cel.Variable("age", cel.IntType))
		where, args, err := Transpile(ast,
			WithColumns(map[string]string{"name": "name", "age": "age"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`("name" = $1 AND "age" > $2)`))
		Expect(args).To(Equal([]any{"Alice", int64(30)}))
	})

	It("combines OR with parentheses per branch", func() {
		ast := mustCompile(`name == "Alice" || name == "Bob"`, cel.Variable("name", cel.StringType))
		where, args, err := Transpile(ast, WithColumns(map[string]string{"name": "name"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`("name" = $1 OR "name" = $2)`))
		Expect(args).To(Equal([]any{"Alice", "Bob"}))
	})

	It("wraps NOT in parentheses", func() {
		ast := mustCompile(`!(name == "Alice")`, cel.Variable("name", cel.StringType))
		where, args, err := Transpile(ast, WithColumns(map[string]string{"name": "name"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`(NOT "name" = $1)`))
		Expect(args).To(Equal([]any{"Alice"}))
	})

	It("binds timestamp literals as time.Time", func() {
		ast := mustCompile(`create_time > timestamp("2025-01-02T03:04:05Z")`,
			cel.Variable("create_time", cel.TimestampType))
		where, args, err := Transpile(ast,
			WithColumns(map[string]string{"create_time": "created_at"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`"created_at" > $1`))
		Expect(args).To(HaveLen(1))
		Expect(args[0]).To(BeAssignableToTypeOf(time.Time{}))
		Expect(args[0].(time.Time)).To(BeTemporally("==", time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)))
	})

	It("binds duration literals as time.Duration", func() {
		ast := mustCompile(`timeout > duration("1h30m")`,
			cel.Variable("timeout", cel.DurationType))
		where, args, err := Transpile(ast, WithColumns(map[string]string{"timeout": "timeout"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`"timeout" > $1`))
		Expect(args).To(Equal([]any{90 * time.Minute}))
	})

	It("folds unary minus on numeric literals", func() {
		ast := mustCompile(`balance > -5`, cel.Variable("balance", cel.IntType))
		where, args, err := Transpile(ast, WithColumns(map[string]string{"balance": "balance"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`"balance" > $1`))
		Expect(args).To(Equal([]any{int64(-5)}))
	})

	It("supports comparisons between two columns", func() {
		ast := mustCompile(`updated > created`,
			cel.Variable("updated", cel.TimestampType),
			cel.Variable("created", cel.TimestampType))
		where, args, err := Transpile(ast,
			WithColumns(map[string]string{"updated": "updated_at", "created": "created_at"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`"updated_at" > "created_at"`))
		Expect(args).To(BeEmpty())
	})

	It("starts placeholders at 1 by default", func() {
		ast := mustCompile(`name == "Alice"`, cel.Variable("name", cel.StringType))
		where, _, err := Transpile(ast, WithColumns(map[string]string{"name": "name"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`"name" = $1`))
	})

	It("propagates a custom WithParamOffset through every placeholder", func() {
		ast := mustCompile(`name == "Alice" && age > 30`,
			cel.Variable("name", cel.StringType),
			cel.Variable("age", cel.IntType))
		where, args, err := Transpile(ast,
			WithColumns(map[string]string{"name": "name", "age": "age"}),
			WithParamOffset(5))
		Expect(err).NotTo(HaveOccurred())
		Expect(where).To(Equal(`("name" = $5 AND "age" > $6)`))
		Expect(args).To(Equal([]any{"Alice", int64(30)}))
	})

	It("fails closed when a filter field is not in columns", func() {
		ast := mustCompile(`name == "Alice"`, cel.Variable("name", cel.StringType))
		_, _, err := Transpile(ast, WithColumns(map[string]string{"other": "other"}))
		Expect(err).To(MatchError(ContainSubstring(`unknown field "name"`)))
	})

	It("fails closed when WithColumns is omitted", func() {
		ast := mustCompile(`name == "Alice"`, cel.Variable("name", cel.StringType))
		_, _, err := Transpile(ast)
		Expect(err).To(MatchError(ContainSubstring(`unknown field "name"`)))
	})
})

var _ = Describe("transpiler internals", func() {
	Describe("transpile", func() {
		It("errors on an unsupported expression kind", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpile(&exprpb.Expr{
				ExprKind: &exprpb.Expr_ListExpr{ListExpr: &exprpb.Expr_CreateList{}},
			})
			Expect(err).To(MatchError(ContainSubstring("unsupported expression kind")))
		})

		It("errors when a Select operand is not an identifier chain", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpile(&exprpb.Expr{
				ExprKind: &exprpb.Expr_SelectExpr{
					SelectExpr: &exprpb.Expr_Select{
						Operand: &exprpb.Expr{
							ExprKind: &exprpb.Expr_ListExpr{ListExpr: &exprpb.Expr_CreateList{}},
						},
						Field: "x",
					},
				},
			})
			Expect(err).To(MatchError(ContainSubstring("unsupported identifier expression")))
		})

		It("resolves a dotted Select path through the column map", func() {
			t := &transpiler{
				columns:     map[string]string{"address.city": "addr.city"},
				paramOffset: 1,
			}
			out, err := t.transpile(&exprpb.Expr{
				ExprKind: &exprpb.Expr_SelectExpr{
					SelectExpr: &exprpb.Expr_Select{
						Operand: &exprpb.Expr{
							ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "address"}},
						},
						Field: "city",
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal(`"addr"."city"`))
		})
	})

	Describe("transpileConst", func() {
		It("binds a uint64 value", func() {
			t := &transpiler{paramOffset: 1}
			out, err := t.transpileConst(&exprpb.Constant{
				ConstantKind: &exprpb.Constant_Uint64Value{Uint64Value: 42},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("$1"))
			Expect(t.args).To(Equal([]any{uint64(42)}))
		})

		It("binds a double value", func() {
			t := &transpiler{paramOffset: 1}
			out, err := t.transpileConst(&exprpb.Constant{
				ConstantKind: &exprpb.Constant_DoubleValue{DoubleValue: 3.14},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("$1"))
			Expect(t.args).To(Equal([]any{3.14}))
		})

		It("binds a bool value", func() {
			t := &transpiler{paramOffset: 1}
			out, err := t.transpileConst(&exprpb.Constant{
				ConstantKind: &exprpb.Constant_BoolValue{BoolValue: true},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("$1"))
			Expect(t.args).To(Equal([]any{true}))
		})

		It("respects paramOffset for the first placeholder", func() {
			t := &transpiler{paramOffset: 5}
			out, err := t.transpileConst(&exprpb.Constant{
				ConstantKind: &exprpb.Constant_Int64Value{Int64Value: 1},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("$5"))
		})

		It("errors on an unsupported constant kind", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpileConst(&exprpb.Constant{
				ConstantKind: &exprpb.Constant_NullValue{},
			})
			Expect(err).To(MatchError(ContainSubstring("unsupported constant kind")))
		})
	})

	Describe("transpileCall", func() {
		It("errors on an unsupported function", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpileCall(&exprpb.Expr_Call{Function: "unknown"})
			Expect(err).To(MatchError(ContainSubstring("unsupported function")))
		})

		It("rejects unary minus on a non-literal operand", func() {
			t := &transpiler{paramOffset: 1, columns: map[string]string{"age": "age"}}
			ident := &exprpb.Expr{
				ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "age"}},
			}
			_, err := t.transpileCall(&exprpb.Expr_Call{Function: "-_", Args: []*exprpb.Expr{ident}})
			Expect(err).To(MatchError(ContainSubstring("unary minus requires a numeric literal")))
		})

		It("rejects unary minus with the wrong arity", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpileCall(&exprpb.Expr_Call{Function: "-_"})
			Expect(err).To(MatchError(ContainSubstring("unary minus expects 1 argument")))
		})

		It("rejects unary minus on a non-numeric literal", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpileCall(&exprpb.Expr_Call{
				Function: "-_",
				Args: []*exprpb.Expr{{
					ExprKind: &exprpb.Expr_ConstExpr{
						ConstExpr: &exprpb.Constant{
							ConstantKind: &exprpb.Constant_StringValue{StringValue: "x"},
						},
					},
				}},
			})
			Expect(err).To(MatchError(ContainSubstring("unary minus requires a numeric literal")))
		})

		It("folds unary minus on an int64 literal", func() {
			t := &transpiler{paramOffset: 1}
			out, err := t.transpileCall(&exprpb.Expr_Call{
				Function: "-_",
				Args: []*exprpb.Expr{{
					ExprKind: &exprpb.Expr_ConstExpr{
						ConstExpr: &exprpb.Constant{
							ConstantKind: &exprpb.Constant_Int64Value{Int64Value: 7},
						},
					},
				}},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("$1"))
			Expect(t.args).To(Equal([]any{int64(-7)}))
		})

		It("folds unary minus on a double literal", func() {
			t := &transpiler{paramOffset: 1}
			out, err := t.transpileCall(&exprpb.Expr_Call{
				Function: "-_",
				Args: []*exprpb.Expr{{
					ExprKind: &exprpb.Expr_ConstExpr{
						ConstExpr: &exprpb.Constant{
							ConstantKind: &exprpb.Constant_DoubleValue{DoubleValue: 2.5},
						},
					},
				}},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("$1"))
			Expect(t.args).To(Equal([]any{-2.5}))
		})

		It("rejects NOT with the wrong arity", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpileCall(&exprpb.Expr_Call{Function: "!_"})
			Expect(err).To(MatchError(ContainSubstring("NOT expects 1 argument")))
		})

		It("propagates errors from inside NOT", func() {
			t := &transpiler{paramOffset: 1}
			bad := &exprpb.Expr{
				ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "missing"}},
			}
			_, err := t.transpileCall(&exprpb.Expr_Call{Function: "!_", Args: []*exprpb.Expr{bad}})
			Expect(err).To(MatchError(ContainSubstring(`unknown field "missing"`)))
		})

		It("rejects has (`:`) with the wrong arity", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpileCall(&exprpb.Expr_Call{Function: ":"})
			Expect(err).To(MatchError(ContainSubstring(": expects 2 arguments")))
		})

		It("propagates lhs errors from `:`", func() {
			t := &transpiler{paramOffset: 1}
			bad := &exprpb.Expr{
				ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "missing"}},
			}
			rhs := &exprpb.Expr{
				ExprKind: &exprpb.Expr_ConstExpr{ConstExpr: &exprpb.Constant{
					ConstantKind: &exprpb.Constant_StringValue{StringValue: "x"},
				}},
			}
			_, err := t.transpileCall(&exprpb.Expr_Call{Function: ":", Args: []*exprpb.Expr{bad, rhs}})
			Expect(err).To(MatchError(ContainSubstring(`unknown field "missing"`)))
		})

		It("propagates rhs errors from `:`", func() {
			t := &transpiler{paramOffset: 1, columns: map[string]string{"name": "name"}}
			lhs := &exprpb.Expr{
				ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "name"}},
			}
			bad := &exprpb.Expr{
				ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "missing"}},
			}
			_, err := t.transpileCall(&exprpb.Expr_Call{Function: ":", Args: []*exprpb.Expr{lhs, bad}})
			Expect(err).To(MatchError(ContainSubstring(`unknown field "missing"`)))
		})

		It("rejects timestamp() with the wrong arity", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpileCall(&exprpb.Expr_Call{Function: "timestamp"})
			Expect(err).To(MatchError(ContainSubstring("timestamp expects 1 argument")))
		})

		It("rejects timestamp() argument that is not a constant expression", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpileCall(&exprpb.Expr_Call{
				Function: "timestamp",
				Args: []*exprpb.Expr{{
					ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "x"}},
				}},
			})
			Expect(err).To(MatchError(ContainSubstring("argument must be a string literal")))
		})

		It("rejects timestamp() without a string literal argument", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpileCall(&exprpb.Expr_Call{
				Function: "timestamp",
				Args: []*exprpb.Expr{{
					ExprKind: &exprpb.Expr_ConstExpr{
						ConstExpr: &exprpb.Constant{
							ConstantKind: &exprpb.Constant_Int64Value{Int64Value: 1},
						},
					},
				}},
			})
			Expect(err).To(MatchError(ContainSubstring("argument must be a string literal")))
		})

		It("rejects timestamp() with an unparseable string", func() {
			t := &transpiler{paramOffset: 1}
			_, err := t.transpileCall(&exprpb.Expr_Call{
				Function: "timestamp",
				Args: []*exprpb.Expr{{
					ExprKind: &exprpb.Expr_ConstExpr{
						ConstExpr: &exprpb.Constant{
							ConstantKind: &exprpb.Constant_StringValue{StringValue: "not-a-date"},
						},
					},
				}},
			})
			Expect(err).To(MatchError(ContainSubstring("timestamp")))
		})

		It("emits ILIKE for the AIP-160 has operator", func() {
			t := &transpiler{
				paramOffset: 1,
				columns:     map[string]string{"name": "name"},
			}
			out, err := t.transpileCall(&exprpb.Expr_Call{
				Function: ":",
				Args: []*exprpb.Expr{
					{ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "name"}}},
					{ExprKind: &exprpb.Expr_ConstExpr{
						ConstExpr: &exprpb.Constant{
							ConstantKind: &exprpb.Constant_StringValue{StringValue: "ali"},
						},
					}},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal(`"name" ILIKE '%' || $1 || '%'`))
			Expect(t.args).To(Equal([]any{"ali"}))
		})

		It("treats AIP-160 FUZZY as a logical AND", func() {
			t := &transpiler{
				paramOffset: 1,
				columns:     map[string]string{"name": "name", "age": "age"},
			}
			lhs := &exprpb.Expr{ExprKind: &exprpb.Expr_CallExpr{CallExpr: &exprpb.Expr_Call{
				Function: "_==_",
				Args: []*exprpb.Expr{
					{ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "name"}}},
					{ExprKind: &exprpb.Expr_ConstExpr{
						ConstExpr: &exprpb.Constant{
							ConstantKind: &exprpb.Constant_StringValue{StringValue: "Alice"},
						},
					}},
				},
			}}}
			rhs := &exprpb.Expr{ExprKind: &exprpb.Expr_CallExpr{CallExpr: &exprpb.Expr_Call{
				Function: "_>_",
				Args: []*exprpb.Expr{
					{ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "age"}}},
					{ExprKind: &exprpb.Expr_ConstExpr{
						ConstExpr: &exprpb.Constant{
							ConstantKind: &exprpb.Constant_Int64Value{Int64Value: 30},
						},
					}},
				},
			}}}
			out, err := t.transpileCall(&exprpb.Expr_Call{
				Function: "FUZZY",
				Args:     []*exprpb.Expr{lhs, rhs},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal(`("name" = $1 AND "age" > $2)`))
			Expect(t.args).To(Equal([]any{"Alice", int64(30)}))
		})
	})
})

var _ = Describe("transpileComparison", func() {
	It("rejects an unmapped operator", func() {
		t := &transpiler{paramOffset: 1}
		_, err := t.transpileComparison(&exprpb.Expr_Call{Function: "_~_"})
		Expect(err).To(MatchError(ContainSubstring(`unsupported comparison "_~_"`)))
	})

	It("rejects the wrong number of arguments", func() {
		t := &transpiler{paramOffset: 1}
		_, err := t.transpileComparison(&exprpb.Expr_Call{Function: "_==_"})
		Expect(err).To(MatchError(ContainSubstring("= expects 2 arguments")))
	})
})

var _ = Describe("transpileBinary", func() {
	It("rejects the wrong number of arguments", func() {
		t := &transpiler{paramOffset: 1}
		_, err := t.transpileBinary(&exprpb.Expr_Call{Function: "_&&_"}, "AND")
		Expect(err).To(MatchError(ContainSubstring("AND expects 2 arguments")))
	})

	It("propagates lhs errors", func() {
		t := &transpiler{paramOffset: 1}
		bad := &exprpb.Expr{
			ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "missing"}},
		}
		ok := &exprpb.Expr{
			ExprKind: &exprpb.Expr_ConstExpr{ConstExpr: &exprpb.Constant{
				ConstantKind: &exprpb.Constant_BoolValue{BoolValue: true},
			}},
		}
		_, err := t.transpileBinary(&exprpb.Expr_Call{Args: []*exprpb.Expr{bad, ok}}, "AND")
		Expect(err).To(MatchError(ContainSubstring(`unknown field "missing"`)))
	})

	It("propagates rhs errors", func() {
		t := &transpiler{paramOffset: 1}
		ok := &exprpb.Expr{
			ExprKind: &exprpb.Expr_ConstExpr{ConstExpr: &exprpb.Constant{
				ConstantKind: &exprpb.Constant_BoolValue{BoolValue: true},
			}},
		}
		bad := &exprpb.Expr{
			ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "missing"}},
		}
		_, err := t.transpileBinary(&exprpb.Expr_Call{Args: []*exprpb.Expr{ok, bad}}, "AND")
		Expect(err).To(MatchError(ContainSubstring(`unknown field "missing"`)))
	})
})

var _ = Describe("stringLiteral", func() {
	It("returns false for a non-constant expression", func() {
		_, ok := stringLiteral(&exprpb.Expr{
			ExprKind: &exprpb.Expr_IdentExpr{IdentExpr: &exprpb.Expr_Ident{Name: "x"}},
		})
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("parsing helpers", func() {
	It("parses a duration string", func() {
		v, err := parseDuration("1h30m")
		Expect(err).NotTo(HaveOccurred())
		Expect(v).To(Equal(90 * time.Minute))
	})
})
