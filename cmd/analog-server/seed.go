package main

import (
	"bytes"
	"compress/zlib"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/meowkey-dev/analog/internal/config"
	"github.com/meowkey-dev/analog/internal/store"
)

// The seeded DB is not decorative: it is calibrated so that the API reproduces the
// fixtures byte-for-byte. GET /spaces/redesign/canvas returns canvas.json,
// ?include_deleted=true returns canvas.with-deleted.json, and
// GET /spaces/redesign/feedback?actor=claude-code — with no `since` — returns
// feedback.claude-code.since-12.json, because claude-code's stored cursor is seeded
// at 12. tests/contract/test_fixtures_roundtrip.py asserts exactly that.

func seedCmd() *cobra.Command {
	var dbPath, mediaDir, fixtures string
	var reset bool

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Load contracts/fixtures/ into a fresh database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dbPath == "" {
				dbPath = config.DBPath()
			}
			if mediaDir == "" {
				mediaDir = config.MediaDir()
			}
			return runSeed(dbPath, mediaDir, fixtures, reset)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", "defaults to ANALOG_DB / data/analog.db")
	cmd.Flags().StringVar(&mediaDir, "media-dir", "", "defaults to data/media")
	cmd.Flags().StringVar(&fixtures, "fixtures", filepath.Join("contracts", "fixtures"),
		"directory holding the frozen fixtures")
	cmd.Flags().BoolVar(&reset, "reset", false, "delete an existing DB first")
	return cmd
}

// jsonObject and jsonArray keep numeric literals exactly as the fixtures wrote them.
func loadFixture(dir, name string, into any) error {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(into)
}

type fixtureEvent struct {
	Seq       int64          `json:"seq"`
	TS        string         `json:"ts"`
	Type      string         `json:"type"`
	SubjectID string         `json:"subject_id"`
	Actor     string         `json:"actor"`
	ActorKind string         `json:"actor_kind"`
	Payload   map[string]any `json:"payload"`
}

func runSeed(dbPath, mediaDir, fixtures string, reset bool) error {
	if _, err := os.Stat(dbPath); err == nil {
		if !reset {
			fmt.Fprintf(os.Stderr, "%s already exists; pass --reset to replace it\n", dbPath)
			os.Exit(1)
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(dbPath + suffix)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}

	var space map[string]any
	var canvas struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}
	var annotations []map[string]any
	var eventFile struct {
		Events []fixtureEvent `json:"events"`
	}
	for _, load := range []func() error{
		func() error { return loadFixture(fixtures, "space.json", &space) },
		func() error { return loadFixture(fixtures, "canvas.with-deleted.json", &canvas) },
		func() error { return loadFixture(fixtures, "annotations.json", &annotations) },
		func() error { return loadFixture(fixtures, "events.json", &eventFile) },
	} {
		if err := load(); err != nil {
			return err
		}
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(ON)")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(store.Schema()); err != nil {
		return err
	}

	spaceID := str(space["id"])
	createdAt := str(space["created_at"])

	// updated_at is not in the fixtures; derive it from the event log.
	lastTS := func(subjectID string) string {
		out := createdAt
		for _, e := range eventFile.Events {
			if e.SubjectID == subjectID {
				out = e.TS
			}
		}
		return out
	}

	if _, err := db.Exec(
		"INSERT INTO space (id, slug, title, revision_mode, seq, created_at)"+
			" VALUES (?,?,?,?,?,?)",
		spaceID, str(space["slug"]), str(space["title"]), str(space["revision_mode"]),
		num(space["seq"]), createdAt); err != nil {
		return err
	}

	// Rows are inserted in fixture order so `ORDER BY rowid` reproduces it.
	// sp_deleted_at is a read-time projection of card.deleted_at, not stored in the
	// node blob — otherwise GET /canvas (live only) would leak it.
	for _, raw := range canvas.Nodes {
		node := map[string]any{}
		for k, v := range raw {
			if k != "sp_deleted_at" {
				node[k] = v
			}
		}
		var deletedAt any
		if v, ok := raw["sp_deleted_at"]; ok {
			deletedAt = v
		}
		rev := int64(1)
		if v, ok := node["sp_rev"]; ok {
			rev = num(v)
		}
		createdBy := "claude-code"
		if v := str(node["sp_created_by"]); v != "" {
			createdBy = v
		}
		blob, err := json.Marshal(node)
		if err != nil {
			return err
		}
		if _, err := db.Exec(
			"INSERT INTO card (id, space_id, node_json, rev, created_by, updated_at,"+
				" deleted_at) VALUES (?,?,?,?,?,?,?)",
			str(node["id"]), spaceID, string(blob), rev, createdBy,
			lastTS(str(node["id"])), deletedAt); err != nil {
			return err
		}
	}

	for _, edge := range canvas.Edges {
		blob, err := json.Marshal(edge)
		if err != nil {
			return err
		}
		createdBy := "claude-code"
		if v := str(edge["sp_created_by"]); v != "" {
			createdBy = v
		}
		if _, err := db.Exec(
			"INSERT INTO link (id, space_id, edge_json, created_by, updated_at,"+
				" deleted_at) VALUES (?,?,?,?,?,NULL)",
			str(edge["id"]), spaceID, string(blob), createdBy,
			lastTS(str(edge["id"]))); err != nil {
			return err
		}
	}

	resolvedTS := map[string]string{}
	for _, e := range eventFile.Events {
		if e.Type == "annotation.resolved" {
			resolvedTS[e.SubjectID] = e.TS
		}
	}
	for _, ann := range annotations {
		// card_title and stale are computed on read; never stored.
		var selector any
		if v, ok := ann["selector"]; ok && v != nil {
			blob, err := json.Marshal(v)
			if err != nil {
				return err
			}
			selector = string(blob)
		}
		resolved := 0
		var resolvedAt any
		if b, ok := ann["resolved"].(bool); ok && b {
			resolved = 1
			if ts, ok := resolvedTS[str(ann["id"])]; ok {
				resolvedAt = ts
			}
		}
		var reply any
		if v, ok := ann["resolved_reply"]; ok && v != nil {
			reply = v
		}
		if _, err := db.Exec(
			"INSERT INTO annotation (id, space_id, card_id, card_rev, selector, body,"+
				" motivation, creator, creator_kind, resolved, resolved_reply, resolved_at,"+
				" created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)",
			str(ann["id"]), spaceID, str(ann["card_id"]), num(ann["card_rev"]),
			selector, str(ann["body"]), str(ann["motivation"]), str(ann["creator"]),
			str(ann["creator_kind"]), resolved, reply, resolvedAt,
			str(ann["created_at"])); err != nil {
			return err
		}
	}

	for _, e := range eventFile.Events {
		var payload any
		if e.Payload != nil {
			blob, err := json.Marshal(e.Payload)
			if err != nil {
				return err
			}
			payload = string(blob)
		}
		if _, err := db.Exec(
			"INSERT INTO event (space_id, seq, ts, type, subject_id, actor, actor_kind,"+
				" payload) VALUES (?,?,?,?,?,?,?,?)",
			spaceID, e.Seq, e.TS, e.Type, e.SubjectID, e.Actor, e.ActorKind,
			payload); err != nil {
			return err
		}
	}

	// The cursor that makes the default feedback call reproduce the fixture.
	if _, err := db.Exec(
		"INSERT INTO actor_cursor (space_id, actor, seq) VALUES (?,?,?)",
		spaceID, "claude-code", 12); err != nil {
		return err
	}

	// c_shot is a file node pointing at .../media/m_01.png. Give it a real file.
	mediaTarget := filepath.Join(mediaDir, spaceID)
	if err := os.MkdirAll(mediaTarget, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(mediaTarget, "m_01.png"),
		placeholderPNG(360, 280), 0o644); err != nil {
		return err
	}

	fmt.Printf("seeded %s\n", dbPath)
	fmt.Printf("  space   redesign (%s) — 6 live cards, 1 deleted, 4 links\n", spaceID)
	fmt.Printf("  cursor  claude-code @ 12\n")
	fmt.Printf("  media   %s\n", filepath.Join(mediaTarget, "m_01.png"))
	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) int64 {
	if n, ok := v.(json.Number); ok {
		i, _ := n.Int64()
		return i
	}
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return 0
}

// placeholderPNG is a valid greyscale PNG with diagonal stripes, so it is obviously
// a placeholder and not a broken image.
func placeholderPNG(width, height int) []byte {
	chunk := func(tag string, data []byte) []byte {
		var out bytes.Buffer
		_ = binary.Write(&out, binary.BigEndian, uint32(len(data)))
		body := append([]byte(tag), data...)
		out.Write(body)
		_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(body))
		return out.Bytes()
	}

	raw := make([]byte, 0, height*(width+1))
	for y := 0; y < height; y++ {
		raw = append(raw, 0) // filter: none
		for x := 0; x < width; x++ {
			if ((x+y)/12)%2 != 0 {
				raw = append(raw, 0xE8)
			} else {
				raw = append(raw, 0xC8)
			}
		}
	}
	var compressed bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&compressed, zlib.BestCompression)
	_, _ = zw.Write(raw)
	_ = zw.Close()

	var ihdr bytes.Buffer
	_ = binary.Write(&ihdr, binary.BigEndian, uint32(width))
	_ = binary.Write(&ihdr, binary.BigEndian, uint32(height))
	ihdr.Write([]byte{8, 0, 0, 0, 0}) // 8-bit greyscale, no interlace

	var out bytes.Buffer
	out.Write([]byte("\x89PNG\r\n\x1a\n"))
	out.Write(chunk("IHDR", ihdr.Bytes()))
	out.Write(chunk("IDAT", compressed.Bytes()))
	out.Write(chunk("IEND", nil))
	return out.Bytes()
}
