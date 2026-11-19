// Package export 生成可离线阅读的整书导出（TXT / Markdown / EPUB3）。
// 导出是快照：文件名与元数据写在项目目录的 exports/ 下，不进入书目真源。
package export

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"novelagent/internal/novel"
	"novelagent/internal/store"
	"novelagent/internal/world"
)

const (
	FormatTXT  = "txt"
	FormatMD   = "md"
	FormatEPUB = "epub"
)

var (
	ErrInvalidFormat  = errors.New("export: unsupported format")
	ErrExportNotFound = errors.New("export: record not found")
)

// Request 是一次导出请求；IncludeImages 仅对 txt/md 生效（生成 zip 素材包）。
type Request struct {
	Format        string `json:"format"`
	Title         string `json:"title"`
	IncludeImages bool   `json:"includeImages"`
}

// Record 是导出结果的公开元数据（保存在 exports/<id>.json）。
type Record struct {
	ID            string    `json:"id"`
	Format        string    `json:"format"`
	Title         string    `json:"title"`
	FileName      string    `json:"fileName"`
	SizeBytes     int64     `json:"sizeBytes"`
	ChapterCount  int       `json:"chapterCount"`
	ImageCount    int       `json:"imageCount"`
	IncludeImages bool      `json:"includeImages"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Service struct {
	st  *store.Store
	log *slog.Logger
	w   *world.Service
}

func NewService(st *store.Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{st: st, log: logger, w: world.NewService(st, logger)}
}

func (s *Service) dir() string {
	return filepath.Join(s.st.Dir(), "exports")
}

// Create 生成导出文件并记录元数据；返回记录（File 已落盘）。
func (s *Service) Create(req Request) (Record, error) {
	format := strings.ToLower(strings.TrimSpace(req.Format))
	switch format {
	case FormatTXT, FormatMD, FormatEPUB:
	default:
		return Record{}, fmt.Errorf("%w: %q (txt|md|epub)", ErrInvalidFormat, req.Format)
	}
	project, err := s.st.LoadProject()
	if err != nil {
		return Record{}, err
	}
	chapters, err := s.st.LoadChapters()
	if err != nil {
		return Record{}, err
	}
	images, err := s.st.ListExportImages()
	if err != nil {
		return Record{}, err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = strings.TrimSpace(project.Title)
	}
	if title == "" {
		title = "未命名小说"
	}
	includeImages := req.IncludeImages && format != FormatEPUB
	if len(chapters) == 0 {
		return Record{}, errors.New("export: no chapters to export")
	}

	data, usedImages, err := s.build(format, title, chapters, images, includeImages)
	if err != nil {
		return Record{}, err
	}
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return Record{}, fmt.Errorf("export: create dir: %w", err)
	}
	id := novel.NewID("exp")
	ext := "." + format
	if includeImages {
		ext += ".zip"
	}
	fileName := fmt.Sprintf("%s_%s%s", safeFileName(title), id, ext)
	path := filepath.Join(s.dir(), fileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Record{}, fmt.Errorf("export: write file: %w", err)
	}
	record := Record{
		ID:            id,
		Format:        format,
		Title:         title,
		FileName:      fileName,
		SizeBytes:     int64(len(data)),
		ChapterCount:  len(chapters),
		ImageCount:    usedImages,
		IncludeImages: includeImages,
		CreatedAt:     time.Now().UTC(),
	}
	meta, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return Record{}, err
	}
	if err := os.WriteFile(filepath.Join(s.dir(), id+".json"), meta, 0o644); err != nil {
		return Record{}, fmt.Errorf("export: write metadata: %w", err)
	}
	s.log.Info("export created", "id", id, "format", format, "bytes", record.SizeBytes, "images", usedImages)
	return record, nil
}

func (s *Service) List() ([]Record, error) {
	entries, err := os.ReadDir(s.dir())
	if err != nil {
		if os.IsNotExist(err) {
			return []Record{}, nil
		}
		return nil, err
	}
	out := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		record, err := readRecord(filepath.Join(s.dir(), entry.Name()))
		if err != nil {
			continue
		}
		out = append(out, record)
	}
	sortRecords(out)
	return out, nil
}

func (s *Service) Get(id string) (Record, error) {
	id = strings.TrimSpace(id)
	if !novel.ValidID(id) || !strings.HasPrefix(id, "exp_") {
		return Record{}, ErrExportNotFound
	}
	record, err := readRecord(filepath.Join(s.dir(), id+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Record{}, ErrExportNotFound
		}
		return Record{}, err
	}
	return record, nil
}

// FilePath 返回记录对应导出文件的绝对路径，并防御元数据被篡改的路径穿越。
func (s *Service) FilePath(record Record) (string, error) {
	name := filepath.Base(strings.TrimSpace(record.FileName))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", ErrExportNotFound
	}
	return filepath.Join(s.dir(), name), nil
}

func readRecord(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func sortRecords(records []Record) {
	for i := 1; i < len(records); i++ {
		for j := i; j > 0 && records[j].CreatedAt.After(records[j-1].CreatedAt); j-- {
			records[j], records[j-1] = records[j-1], records[j]
		}
	}
}

var unsafeFileName = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

func safeFileName(title string) string {
	title = unsafeFileName.ReplaceAllString(strings.TrimSpace(title), "_")
	title = strings.Trim(title, " .")
	if title == "" {
		title = "book"
	}
	runes := []rune(title)
	if len(runes) > 60 {
		title = string(runes[:60])
	}
	return title
}

func (s *Service) build(format, title string, chapters []novel.Chapter, images []store.ImageAsset, includeImages bool) ([]byte, int, error) {
	switch format {
	case FormatEPUB:
		data, count, err := s.buildEPUB(title, chapters, images)
		return data, count, err
	case FormatMD:
		doc := buildMarkdown(title, chapters, images, includeImages)
		if !includeImages {
			return []byte(doc), 0, nil
		}
		data, count, err := s.zipDocument(title+".md", []byte(doc), images)
		return data, count, err
	default:
		doc := buildText(title, chapters, images, includeImages)
		if !includeImages {
			return []byte(doc), 0, nil
		}
		data, count, err := s.zipDocument(title+".txt", []byte(doc), images)
		return data, count, err
	}
}

func buildText(title string, chapters []novel.Chapter, images []store.ImageAsset, reference bool) string {
	var buf strings.Builder
	buf.WriteString(title)
	buf.WriteString("\n\n")
	for _, chapter := range chapters {
		buf.WriteString(chapter.Title)
		buf.WriteString("\n\n")
		buf.WriteString(strings.TrimSpace(chapter.Content))
		buf.WriteString("\n\n")
		if reference {
			if asset, ok := chapterImage(images, chapter); ok {
				fmt.Fprintf(&buf, "[插图：images/%s]\n\n", imageName(asset))
			}
		}
	}
	return buf.String()
}

func buildMarkdown(title string, chapters []novel.Chapter, images []store.ImageAsset, reference bool) string {
	var buf strings.Builder
	buf.WriteString("# " + title + "\n\n")
	for _, chapter := range chapters {
		buf.WriteString("## " + chapter.Title + "\n\n")
		buf.WriteString(strings.TrimSpace(chapter.Content))
		buf.WriteString("\n\n")
		if reference {
			if asset, ok := chapterImage(images, chapter); ok {
				fmt.Fprintf(&buf, "![%s](images/%s)\n\n", escapeMarkdown(chapter.Title), imageName(asset))
			}
		}
	}
	return buf.String()
}

func escapeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "[", "\\[")
	value = strings.ReplaceAll(value, "]", "\\]")
	return value
}

func (s *Service) zipDocument(docName string, doc []byte, images []store.ImageAsset) ([]byte, int, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeZipEntry(zw, docName, doc); err != nil {
		return nil, 0, err
	}
	count := 0
	seen := map[string]struct{}{}
	for _, asset := range images {
		name := imageName(asset)
		if _, ok := seen[name]; ok {
			continue
		}
		data, err := s.readImage(asset)
		if err != nil {
			continue
		}
		seen[name] = struct{}{}
		if err := writeZipEntry(zw, "images/"+name, data); err != nil {
			return nil, 0, err
		}
		count++
	}
	if err := zw.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), count, nil
}

func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()}
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func (s *Service) readImage(asset store.ImageAsset) ([]byte, error) {
	key := strings.TrimSpace(asset.FileKey)
	if key == "" {
		return nil, errors.New("export: empty image file key")
	}
	return os.ReadFile(filepath.Join(s.st.Dir(), filepath.FromSlash(key)))
}

func imageName(asset store.ImageAsset) string {
	ext := filepath.Ext(asset.FileKey)
	if ext == "" {
		ext = ".png"
	}
	return asset.ID + ext
}

func chapterImage(images []store.ImageAsset, chapter novel.Chapter) (store.ImageAsset, bool) {
	var candidate store.ImageAsset
	found := false
	for _, asset := range images {
		if asset.Kind != "illustration" || asset.ChapterID != chapter.ID {
			continue
		}
		if asset.IsAnchor {
			return asset, true
		}
		if !found || asset.CreatedAt.Before(candidate.CreatedAt) {
			candidate = asset
			found = true
		}
	}
	return candidate, found
}
