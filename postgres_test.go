package pgxcel

import (
	"context"
	"os"
	"slices"
	"time"

	"cel.dev/cel-go/cel"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// person is a row of the people table, mirrored in Go so each filter
// can be evaluated by cel-go and compared with what Postgres returns.
type person struct {
	id      int64
	name    string
	age     int64
	score   float64
	active  bool
	created time.Time     // timestamptz
	local   time.Time     // timestamp (without time zone), UTC wall clock
	ttl     time.Duration // interval
}

var people = []person{
	{1, "Alice", 30, 8.5, true, date(2025, 1, 2, 3, 4, 5), date(2025, 1, 2, 1, 4, 5), time.Hour},
	{2, "Bob", 8, 2.5, false, date(2024, 6, 1, 0, 0, 0), date(2025, 1, 2, 3, 4, 5), 90 * time.Minute},
	{3, `a50%_\x`, 41, 0, true, date(2023, 3, 3, 12, 0, 0), date(2023, 3, 3, 12, 0, 0), 0},
	{4, "carol_b", 17, -1.5, false, date(2025, 1, 2, 3, 4, 5).Add(time.Microsecond), date(2020, 1, 1, 0, 0, 0), time.Second},
}

func date(y int, m time.Month, d, h, mi, s int) time.Time {
	return time.Date(y, m, d, h, mi, s, 0, time.UTC)
}

func (p person) activation() map[string]any {
	return map[string]any{
		"name": p.name, "age": p.age, "score": p.score, "active": p.active,
		"created": p.created, "local": p.local, "ttl": p.ttl, "x": p.age,
	}
}

var peopleColumns = map[string]string{
	"name": "name", "age": "age", "score": "score", "active": "active",
	"created": "created", "local": "local", "ttl": "ttl", "x": "age",
}

var peopleEnv = []cel.EnvOption{
	cel.Variable("name", cel.StringType),
	cel.Variable("age", cel.IntType),
	cel.Variable("score", cel.DoubleType),
	cel.Variable("active", cel.BoolType),
	cel.Variable("created", cel.TimestampType),
	cel.Variable("local", cel.TimestampType),
	cel.Variable("ttl", cel.DurationType),
	cel.Variable("x", cel.DynType),
}

var _ = Describe("Where against Postgres", Ordered, func() {
	var conn *pgx.Conn

	BeforeAll(func(ctx context.Context) {
		url := os.Getenv("PGX_DATABASE_URL")
		if url == "" {
			Skip("PGX_DATABASE_URL not set")
		}
		var err error
		conn, err = pgx.Connect(ctx, url)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx context.Context) { Expect(conn.Close(ctx)).To(Succeed()) })

		_, err = conn.Exec(ctx, `CREATE TEMP TABLE people (
			id bigint PRIMARY KEY,
			name text NOT NULL,
			age integer NOT NULL,
			score double precision NOT NULL,
			active boolean NOT NULL,
			created timestamptz NOT NULL,
			local timestamp NOT NULL,
			ttl interval NOT NULL
		)`)
		Expect(err).NotTo(HaveOccurred())
		for _, p := range people {
			_, err = conn.Exec(ctx, `INSERT INTO people VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				p.id, p.name, p.age, p.score, p.active, p.created, p.local, p.ttl)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	// celMatches returns the ids of the people for which cel-go itself
	// evaluates src to true.
	celMatches := func(src string) []int64 {
		env, err := cel.NewEnv(peopleEnv...)
		Expect(err).NotTo(HaveOccurred())
		ast, iss := env.Compile(src)
		Expect(iss.Err()).NotTo(HaveOccurred())
		prg, err := env.Program(ast)
		Expect(err).NotTo(HaveOccurred())
		ids := []int64{}
		for _, p := range people {
			out, _, err := prg.Eval(p.activation())
			Expect(err).NotTo(HaveOccurred())
			if out.Value() == true {
				ids = append(ids, p.id)
			}
		}
		return ids
	}

	sqlMatches := func(ctx context.Context, src string, mode pgx.QueryExecMode) []int64 {
		where, args, err := Where(mustCompile(src, peopleEnv...), WithColumns(peopleColumns))
		Expect(err).NotTo(HaveOccurred())
		rows, err := conn.Query(ctx, "SELECT id FROM people WHERE "+where+" ORDER BY id",
			append([]any{mode}, args...)...)
		Expect(err).NotTo(HaveOccurred())
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		Expect(err).NotTo(HaveOccurred(), "SQL: %s", where)
		return ids
	}

	DescribeTable("selects the same rows as cel-go evaluates",
		func(ctx context.Context, src string) {
			want := celMatches(src)
			Expect(slices.IsSorted(want)).To(BeTrue())
			for _, mode := range []pgx.QueryExecMode{pgx.QueryExecModeCacheStatement, pgx.QueryExecModeSimpleProtocol} {
				Expect(sqlMatches(ctx, src, mode)).To(Equal(want), "mode %v", mode)
			}
		},
		Entry(nil, `name == "Alice"`),
		Entry(nil, `name != "Alice"`),
		Entry(nil, `age >= 17 && age < 41`),
		Entry(nil, `age < 10 || score > 8.0`),
		Entry(nil, `!(age == 8)`),
		Entry(nil, `active`),
		Entry(nil, `!active`),
		Entry(nil, `active == false`),
		Entry(nil, `score > -2.0`),
		Entry(nil, `age > -5`),
		Entry(nil, `age in [8, 17, 99]`),
		Entry(nil, `name in []`),
		Entry(nil, `name.contains("o")`),
		Entry(nil, `name.contains("%")`),
		Entry(nil, `name.contains("_")`),
		Entry(nil, `name.contains("\\")`),
		Entry(nil, `name.startsWith("a50%")`),
		Entry(nil, `name.endsWith("_b")`),
		Entry(nil, `name.matches("^[A-Z]")`),
		Entry(nil, `"Alice and Bob".contains(name)`),
		Entry(nil, `created > timestamp("2025-01-02T03:04:05Z")`),
		Entry(nil, `created == timestamp("2025-01-02T05:04:05+02:00")`),
		Entry(nil, `local == timestamp("2025-01-02T03:04:05+02:00")`),
		Entry(nil, `ttl >= duration("1h")`),
		Entry(nil, `ttl == duration("1s")`),
		Entry(nil, `(age == 8) == (score == 2.5)`),
		Entry(nil, `(age < 20) in [true]`),
		Entry(nil, `2 < 10 && age == 30`),
		Entry(nil, `1.5 > 2.5 || active`),
		Entry(nil, `"x" == "x" && age == 8`),
		Entry(nil, `timestamp("2025-01-01T00:00:00Z") < timestamp("2026-01-01T00:00:00Z")`),
		Entry(nil, `duration("1s") > duration("0s") && !active`),
		Entry(nil, `-1 in [1, -1] && age == 41`),
		Entry(nil, `x == 8`),
		Entry(nil, `x == 8.0`),
		Entry(nil, `x == 8.6`),
		Entry(nil, `x in [8.6, 30]`),
		Entry(nil, `8.6 == x`),
	)

	It("errors instead of coercing a mixed-type list against a dyn column", func(ctx context.Context) {
		where, args, err := Where(mustCompile(`x in [1, "8"]`, peopleEnv...), WithColumns(peopleColumns))
		Expect(err).NotTo(HaveOccurred())
		_, err = conn.Exec(ctx, "SELECT id FROM people WHERE "+where, args...)
		Expect(err).To(MatchError(ContainSubstring("operator does not exist")))
	})
})
