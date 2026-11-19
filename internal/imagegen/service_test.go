package imagegen

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

type stubBackend struct {
	data    []byte
	err     error
	calls   []Request
	lastRef []byte
}

func (b *stubBackend) Name() string { return "stub" }

func (b *stubBackend) Generate(ctx context.Context, req Request) (Result, error) {
	b.calls = append(b.calls, req)
	if b.err != nil {
		return Result{}, b.err
	}
	b.lastRef = req.Reference
	return Result{Data: b.data, MIME: "image/png", Seed: req.Seed}, nil
}

func testPNG(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func newTestService(t *testing.T) (*Service, *store.Store, *stubBackend) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.ImportChapters("测试书", []novel.Chapter{{Title: "第一章", Content: "山门在望，雾气缭绕。"}}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	if _, err := st.SaveKBEntry(novel.KBEntry{
		ID: novel.NewID("loc"), Kind: novel.EntryLocation, Name: "山门",
		Summary: "云雾中的石门", Fields: map[string]string{"content": "青石台阶"},
	}); err != nil {
		t.Fatalf("SaveKBEntry: %v", err)
	}
	cfg := store.DefaultConfig()
	cfg.Image = store.ImageConfig{Backend: store.ImageBackendOpenAI, BaseURL: "http://stub.local/v1", Model: "stub-model", StyleTag: "水墨风"}
	if _, err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	stub := &stubBackend{data: testPNG(t, 512)}
	svc := NewService(st, "default", nil)
	svc.SetBackendFactory(func(store.ImageConfig, string) (Backend, error) { return stub, nil })
	return svc, st, stub
}

func TestServiceGenerateAndAnchor(t *testing.T) {
	svc, st, stub := newTestService(t)
	entries, err := st.ListKBEntries(string(novel.EntryLocation))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %+v err=%v", entries, err)
	}
	entryID := entries[0].ID

	asset, err := svc.Submit(GenerateInput{Kind: "scene", EntryID: entryID})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if asset.Status != store.ImageAssetPending {
		t.Fatalf("initial status = %s", asset.Status)
	}
	worked, err := svc.RunOnce(context.Background())
	if err != nil || !worked {
		t.Fatalf("RunOnce = %v %v", worked, err)
	}
	done, err := st.GetImageAsset(asset.ID)
	if err != nil {
		t.Fatalf("GetImageAsset: %v", err)
	}
	if done.Status != store.ImageAssetDone {
		t.Fatalf("status = %s err=%s", done.Status, done.Error)
	}
	if done.FileKey == "" || done.ThumbKey == "" {
		t.Fatalf("file keys = %q %q", done.FileKey, done.ThumbKey)
	}
	for _, key := range []string{done.FileKey, done.ThumbKey} {
		if _, statErr := os.Stat(svc.FilePath(store.ImageAsset{FileKey: key})); statErr != nil {
			t.Fatalf("file %s missing: %v", key, statErr)
		}
	}
	if !done.VisualProxy || !strings.Contains(done.ConsistencyNote, "代理") {
		t.Fatalf("consistency not marked as proxy: %+v", done)
	}
	if done.StyleTag != "水墨风" || !strings.Contains(string(done.Prompt), "山门") {
		t.Fatalf("prompt/style = %s / %s", done.Prompt, done.StyleTag)
	}

	anchored, err := svc.SetAnchor(done.ID)
	if err != nil || !anchored.IsAnchor {
		t.Fatalf("SetAnchor = %+v err=%v", anchored, err)
	}

	second, err := svc.Submit(GenerateInput{Kind: "scene", EntryID: entryID})
	if err != nil {
		t.Fatalf("second Submit: %v", err)
	}
	if _, err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if len(stub.lastRef) == 0 {
		t.Fatal("anchor image was not sent as reference")
	}
	secondDone, err := st.GetImageAsset(second.ID)
	if err != nil || secondDone.Status != store.ImageAssetDone {
		t.Fatalf("second asset = %+v err=%v", secondDone, err)
	}
	if secondDone.ConsistencyScore < 0.75 {
		t.Fatalf("anchor consistency = %.2f", secondDone.ConsistencyScore)
	}

	if _, err := svc.Delete(second.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(svc.FilePath(secondDone)); !os.IsNotExist(err) {
		t.Fatalf("file still exists after delete: %v", err)
	}
}

func TestServiceFailureAndRetry(t *testing.T) {
	svc, st, stub := newTestService(t)
	entries, _ := st.ListKBEntries("")
	if len(entries) == 0 {
		t.Fatal("no entries")
	}
	asset, err := svc.Submit(GenerateInput{Kind: "scene", EntryID: entries[0].ID})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	stub.err = errors.New("backend exploded")
	if _, err := svc.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	failed, err := st.GetImageAsset(asset.ID)
	if err != nil || failed.Status != store.ImageAssetFailed || !strings.Contains(failed.Error, "exploded") {
		t.Fatalf("failed asset = %+v err=%v", failed, err)
	}
	stub.err = nil
	if err := svc.Reset(asset.ID); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("retry RunOnce: %v", err)
	}
	done, _ := st.GetImageAsset(asset.ID)
	if done.Status != store.ImageAssetDone {
		t.Fatalf("retry status = %s", done.Status)
	}
}

func TestSubmitValidation(t *testing.T) {
	svc, st, _ := newTestService(t)

	if _, err := svc.Submit(GenerateInput{Kind: "scene"}); !errors.Is(err, ErrEntryRequired) {
		t.Fatalf("missing entry err = %v", err)
	}
	if _, err := svc.Submit(GenerateInput{Kind: "spaceship"}); !errors.Is(err, ErrInvalidKind) {
		t.Fatalf("bad kind err = %v", err)
	}
	if _, err := svc.Submit(GenerateInput{Kind: "illustration"}); !errors.Is(err, ErrChapterNeeded) {
		t.Fatalf("missing chapter err = %v", err)
	}
	if _, err := svc.Submit(GenerateInput{Kind: "scene", EntryID: "loc_missing"}); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("missing entry err = %v", err)
	}
	cfg := store.DefaultConfig()
	if _, err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	entries, _ := st.ListKBEntries("")
	if _, err := svc.Submit(GenerateInput{Kind: "scene", EntryID: entries[0].ID}); !errors.Is(err, ErrImageConfigMissing) {
		t.Fatalf("empty config err = %v", err)
	}
}

func TestBuildPromptAndConsistency(t *testing.T) {
	spec := BuildPrompt(PromptInput{
		Kind:     "character",
		Entry:    novel.KBEntry{Name: "林越", Summary: "剑客", Fields: map[string]string{"appearance": "黑发，左眼有疤", "state": "受伤"}},
		StyleTag: "水墨风",
		Negative: "现代元素",
	})
	for _, want := range []string{"水墨风", "林越", "黑发", "当前状态：受伤", "半身立绘"} {
		if !strings.Contains(spec.Positive, want) {
			t.Fatalf("positive %q missing %q", spec.Positive, want)
		}
	}
	if !strings.Contains(spec.Negative, "现代元素") || !strings.Contains(spec.Negative, "低质量") {
		t.Fatalf("negative = %q", spec.Negative)
	}
	if len(spec.CoreFeatures) == 0 || spec.Shot != "半身立绘" {
		t.Fatalf("features/shot = %+v", spec)
	}

	fresh := ScoreConsistency(spec.Positive, spec, nil)
	if !fresh.VisualProxy || fresh.Score <= 0 {
		t.Fatalf("fresh consistency = %+v", fresh)
	}
	anchor := &store.ImageAsset{ID: "img_a", CoreFeatures: spec.CoreFeatures, StyleTag: "水墨风"}
	same := ScoreConsistency(spec.Positive, spec, anchor)
	if same.Score < 0.99 {
		t.Fatalf("same consistency = %+v", same)
	}
	anchor.StyleTag = "照片写实"
	anchor.CoreFeatures = []string{"蓝发"}
	mismatch := ScoreConsistency(spec.Positive, spec, anchor)
	if mismatch.Score >= same.Score {
		t.Fatalf("mismatch %.2f should be lower than %.2f", mismatch.Score, same.Score)
	}
}
