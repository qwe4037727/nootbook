package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"novelagent/internal/imagegen"
	"novelagent/internal/milestone"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

type m6StubBackend struct{ data []byte }

func (b *m6StubBackend) Name() string { return "stub" }

func (b *m6StubBackend) Generate(ctx context.Context, req imagegen.Request) (imagegen.Result, error) {
	return imagegen.Result{Data: b.data, MIME: "image/png", Seed: req.Seed}, nil
}

func m6PNG(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.Set(x, y, color.RGBA{R: 40, G: uint8(x % 255), B: uint8(y % 255), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func newM6Handler(t *testing.T, cfg milestone.Config) (http.Handler, *store.Store, *imagegen.Service, string) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.ImportChapters("图像书", []novel.Chapter{{Title: "第一章", Content: "山门在望。"}}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	entry, err := st.SaveKBEntry(novel.KBEntry{
		ID: novel.NewID("loc"), Kind: novel.EntryLocation, Name: "山门",
		Summary: "云雾石门", Fields: map[string]string{"content": "青石台阶"},
	})
	if err != nil {
		t.Fatalf("SaveKBEntry: %v", err)
	}
	modelCfg := store.DefaultConfig()
	modelCfg.Image = store.ImageConfig{Backend: store.ImageBackendOpenAI, BaseURL: "http://stub.local/v1", Model: "stub-model", StyleTag: "水墨风"}
	if _, err := st.SaveConfig(modelCfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	var svc *imagegen.Service
	if cfg.M6Enabled {
		svc = imagegen.NewService(st, "default", nil)
		svc.SetBackendFactory(func(store.ImageConfig, string) (imagegen.Backend, error) {
			return &m6StubBackend{data: m6PNG(t, 400)}, nil
		})
	}
	static := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	h := NewWithServices(st, static, nil, &fakeAnalyzer{}, nil, cfg, Services{Images: svc})
	return h, st, svc, entry.ID
}

func TestImageRoutesLifecycle(t *testing.T) {
	h, _, svc, entryID := newM6Handler(t, milestone.Config{M6Enabled: true})

	rec := serveJSON(t, h, http.MethodGet, "/api/features", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("features = %d: %s", rec.Code, rec.Body.String())
	}
	var features map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &features); err != nil || features["images"] != true || features["m6"] != true {
		t.Fatalf("features = %s err=%v", rec.Body.String(), err)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/images/preview-prompt", map[string]any{"kind": "scene", "entryId": entryID})
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", rec.Code, rec.Body.String())
	}
	var spec imagegen.PromptSpec
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	if spec.Positive == "" || spec.StyleTag != "水墨风" || spec.Negative == "" {
		t.Fatalf("spec = %+v", spec)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/images/generate", map[string]any{"kind": "scene", "entryId": entryID})
	if rec.Code != http.StatusOK {
		t.Fatalf("generate = %d: %s", rec.Code, rec.Body.String())
	}
	var asset store.ImageAsset
	if err := json.Unmarshal(rec.Body.Bytes(), &asset); err != nil || asset.ID == "" || asset.Status != store.ImageAssetPending {
		t.Fatalf("asset = %s err=%v", rec.Body.String(), err)
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/images/assets?kind=scene", nil)
	var list struct {
		Assets  []store.ImageAsset `json:"assets"`
		Pending int                `json:"pending"`
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Assets) != 1 || list.Pending != 1 {
		t.Fatalf("list = %s err=%v", rec.Body.String(), err)
	}

	if _, err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	rec = serveJSON(t, h, http.MethodGet, "/api/images/"+asset.ID, nil)
	var done store.ImageAsset
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &done) != nil || done.Status != store.ImageAssetDone {
		t.Fatalf("get asset = %d: %s", rec.Code, rec.Body.String())
	}
	if done.VisualProxy != true || done.ConsistencyNote == "" {
		t.Fatalf("consistency = %+v", done)
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/images/"+asset.ID+"/file", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Body.Len() == 0 {
		t.Fatalf("file = %d %s %d", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	rec = serveJSON(t, h, http.MethodGet, "/api/images/"+asset.ID+"/thumb", nil)
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("thumb = %d: %s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/images/"+asset.ID+"/anchor", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("anchor = %d: %s", rec.Code, rec.Body.String())
	}
	var anchored store.ImageAsset
	if err := json.Unmarshal(rec.Body.Bytes(), &anchored); err != nil || !anchored.IsAnchor {
		t.Fatalf("anchor = %s err=%v", rec.Body.String(), err)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/images/"+asset.ID+"/retry", nil)
	var retried store.ImageAsset
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &retried) != nil || retried.Status != store.ImageAssetPending {
		t.Fatalf("retry = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("retry RunOnce: %v", err)
	}

	rec = serveJSON(t, h, http.MethodDelete, "/api/images/"+asset.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := serveJSON(t, h, http.MethodGet, "/api/images/"+asset.ID, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted = %d", rec.Code)
	}
}

func TestCharacterAppearanceAutoAudit(t *testing.T) {
	h, st, svc, _ := newM6Handler(t, milestone.Config{M6Enabled: true})
	entry, err := st.CreateKnowledgeEntry(novel.KBEntry{ID: novel.NewID("c"), Kind: novel.EntryCharacter, Name: "阿遥"})
	if err != nil {
		t.Fatal(err)
	}
	verdicts := make(chan string, 4)
	for _, verdict := range []string{"drift", "consistent", "uncertain", "failed"} {
		verdicts <- verdict
	}
	close(verdicts)
	vision := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Model != "vision-stub" || r.URL.Path != "/v1/chat/completions" {
			t.Error("invalid vision request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var images int
		for _, message := range payload.Messages {
			images += strings.Count(string(message.Content), `"type":"image_url"`)
		}
		if images != 2 {
			t.Errorf("vision image count = %d", images)
		}
		verdict := <-verdicts
		if verdict == "" || verdict == "failed" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		content, _ := json.Marshal(map[string]any{"verdict": verdict, "evidence": []string{"左眉疤痕与面部轮廓对照"}})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(content)}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 10, "total_tokens": 60}})
	}))
	defer vision.Close()
	cfg, err := st.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.BaseURL, cfg.Model = vision.URL+"/v1", "text-stub"
	if _, err := st.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	gen := func(status, reason, anchorID string) store.ImageAsset {
		t.Helper()
		asset, err := svc.Submit(imagegen.GenerateInput{Kind: "character", EntryID: entry.ID})
		if err != nil {
			t.Fatal(err)
		}
		if worked, err := svc.RunOnce(context.Background()); !worked || err != nil {
			t.Fatalf("RunOnce = %v, %v", worked, err)
		}
		var result store.ImageAsset
		rec := serveJSON(t, h, http.MethodGet, "/api/images/"+asset.ID, nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.Status != "done" || result.Appearance == nil {
			t.Fatalf("generation response = %d %s, %v", rec.Code, rec.Body, err)
		}
		if a := result.Appearance; a.Status != status || a.Reason != reason || a.AnchorID != anchorID {
			t.Fatalf("appearance = %+v; want %s / %s / %s", a, status, reason, anchorID)
		}
		return result
	}
	gen("unavailable", "disabled", "")
	rec := serveJSON(t, h, http.MethodPut, "/api/config/image-model", map[string]any{
		"backend": "openai", "baseUrl": cfg.Image.BaseURL, "model": cfg.Image.Model,
		"appearanceEnabled": true, "appearanceModel": "vision-stub",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"appearanceEnabled":true`) {
		t.Fatalf("image config = %d %s", rec.Code, rec.Body)
	}
	anchor := gen("unavailable", "no_anchor", "")
	if _, err := svc.SetAnchor(anchor.ID); err != nil {
		t.Fatal(err)
	}
	drift := gen("drift", "", anchor.ID)
	gen("consistent", "", anchor.ID)
	gen("uncertain", "", anchor.ID)
	unavailable := gen("unavailable", "model_error", anchor.ID)
	if rec := serveJSON(t, h, http.MethodGet, "/api/images/"+unavailable.ID+"/file", nil); rec.Code != http.StatusOK {
		t.Fatalf("image after audit failure = %d", rec.Code)
	}
	reports, err := st.ListAgentReports("image", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var driftReports int
	for _, report := range reports {
		if strings.Contains(string(report.Findings), "image_appearance_drift") {
			driftReports++
			if report.EventID != drift.ID || !strings.Contains(string(report.Findings), anchor.ID) || !strings.Contains(string(report.Findings), "左眉疤痕") {
				t.Fatalf("drift report = %+v", report)
			}
		}
	}
	if driftReports != 1 {
		t.Fatalf("drift reports = %d", driftReports)
	}
}

func TestImageRoutesCharacterSimilarity(t *testing.T) {
	h, st, svc, _ := newM6Handler(t, milestone.Config{M6Enabled: true})
	entry, err := st.SaveKBEntry(novel.KBEntry{ID: novel.NewID("c"), Kind: novel.EntryCharacter, Name: "林越"})
	if err != nil {
		t.Fatal(err)
	}
	generate := func(data []byte) store.ImageAsset {
		t.Helper()
		svc.SetBackendFactory(func(store.ImageConfig, string) (imagegen.Backend, error) {
			return &m6StubBackend{data: data}, nil
		})
		rec := serveJSON(t, h, http.MethodPost, "/api/images/generate", map[string]any{"kind": "character", "entryId": entry.ID})
		var asset store.ImageAsset
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &asset) != nil || asset.ID == "" {
			t.Fatalf("generate = %d: %s", rec.Code, rec.Body.String())
		}
		if worked, err := svc.RunOnce(context.Background()); err != nil || !worked {
			t.Fatalf("RunOnce = %v, %v", worked, err)
		}
		asset, err := st.GetImageAsset(asset.ID)
		if err != nil || asset.Status != store.ImageAssetDone {
			t.Fatalf("get saved asset = %+v, %v", asset, err)
		}
		return asset
	}
	pattern := func(side int, swap bool) []byte {
		t.Helper()
		img := image.NewGray(image.Rect(0, 0, side, side))
		for y := 0; y < side; y++ {
			for x := 0; x < side; x++ {
				if (x < side/2) != swap {
					img.SetGray(x, y, color.Gray{Y: 255})
				}
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	first := generate(pattern(64, false))
	similar := generate(pattern(128, false))
	different := generate(pattern(64, true)) // Same histogram, reversed layout.
	if first.ConsistencyScore != different.ConsistencyScore {
		t.Fatalf("fixtures should have equal prompt scores: %v and %v", first.ConsistencyScore, different.ConsistencyScore)
	}
	endpoint := "/api/images/" + first.ID + "/similarity?otherId="
	compare := func(id string) imagegen.PixelSimilarity {
		t.Helper()
		rec := serveJSON(t, h, http.MethodGet, endpoint+id, nil)
		var result imagegen.PixelSimilarity
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.AssetID != first.ID || result.OtherID != id || result.Method == "" || result.Note == "" {
			t.Fatalf("compare = %d: %s", rec.Code, rec.Body.String())
		}
		return result
	}
	if got := compare(similar.ID); !got.Available || got.Score == nil || *got.Score < 0.99 || *got.Score > 1 {
		t.Fatalf("matching layout at different resolutions = %+v", got)
	}
	if got := compare(different.ID); !got.Available || got.Score == nil || *got.Score != 0 {
		t.Fatalf("opposite layout should produce a real zero = %+v", got)
	}

	path := filepath.Join(st.Dir(), filepath.FromSlash(different.FileKey))
	oversized := image.NewGray(image.Rect(0, 0, 4097, 1))
	var oversizedPNG bytes.Buffer
	if err := png.Encode(&oversizedPNG, oversized); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		data   []byte
		reason string
	}{
		{oversizedPNG.Bytes(), "image_too_large"},
		{[]byte("corrupt image"), "unsupported_or_invalid_image"},
	} {
		if err := os.WriteFile(path, tc.data, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := compare(different.ID); got.Available || got.Score != nil || got.Reason != tc.reason {
			t.Fatalf("unavailable comparison (%s) = %+v", tc.reason, got)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := compare(different.ID); got.Available || got.Score != nil || got.Reason != "file_unavailable" {
		t.Fatalf("missing original = %+v", got)
	}
	if err := st.ResetImageAsset(different.ID); err != nil {
		t.Fatal(err)
	}
	if got := compare(different.ID); got.Available || got.Score != nil || got.Reason != "asset_not_done" {
		t.Fatalf("pending image = %+v", got)
	}

	other, err := st.CreateImageAsset(store.ImageAsset{Kind: "character", EntryID: "other"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", first.ID, other.ID} {
		if rec := serveJSON(t, h, http.MethodGet, endpoint+id, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("invalid pair %q = %d: %s", id, rec.Code, rec.Body.String())
		}
	}
	if rec := serveJSON(t, h, http.MethodGet, endpoint+"missing", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown asset = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestImageRoutesValidationAndMilestone(t *testing.T) {
	h, _, _, entryID := newM6Handler(t, milestone.Config{M6Enabled: true})

	if rec := serveJSON(t, h, http.MethodPost, "/api/images/generate", map[string]any{"kind": "scene"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing entry = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := serveJSON(t, h, http.MethodPost, "/api/images/generate", map[string]any{"kind": "illustration"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing chapter = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := serveJSON(t, h, http.MethodPost, "/api/images/generate", map[string]any{"kind": "scene", "entryId": "loc_missing"}); rec.Code != http.StatusNotFound {
		t.Fatalf("missing entry id = %d: %s", rec.Code, rec.Body.String())
	}

	rec := serveJSON(t, h, http.MethodPut, "/api/config/image-model", map[string]any{
		"backend": "sdwebui", "baseUrl": "http://127.0.0.1:7860/", "model": "", "styleTag": "写实",
		"width": 300, "height": 6000, "steps": 0, "timeoutSeconds": 5,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("save image config = %d: %s", rec.Code, rec.Body.String())
	}
	var pub publicImageConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &pub); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if pub.Backend != "sdwebui" || pub.BaseURL != "http://127.0.0.1:7860" || pub.Width != 300 || pub.Height != 2048 || pub.Steps != 20 || pub.TimeoutSeconds != 10 {
		t.Fatalf("normalized config = %+v", pub)
	}
	if rec := serveJSON(t, h, http.MethodPut, "/api/config/image-model", map[string]any{"backend": "dalle"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad backend = %d", rec.Code)
	}
	if rec := serveJSON(t, h, http.MethodPut, "/api/config/image-model", map[string]any{"baseUrl": "ftp://x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad base url = %d", rec.Code)
	}

	// 恢复 openai 配置，确认生成链仍可用
	rec = serveJSON(t, h, http.MethodPut, "/api/config/image-model", map[string]any{
		"backend": "openai", "baseUrl": "http://stub.local/v1", "model": "stub-model",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("restore config = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := serveJSON(t, h, http.MethodPost, "/api/images/generate", map[string]any{"kind": "scene", "entryId": entryID}); rec.Code != http.StatusOK {
		t.Fatalf("generate after config = %d: %s", rec.Code, rec.Body.String())
	}

	off := newM6HandlerForMilestoneOff(t)
	if rec := serveJSON(t, off, http.MethodGet, "/api/images/assets", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("disabled image routes = %d", rec.Code)
	}
	if rec := serveJSON(t, off, http.MethodGet, "/api/features", nil); rec.Code != http.StatusOK {
		t.Fatalf("features off = %d", rec.Code)
	}
}

func newM6HandlerForMilestoneOff(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	static := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	return NewWithMilestone(st, static, nil, &fakeAnalyzer{}, nil, milestone.Config{})
}
