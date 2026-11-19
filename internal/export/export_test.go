package export

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func exportPNG(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.Set(x, y, color.RGBA{R: 60, G: uint8(x % 255), B: uint8(y % 255), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func newExportFixture(t *testing.T) (*Service, *store.Store, novel.Project) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project, err := st.ImportChapters("导出书", []novel.Chapter{
		{Title: "第一章 启程", Content: "林越背剑出城。\n山门在望。"},
		{Title: "第二章 入山", Content: "山雾渐浓。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	for _, entry := range []novel.KBEntry{
		{ID: novel.NewID("c"), Kind: novel.EntryCharacter, Name: "林越", Summary: "剑客"},
		{ID: novel.NewID("loc"), Kind: novel.EntryLocation, Name: "山门", Summary: "石门"},
	} {
		if _, err := st.SaveKBEntry(entry); err != nil {
			t.Fatalf("SaveKBEntry: %v", err)
		}
	}
	return NewService(st, nil), st, project
}

func seedImage(t *testing.T, st *store.Store, kind, entryID, chapterID string, anchor bool) store.ImageAsset {
	t.Helper()
	asset, err := st.CreateImageAsset(store.ImageAsset{
		Kind: kind, EntryID: entryID, ChapterID: chapterID, Prompt: json.RawMessage(`{"positive":"x"}`),
	})
	if err != nil {
		t.Fatalf("CreateImageAsset: %v", err)
	}
	dir := filepath.Join(st.Dir(), "images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir images: %v", err)
	}
	key := "images/" + asset.ID + ".png"
	if err := os.WriteFile(filepath.Join(st.Dir(), filepath.FromSlash(key)), exportPNG(t, 80), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := st.CompleteImageAsset(asset.ID, store.ImageAssetResult{FileKey: key}); err != nil {
		t.Fatalf("CompleteImageAsset: %v", err)
	}
	if anchor {
		if _, err := st.SetImageAnchor(asset.ID); err != nil {
			t.Fatalf("SetImageAnchor: %v", err)
		}
	}
	saved, err := st.GetImageAsset(asset.ID)
	if err != nil {
		t.Fatalf("GetImageAsset: %v", err)
	}
	return saved
}

func TestExportTextAndMarkdown(t *testing.T) {
	svc, st, project := newExportFixture(t)
	chapterID := project.Chapters[0].ID
	entry, _ := st.ListKBEntries(string(novel.EntryCharacter))
	illustration := seedImage(t, st, "illustration", "", chapterID, true)
	_ = seedImage(t, st, "character", entry[0].ID, "", false)

	record, err := svc.Create(Request{Format: FormatTXT})
	if err != nil {
		t.Fatalf("Create txt: %v", err)
	}
	if record.Format != FormatTXT || record.ChapterCount != 2 || record.SizeBytes == 0 || strings.Contains(record.FileName, "/") {
		t.Fatalf("txt record = %+v", record)
	}
	path, _ := svc.FilePath(record)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read txt: %v", err)
	}
	text := string(data)
	for _, want := range []string{"导出书", "第一章 启程", "林越背剑出城。", "第二章 入山"} {
		if !strings.Contains(text, want) {
			t.Fatalf("txt missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "插图") {
		t.Fatalf("plain txt should not reference images:\n%s", text)
	}

	record, err = svc.Create(Request{Format: FormatMD, IncludeImages: true})
	if err != nil {
		t.Fatalf("Create md zip: %v", err)
	}
	if !record.IncludeImages || !strings.HasSuffix(record.FileName, ".zip") || record.ImageCount == 0 {
		t.Fatalf("md zip record = %+v", record)
	}
	path, _ = svc.FilePath(record)
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read md zip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	var doc, imageName string
	var imageBytes int
	for _, file := range zr.File {
		if strings.HasSuffix(file.Name, ".md") {
			reader, _ := file.Open()
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(reader)
			reader.Close()
			doc = buf.String()
		}
		if strings.HasPrefix(file.Name, "images/") {
			imageName = file.Name
			imageBytes = int(file.UncompressedSize64)
		}
	}
	if doc == "" || imageBytes == 0 || !strings.Contains(doc, "images/") {
		t.Fatalf("md zip content doc=%q image=%q bytes=%d", doc, imageName, imageBytes)
	}
	if !strings.Contains(doc, "images/"+illustration.ID) {
		t.Fatalf("md zip missing illustration ref:\n%s", doc)
	}

	records, err := svc.List()
	if err != nil || len(records) != 2 {
		t.Fatalf("List = %+v err=%v", records, err)
	}
	if _, err := svc.Get("exp_missing"); err != ErrExportNotFound {
		t.Fatalf("Get missing err = %v", err)
	}
}

func TestExportEPUBStructure(t *testing.T) {
	svc, st, project := newExportFixture(t)
	chapterID := project.Chapters[0].ID
	charEntries, _ := st.ListKBEntries(string(novel.EntryCharacter))
	illustration := seedImage(t, st, "illustration", "", chapterID, true)
	cover := seedImage(t, st, "cover", "", "", true)
	character := seedImage(t, st, "character", charEntries[0].ID, "", true)

	record, err := svc.Create(Request{Format: FormatEPUB})
	if err != nil {
		t.Fatalf("Create epub: %v", err)
	}
	if record.Format != FormatEPUB || record.ImageCount != 3 {
		t.Fatalf("epub record = %+v", record)
	}
	path, _ := svc.FilePath(record)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read epub: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open epub: %v", err)
	}
	if len(zr.File) == 0 || zr.File[0].Name != "mimetype" || zr.File[0].Method != zip.Store {
		t.Fatalf("mimetype must be first and stored: %+v", zr.File[0])
	}
	entries := map[string]*zip.File{}
	for _, file := range zr.File {
		entries[file.Name] = file
	}
	read := func(name string) string {
		file, ok := entries[name]
		if !ok {
			t.Fatalf("epub missing %s", name)
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer reader.Close()
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(reader)
		return buf.String()
	}
	if read("mimetype") != "application/epub+zip" {
		t.Fatal("bad mimetype content")
	}
	if !strings.Contains(read("META-INF/container.xml"), "OEBPS/content.opf") {
		t.Fatal("container.xml missing opf path")
	}
	opf := read("OEBPS/content.opf")
	for _, want := range []string{`id="chapter-1"`, `id="chapter-2"`, `properties="nav"`, `properties="cover-image"`, "导出书"} {
		if !strings.Contains(opf, want) {
			t.Fatalf("opf missing %q:\n%s", want, opf)
		}
	}
	chapter := read("OEBPS/chapter-1.xhtml")
	if !strings.Contains(chapter, "林越背剑出城") || !strings.Contains(chapter, "images/"+illustration.ID) {
		t.Fatalf("chapter xhtml = %s", chapter)
	}
	gallery := read("OEBPS/gallery.xhtml")
	if !strings.Contains(gallery, "images/"+character.ID) || !strings.Contains(gallery, "林越") {
		t.Fatalf("gallery = %s", gallery)
	}
	if !strings.Contains(read("OEBPS/cover.xhtml"), "images/"+cover.ID) {
		t.Fatal("cover.xhtml missing cover image")
	}
	if !strings.Contains(read("OEBPS/nav.xhtml"), "第一章 启程") {
		t.Fatal("nav missing chapter")
	}
}
