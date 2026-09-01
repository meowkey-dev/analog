// SPEC §2.1 file nodes and §3 POST /media.
//
// Note: openapi.json documents the upload but not the GET that serves the bytes
// back, even though contracts/fixtures/canvas.json contains a file node pointing
// at one. See AMENDMENTS.md #1.
package conformance

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
)

// tinyPNG builds a real 1×1 png the way the python harness did: a signature, three
// chunks, correct CRCs. No fixture file on disk.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	chunk := func(tag string, data []byte) []byte {
		buf := &bytes.Buffer{}
		_ = binary.Write(buf, binary.BigEndian, uint32(len(data)))
		buf.WriteString(tag)
		buf.Write(data)
		_ = binary.Write(buf, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(tag), data...)))
		return buf.Bytes()
	}
	ihdr := &bytes.Buffer{}
	for _, v := range []any{uint32(1), uint32(1), uint8(8), uint8(0), uint8(0), uint8(0), uint8(0)} {
		_ = binary.Write(ihdr, binary.BigEndian, v)
	}
	idat := &bytes.Buffer{}
	w, err := zlib.NewWriterLevel(idat, 9)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("\x00\x00"))
	_ = w.Close()

	out := &bytes.Buffer{}
	out.WriteString("\x89PNG\r\n\x1a\n")
	out.Write(chunk("IHDR", ihdr.Bytes()))
	out.Write(chunk("IDAT", idat.Bytes()))
	out.Write(chunk("IEND", nil))
	return out.Bytes()
}

func mediaSpace(t *testing.T) *server {
	t.Helper()
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	return s
}

// upload posts one multipart file to the demo space as an agent and returns the
// raw response.
func upload(t *testing.T, s *server, name, contentType string, data []byte) *response {
	t.Helper()
	return uploadTo(t, s, "/api/spaces/demo/media", name, contentType, data, agentP(), nil)
}

func uploadTo(t *testing.T, s *server, path, name, contentType string, data []byte,
	actor url.Values, headers map[string]string) *response {
	t.Helper()
	if data == nil {
		data = tinyPNG(t)
	}
	buf := &bytes.Buffer{}
	form := multipart.NewWriter(buf)
	// httpx's file tuple carries an explicit content type per part;
	// CreateFormFile would hardcode application/octet-stream.
	part, err := form.CreatePart(textproto.MIMEHeader{
		"Content-Type":        {contentType},
		"Content-Disposition": {fmt.Sprintf(`form-data; name="file"; filename="%s"`, name)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	return s.do(t, "POST", path, actor, headers, form.FormDataContentType(), buf.Bytes())
}

func TestMedia_UploadReturnsAUrlAndMetadata(t *testing.T) {
	s := mediaSpace(t)
	r := upload(t, s, "shot.png", "image/png", nil)
	if r.status != 201 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	body := r.obj()
	if asStr(body["content_type"]) != "image/png" {
		t.Errorf("content_type = %v", body["content_type"])
	}
	if got := num(t, body["bytes"]); got != float64(len(tinyPNG(t))) {
		t.Errorf("bytes = %v, want %d", got, len(tinyPNG(t)))
	}
	if !hasPrefix(asStr(body["url"]), "/api/spaces/demo/media/") {
		t.Errorf("url = %v", body["url"])
	}
	if !strings.HasSuffix(asStr(body["url"]), ".png") {
		t.Errorf("url = %v", body["url"])
	}
}

func TestMedia_TheReturnedUrlServesTheBytes(t *testing.T) {
	s := mediaSpace(t)
	link := asStr(upload(t, s, "shot.png", "image/png", nil).obj()["url"])
	r := s.get(t, link, nil)
	if r.status != 200 {
		t.Fatalf("%d", r.status)
	}
	if string(r.raw) != string(tinyPNG(t)) {
		t.Error("served bytes differ from the upload")
	}
	if ct := r.header.Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Errorf("content-type = %q", ct)
	}
}

func TestMedia_TheUrlDropsIntoAFileNode(t *testing.T) {
	// SPEC §2.1: binary content is a JSON Canvas file node, so it survives export.
	s := mediaSpace(t)
	link := asStr(upload(t, s, "shot.png", "image/png", nil).obj()["url"])
	r := s.post(t, "/api/spaces/demo/cards", agentP(), map[string]any{"nodes": []any{
		map[string]any{"id": "ignored", "type": "file", "x": 0, "y": 0,
			"width": 360, "height": 280, "file": link, "sp_title": "Current UI"}}})
	if r.status != 201 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	node := r.arr()[0].(map[string]any)
	if asStr(node["type"]) != "file" || asStr(node["file"]) != link {
		t.Errorf("node = %s", canonical(node))
	}
	if _, has := node["sp_kind"]; has {
		t.Error("sp_kind is meaningful only on text nodes")
	}
	if r := s.get(t, asStr(node["file"]), nil); r.status != 200 {
		t.Errorf("media GET: %d", r.status)
	}
}

func TestMedia_MediaIsScopedToItsSpace(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	makeSpace(t, s, "other", "Other", "")
	link := asStr(upload(t, s, "shot.png", "image/png", nil).obj()["url"])
	if r := s.get(t, strings.Replace(link, "/demo/", "/other/", 1), nil); r.status != 404 {
		t.Errorf("cross-space media GET: %d, want 404", r.status)
	}
}

func TestMedia_UnknownMediaIs404(t *testing.T) {
	s := mediaSpace(t)
	if r := s.get(t, "/api/spaces/demo/media/m_nope.png", nil); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

func TestMedia_UploadingToAnUnknownSpaceIs404(t *testing.T) {
	s := startServer(t)
	if r := uploadTo(t, s, "/api/spaces/nope/media", "a.png", "image/png",
		tinyPNG(t), agentP(), nil); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

func TestMedia_UploadEmitsNoEvent(t *testing.T) {
	// There is no media.* event type; the card that references it is the event.
	s := mediaSpace(t)
	before := eventsOf(t, s, "demo", "0")
	upload(t, s, "shot.png", "image/png", nil)
	if got := eventsOf(t, s, "demo", "0"); len(got) != len(before) {
		t.Errorf("upload emitted %d events", len(got)-len(before))
	}
}

func TestMedia_SupportedTypes(t *testing.T) {
	for _, tc := range []struct {
		contentType, suffix string
	}{
		{"image/png", ".png"}, {"image/jpeg", ".jpg"}, {"image/gif", ".gif"},
		{"image/webp", ".webp"}, {"image/svg+xml", ".svg"}, {"application/pdf", ".pdf"},
	} {
		t.Run(tc.contentType, func(t *testing.T) {
			s := mediaSpace(t)
			r := upload(t, s, "f"+tc.suffix, tc.contentType, []byte("data"))
			if r.status != 201 {
				t.Fatalf("%d %s", r.status, r.str())
			}
			if !strings.HasSuffix(asStr(r.obj()["url"]), tc.suffix) {
				t.Errorf("url = %v, want suffix %q", r.obj()["url"], tc.suffix)
			}
		})
	}
}

func TestMedia_AnUnsupportedTypeIsRejected(t *testing.T) {
	s := mediaSpace(t)
	r := upload(t, s, "x.exe", "application/x-msdownload", []byte("MZ"))
	if r.status != 400 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if asStr(r.obj()["error"]) != "unsupported_kind" {
		t.Errorf("error = %v", r.obj()["error"])
	}
}

func TestMedia_ATraversalFilenameCannotEscapeTheMediaDirectory(t *testing.T) {
	// The stored name is server-assigned; the client's filename is advisory only.
	s := mediaSpace(t)
	link := asStr(upload(t, s, "../../../etc/passwd.png", "image/png", nil).obj()["url"])
	if strings.Contains(link, "..") {
		t.Errorf("url = %q", link)
	}
	if r := s.get(t, link, nil); r.status != 200 {
		t.Errorf("media GET: %d", r.status)
	}
}
