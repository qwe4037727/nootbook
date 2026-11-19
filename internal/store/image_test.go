package store

import (
	"errors"
	"testing"
)

func TestImageAssetLifecycleAndAnchor(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	first, err := st.CreateImageAsset(ImageAsset{
		Kind: "character", EntryID: "c_hero", StyleTag: "水墨", Prompt: []byte(`{"positive":"x"}`),
		Features: []string{"黑发", "红瞳"}, CoreFeatures: []string{"红瞳"},
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if first.Status != "pending" || first.ID == "" {
		t.Fatalf("first asset = %+v", first)
	}
	ok, err := st.MarkImageRunning(first.ID)
	if err != nil || !ok {
		t.Fatalf("mark running = %v %v", ok, err)
	}
	if ok, _ := st.MarkImageRunning(first.ID); ok {
		t.Fatal("double claim should fail")
	}
	if n, err := st.RecoverRunningImageAssets(); err != nil || n != 1 {
		t.Fatalf("recover running = %d %v", n, err)
	}
	rec, err := st.GetImageAsset(first.ID)
	if err != nil || rec.Status != "pending" {
		t.Fatalf("recovered asset = %+v err=%v", rec, err)
	}
	if err := st.CompleteImageAsset(first.ID, ImageAssetResult{
		FileKey: "images/a.png", ThumbKey: "images/a_thumb.png", Seed: 7,
		ConsistencyScore: 0.9, VisualProxy: true, Width: 1024, Height: 1024, Steps: 20,
	}); err != nil {
		t.Fatalf("complete first: %v", err)
	}
	second, err := st.CreateImageAsset(ImageAsset{Kind: "character", EntryID: "c_hero"})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if err := st.CompleteImageAsset(second.ID, ImageAssetResult{FileKey: "images/b.png"}); err != nil {
		t.Fatalf("complete second: %v", err)
	}
	if _, err := st.SetImageAnchor(first.ID); err != nil {
		t.Fatalf("anchor first: %v", err)
	}
	anchor, err := st.FindImageAnchor("character", "c_hero", "")
	if err != nil || anchor.ID != first.ID {
		t.Fatalf("find anchor = %+v err=%v", anchor, err)
	}
	if _, err := st.SetImageAnchor(second.ID); err != nil {
		t.Fatalf("anchor second: %v", err)
	}
	anchor, err = st.FindImageAnchor("character", "c_hero", "")
	if err != nil || anchor.ID != second.ID || !anchor.IsAnchor {
		t.Fatalf("anchor after switch = %+v err=%v", anchor, err)
	}
	old, err := st.GetImageAsset(first.ID)
	if err != nil || old.IsAnchor {
		t.Fatalf("old anchor should be cleared: %+v err=%v", old, err)
	}

	list, err := st.ListImageAssets(ImageAssetQuery{Kind: "character", EntryID: "c_hero", Status: "done"})
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %d %v", len(list), err)
	}
	deleted, err := st.DeleteImageAsset(first.ID)
	if err != nil || deleted.FileKey != "images/a.png" {
		t.Fatalf("delete = %+v err=%v", deleted, err)
	}
	if _, err := st.GetImageAsset(first.ID); !errors.Is(err, ErrImageAssetNotFound) {
		t.Fatalf("deleted get err = %v", err)
	}
	if _, err := st.FindImageAnchor("character", "c_missing", ""); !errors.Is(err, ErrImageAssetNotFound) {
		t.Fatalf("missing anchor err = %v", err)
	}
}

func TestImageAppearanceMigration(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	asset, err := st.CreateImageAsset(ImageAsset{Kind: "character", EntryID: "c_hero"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAgentReport(AgentReportRecord{AgentKey: "image", Summary: "图像一致性偏低：0.50"}); err != nil {
		t.Fatal(err)
	}
	db, err := st.agentDatabase()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE image_asset DROP COLUMN appearance_json`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.GetImageAsset(asset.ID)
	if err != nil || loaded.Appearance != nil {
		t.Fatalf("legacy asset = %+v / %v", loaded, err)
	}
	reports, err := st.ListAgentReports("image", "", "", 10)
	if err != nil || len(reports) != 1 || reports[0].Summary != "提示词与特征一致性偏低：0.50" {
		t.Fatalf("legacy reports = %+v / %v", reports, err)
	}
}

func TestImageConfigNormalize(t *testing.T) {
	cfg := Config{Image: ImageConfig{Backend: "SDWEBUI", Width: 99, Height: 99999, Steps: 0, TimeoutSeconds: 1}}
	cfg.Normalize()
	if cfg.Image.Backend != ImageBackendSDWebUI {
		t.Fatalf("backend = %q", cfg.Image.Backend)
	}
	if cfg.Image.Width != 256 || cfg.Image.Height != 2048 || cfg.Image.Steps != 20 || cfg.Image.TimeoutSeconds != 10 {
		t.Fatalf("normalized = %+v", cfg.Image)
	}
}
