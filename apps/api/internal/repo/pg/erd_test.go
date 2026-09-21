package pg_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"doelab/api/internal/testutil"
)

var update = flag.Bool("update", false, "rewrite the diagram in packages/db/README.md")

const (
	erdStart = "<!-- erd:start -->"
	erdEnd   = "<!-- erd:end -->"
)

// The entity-relationship diagram in packages/db/README.md is generated from
// the catalog, and this test fails when the two differ: the picture a reader
// sees is the schema that is deployed.
func TestERD(t *testing.T) {
	t.Parallel()
	pool := testutil.Postgres(t)
	ctx := context.Background()

	tables := []string{}
	for _, r := range resources {
		tables = append(tables, r.table)
	}
	sort.Strings(tables)

	var b strings.Builder
	b.WriteString("```mermaid\nerDiagram\n")
	for _, table := range tables {
		fmt.Fprintf(&b, "  %s {\n", table)
		rows, err := pool.Query(ctx, `
			SELECT c.column_name::text,
			       CASE WHEN c.udt_name LIKE '\_%' THEN substr(c.udt_name, 2) || '[]' ELSE c.udt_name END,
			       c.is_nullable = 'YES',
			       EXISTS (SELECT 1 FROM pg_constraint k
			                WHERE k.conrelid = cls.oid AND k.contype = 'p' AND a.attnum = ANY (k.conkey)),
			       EXISTS (SELECT 1 FROM pg_constraint k
			                WHERE k.conrelid = cls.oid AND k.contype = 'f' AND a.attnum = ANY (k.conkey))
			  FROM information_schema.columns c
			  JOIN pg_class cls ON cls.relname = c.table_name AND cls.relnamespace = 'public'::regnamespace
			  JOIN pg_attribute a ON a.attrelid = cls.oid AND a.attname = c.column_name
			 WHERE c.table_schema = 'public' AND c.table_name = $1
			 ORDER BY c.ordinal_position`, table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name, kind string
			var nullable, primary, foreign bool
			if err := rows.Scan(&name, &kind, &nullable, &primary, &foreign); err != nil {
				t.Fatal(err)
			}
			var keys []string
			if primary {
				keys = append(keys, "PK")
			}
			if foreign {
				keys = append(keys, "FK")
			}
			line := fmt.Sprintf("    %s %s", kind, name)
			if len(keys) > 0 {
				line += " " + strings.Join(keys, ",")
			}
			if nullable {
				line += ` "nullable"`
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("  }\n")
	}

	// One line per pair of tables that a foreign key joins.
	rows, err := pool.Query(ctx, `
		SELECT parent.relname::text, child.relname::text,
		       string_agg(DISTINCT k.conname, ', ' ORDER BY k.conname)
		  FROM pg_constraint k
		  JOIN pg_class child ON child.oid = k.conrelid
		  JOIN pg_class parent ON parent.oid = k.confrelid
		 WHERE k.contype = 'f' AND child.relnamespace = 'public'::regnamespace
		   AND child.relname = ANY ($1) AND parent.relname = ANY ($1)
		 GROUP BY parent.relname, child.relname
		 ORDER BY parent.relname, child.relname`, tables)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var parent, child, constraints string
		if err := rows.Scan(&parent, &child, &constraints); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "  %s ||--o{ %s : \"%s\"\n", parent, child, constraints)
	}
	b.WriteString("```")
	diagram := b.String()

	path := filepath.Join(testutil.RepoRoot(), "packages", "db", "README.md")
	readme, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)
	start, end := strings.Index(text, erdStart), strings.Index(text, erdEnd)
	if start < 0 || end < start {
		t.Fatalf("%s has no %s ... %s block", path, erdStart, erdEnd)
	}
	current := strings.TrimSpace(text[start+len(erdStart) : end])

	if *update {
		text = text[:start+len(erdStart)] + "\n" + diagram + "\n" + text[end:]
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if current != diagram {
		t.Errorf("the diagram in packages/db/README.md does not match the schema; rerun with -update")
	}
}
