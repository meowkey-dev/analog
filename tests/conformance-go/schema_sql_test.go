// schema.sql is frozen; these tests pin what an implementation may rely on.
//
// They need a real sqlite to execute the DDL, which is why this module's single
// dependency is modernc.org/sqlite — the same pure-go driver the server uses.
package conformance

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

var expectedColumns = map[string][]string{
	"space":        {"id", "slug", "title", "revision_mode", "seq", "created_at"},
	"card":         {"id", "space_id", "node_json", "rev", "created_by", "updated_at", "deleted_at"},
	"link":         {"id", "space_id", "edge_json", "created_by", "updated_at", "deleted_at"},
	"annotation":   {"id", "space_id", "card_id", "card_rev", "selector", "body", "motivation", "creator", "creator_kind", "resolved", "resolved_reply", "resolved_at", "created_at"},
	"event":        {"space_id", "seq", "ts", "type", "subject_id", "actor", "actor_kind", "payload"},
	"actor_cursor": {"space_id", "actor", "seq"},
}

// schemaSQL reads schema.sql as a file rather than as an import.
//
// Frozen bytes; the path moved once already (analog/server/ -> internal/store/),
// so this looks it up rather than importing a constant from an implementation.
func schemaSQL(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(repoRoot, "internal", "store", "schema.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no schema.sql at %s", path)
	}
	return raw
}

func openSchemaDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(string(schemaSQL(t))); err != nil {
		t.Fatalf("executescript failed: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	return db
}

func TestSchema_SchemaAppliesCleanly(t *testing.T) {
	db := openSchemaDB(t)
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got[name] = true
	}
	if len(got) != len(expectedColumns) {
		t.Fatalf("tables = %v, want exactly the six contract tables", got)
	}
	for name := range expectedColumns {
		if !got[name] {
			t.Errorf("table %s missing", name)
		}
	}
}

func TestSchema_TableColumns(t *testing.T) {
	for table, want := range expectedColumns {
		t.Run(table, func(t *testing.T) {
			db := openSchemaDB(t)
			rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			got := map[string]bool{}
			for rows.Next() {
				var cid int
				var name, typ string
				var notNull, pk int
				var dflt sql.NullString
				if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
					t.Fatal(err)
				}
				got[name] = true
			}
			if len(got) != len(want) {
				t.Fatalf("columns = %v, want %v", got, want)
			}
			for _, name := range want {
				if !got[name] {
					t.Errorf("column %s missing", name)
				}
			}
		})
	}
}

// insertSpace mirrors the python helper: sid, slug and mode are parameters.
func insertSpace(db *sql.DB, sid, slug, mode string) error {
	_, err := db.Exec("INSERT INTO space (id,slug,title,revision_mode,seq,created_at)"+
		" VALUES (?,?,?,?,0,'2026-01-01T00:00:00Z')", sid, slug, "T", mode)
	return err
}

func TestSchema_SlugIsUnique(t *testing.T) {
	db := openSchemaDB(t)
	if err := insertSpace(db, "s_1", "demo", "replace"); err != nil {
		t.Fatal(err)
	}
	if err := insertSpace(db, "s_2", "demo", "replace"); err == nil {
		t.Fatal("duplicate slug was accepted")
	}
}

func TestSchema_RevisionModesAccepted(t *testing.T) {
	for _, mode := range []string{"replace", "branch"} {
		t.Run(mode, func(t *testing.T) {
			db := openSchemaDB(t)
			if err := insertSpace(db, "s_"+mode, mode, mode); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSchema_RevisionModeCheckRejectsOthers(t *testing.T) {
	db := openSchemaDB(t)
	if err := insertSpace(db, "s_1", "demo", "merge"); err == nil {
		t.Fatal("revision_mode merge was accepted")
	}
}

func TestSchema_EventTypeCheck(t *testing.T) {
	db := openSchemaDB(t)
	if err := insertSpace(db, "s_1", "demo", "replace"); err != nil {
		t.Fatal(err)
	}
	for i, eventType := range []string{
		"card.created", "card.updated", "card.moved", "card.deleted",
		"link.created", "link.deleted", "annotation.created", "annotation.resolved",
	} {
		_, err := db.Exec("INSERT INTO event (space_id,seq,ts,type,subject_id,actor,actor_kind)"+
			" VALUES (?,?,?,?,?,?,?)", "s_1", i+1, "t", eventType, "x", "human", "human")
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := db.Exec("INSERT INTO event (space_id,seq,ts,type,subject_id,actor,actor_kind)"+
		" VALUES (?,999,'t','card.renamed','x','human','human')", "s_1")
	if err == nil {
		t.Fatal("event type card.renamed was accepted")
	}
}

func TestSchema_EventSeqIsUniquePerSpace(t *testing.T) {
	db := openSchemaDB(t)
	if err := insertSpace(db, "s_1", "demo", "replace"); err != nil {
		t.Fatal(err)
	}
	if err := insertSpace(db, "s_2", "two", "replace"); err != nil {
		t.Fatal(err)
	}
	for _, sid := range []string{"s_1", "s_2"} {
		_, err := db.Exec("INSERT INTO event (space_id,seq,ts,type,subject_id,actor,actor_kind)"+
			" VALUES (?,1,'t','card.created','c','human','human')", sid)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := db.Exec("INSERT INTO event (space_id,seq,ts,type,subject_id,actor,actor_kind)"+
		" VALUES (?,1,'t','card.created','c','human','human')", "s_1")
	if err == nil {
		t.Fatal("duplicate seq in one space was accepted")
	}
}

func TestSchema_ActorKindAndMotivationChecks(t *testing.T) {
	db := openSchemaDB(t)
	if err := insertSpace(db, "s_1", "demo", "replace"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO card (id,space_id,node_json,rev,created_by,updated_at)"+
		" VALUES ('c_1',?,'{}',1,'human','t')", "s_1"); err != nil {
		t.Fatal(err)
	}
	const ok = "INSERT INTO annotation (id,space_id,card_id,card_rev,body,motivation," +
		"creator,creator_kind,created_at) VALUES (?,?, 'c_1',1,'b',?,'human',?,'t')"
	for _, motivation := range []string{"commenting", "assessing", "editing"} {
		if _, err := db.Exec(ok, "a_"+motivation, "s_1", motivation, "human"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ok, "a_bad", "s_1", "praising", "human"); err == nil {
		t.Fatal("motivation praising was accepted")
	}
	if _, err := db.Exec(ok, "a_bad2", "s_1", "editing", "robot"); err == nil {
		t.Fatal("creator_kind robot was accepted")
	}
}

func TestSchema_DeletingASpaceCascades(t *testing.T) {
	db := openSchemaDB(t)
	if err := insertSpace(db, "s_1", "demo", "replace"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO card (id,space_id,node_json,rev,created_by,updated_at)"+
		" VALUES ('c_1',?,'{}',1,'human','t')", "s_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO actor_cursor (space_id,actor,seq) VALUES (?,'a',3)", "s_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM space WHERE id = ?", "s_1"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"card", "actor_cursor"} {
		var count int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("%s rows survived the space deletion", table)
		}
	}
}

func TestSchema_SoftDeleteColumnsDefaultNull(t *testing.T) {
	db := openSchemaDB(t)
	if err := insertSpace(db, "s_1", "demo", "replace"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO card (id,space_id,node_json,rev,created_by,updated_at)"+
		" VALUES ('c_1',?,'{}',1,'human','t')", "s_1"); err != nil {
		t.Fatal(err)
	}
	var deletedAt sql.NullString
	if err := db.QueryRow("SELECT deleted_at FROM card").Scan(&deletedAt); err != nil {
		t.Fatal(err)
	}
	if deletedAt.Valid {
		t.Errorf("deleted_at defaults to %q, want null", deletedAt.String)
	}
}
