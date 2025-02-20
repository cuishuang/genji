package docs_test

import (
	"strconv"
	"testing"

	"github.com/genjidb/genji/document"
	"github.com/genjidb/genji/internal/environment"
	"github.com/genjidb/genji/internal/expr"
	"github.com/genjidb/genji/internal/expr/functions"
	"github.com/genjidb/genji/internal/sql/parser"
	"github.com/genjidb/genji/internal/stream"
	"github.com/genjidb/genji/internal/stream/docs"
	"github.com/genjidb/genji/internal/stream/table"
	"github.com/genjidb/genji/internal/testutil"
	"github.com/genjidb/genji/internal/testutil/assert"
	"github.com/genjidb/genji/types"
	"github.com/stretchr/testify/require"
)

func TestAggregate(t *testing.T) {
	tests := []struct {
		name     string
		groupBy  expr.Expr
		builders []expr.AggregatorBuilder
		in       []types.Document
		want     []types.Document
		fails    bool
	}{
		{
			"fake count",
			nil,
			makeAggregatorBuilders("agg"),
			[]types.Document{testutil.MakeDocument(t, `{"a": 10}`)},
			[]types.Document{testutil.MakeDocument(t, `{"agg": 1}`)},
			false,
		},
		{
			"count",
			nil,
			[]expr.AggregatorBuilder{&functions.Count{Wildcard: true}},
			[]types.Document{testutil.MakeDocument(t, `{"a": 10}`)},
			[]types.Document{testutil.MakeDocument(t, `{"COUNT(*)": 1}`)},
			false,
		},
		{
			"count/groupBy",
			parser.MustParseExpr("a % 2"),
			[]expr.AggregatorBuilder{&functions.Count{Expr: parser.MustParseExpr("a")}, &functions.Avg{Expr: parser.MustParseExpr("a")}},
			generateSeqDocs(t, 10),
			[]types.Document{testutil.MakeDocument(t, `{"a % 2": 0, "COUNT(a)": 5, "AVG(a)": 4.0}`), testutil.MakeDocument(t, `{"a % 2": 1, "COUNT(a)": 5, "AVG(a)": 5.0}`)},
			false,
		},
		{
			"count/noInput",
			nil,
			[]expr.AggregatorBuilder{&functions.Count{Expr: parser.MustParseExpr("a")}, &functions.Avg{Expr: parser.MustParseExpr("a")}},
			nil,
			[]types.Document{testutil.MakeDocument(t, `{"COUNT(a)": 0, "AVG(a)": 0.0}`)},
			false,
		},
		{
			"no aggregator",
			parser.MustParseExpr("a % 2"),
			nil,
			generateSeqDocs(t, 4),
			testutil.MakeDocuments(t, `{"a % 2": 0}`, `{"a % 2": 1}`),
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, tx, cleanup := testutil.NewTestTx(t)
			defer cleanup()

			testutil.MustExec(t, db, tx, "CREATE TABLE test(a int)")

			for _, doc := range test.in {
				testutil.MustExec(t, db, tx, "INSERT INTO test VALUES ?", environment.Param{Value: doc})
			}

			var env environment.Environment
			env.DB = db
			env.Tx = tx
			env.Catalog = db.Catalog

			s := stream.New(table.Scan("test"))
			if test.groupBy != nil {
				s = s.Pipe(docs.TempTreeSort(test.groupBy))
			}

			s = s.Pipe(docs.GroupAggregate(test.groupBy, test.builders...))

			var got []types.Document
			err := s.Iterate(&env, func(env *environment.Environment) error {
				d, ok := env.GetDocument()
				require.True(t, ok)
				var fb document.FieldBuffer
				fb.Copy(d)
				got = append(got, &fb)
				return nil
			})
			if test.fails {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				for i, doc := range test.want {
					testutil.RequireDocEqual(t, doc, got[i])
				}

				require.Equal(t, len(test.want), len(got))
			}
		})
	}

	t.Run("String", func(t *testing.T) {
		require.Equal(t, `docs.GroupAggregate(a % 2, a(), b())`, docs.GroupAggregate(parser.MustParseExpr("a % 2"), makeAggregatorBuilders("a()", "b()")...).String())
		require.Equal(t, `docs.GroupAggregate(NULL, a(), b())`, docs.GroupAggregate(nil, makeAggregatorBuilders("a()", "b()")...).String())
		require.Equal(t, `docs.GroupAggregate(a % 2)`, docs.GroupAggregate(parser.MustParseExpr("a % 2")).String())
	})
}

type fakeAggregator struct {
	count int64
	name  string
}

func (f *fakeAggregator) Eval(env *environment.Environment) (types.Value, error) {
	return types.NewIntegerValue(f.count), nil
}

func (f *fakeAggregator) Aggregate(env *environment.Environment) error {
	f.count++
	return nil
}

func (f *fakeAggregator) Name() string {
	return f.name
}

func (f *fakeAggregator) String() string {
	return f.name
}

type fakeAggretatorBuilder struct {
	expr.Expr
	name string
}

func (f *fakeAggretatorBuilder) Aggregator() expr.Aggregator {
	return &fakeAggregator{
		name: f.name,
	}
}

func (f *fakeAggretatorBuilder) String() string {
	return f.name
}

func makeAggregatorBuilders(names ...string) []expr.AggregatorBuilder {
	aggs := make([]expr.AggregatorBuilder, len(names))
	for i := range names {
		aggs[i] = &fakeAggretatorBuilder{
			name: names[i],
		}
	}

	return aggs
}

func generateSeqDocs(t testing.TB, max int) (docs []types.Document) {
	t.Helper()

	for i := 0; i < max; i++ {
		docs = append(docs, testutil.MakeDocument(t, `{"a": `+strconv.Itoa(i)+`}`))
	}

	return docs
}
