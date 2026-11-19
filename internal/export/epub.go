package export

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

const epubCSS = `body { font-family: "Songti SC", "Noto Serif CJK SC", serif; line-height: 1.8; margin: 1.2em; }
h1, h2 { font-weight: 600; }
p { text-indent: 2em; margin: 0 0 0.6em; }
figure { margin: 1.2em 0; text-align: center; }
figure img { max-width: 100%; }
figcaption, .caption { color: #666; font-size: 0.85em; text-indent: 0; }
.cover { text-align: center; }
.cover img { max-width: 100%; max-height: 90vh; }
.gallery-item { margin-bottom: 1.6em; text-align: center; }
.gallery-item img { max-width: 100%; }
`

type epubImage struct {
	asset store.ImageAsset
	href  string
	mime  string
	data  []byte
}

type galleryItem struct {
	href    string
	caption string
}

type manifestItem struct {
	id, href, mime, properties string
}

type spineItem struct{ id string }

func (s *Service) buildEPUB(title string, chapters []novel.Chapter, images []store.ImageAsset) ([]byte, int, error) {
	embeds := make(map[string]*epubImage)
	order := make([]*epubImage, 0, len(images))
	add := func(asset store.ImageAsset) (*epubImage, bool) {
		if existing, ok := embeds[asset.ID]; ok {
			return existing, true
		}
		data, err := s.readImage(asset)
		if err != nil {
			s.log.Warn("export: skip unreadable image", "asset", asset.ID, "err", err)
			return nil, false
		}
		ext := filepath.Ext(asset.FileKey)
		if ext == "" {
			ext = ".png"
		}
		image := &epubImage{
			asset: asset,
			href:  "images/" + asset.ID + ext,
			mime:  imageMIME(ext),
			data:  data,
		}
		embeds[asset.ID] = image
		order = append(order, image)
		return image, true
	}

	chapterFigures := map[string]string{}
	used := map[string]struct{}{}
	for _, chapter := range chapters {
		asset, ok := chapterImage(images, chapter)
		if !ok {
			continue
		}
		if image, ok := add(asset); ok {
			chapterFigures[chapter.ID] = image.href
			used[asset.ID] = struct{}{}
		}
	}

	var cover *epubImage
	for _, asset := range images {
		if asset.Kind != "cover" {
			continue
		}
		if cover != nil && !asset.IsAnchor {
			continue
		}
		if image, ok := add(asset); ok {
			cover = image
			if asset.IsAnchor {
				break
			}
		}
	}
	if cover != nil {
		used[cover.asset.ID] = struct{}{}
	}

	gallery := make([]galleryItem, 0, len(images))
	for _, asset := range images {
		switch asset.Kind {
		case "character", "scene", "item":
		default:
			continue
		}
		if _, ok := used[asset.ID]; ok {
			continue
		}
		image, ok := add(asset)
		if !ok {
			continue
		}
		gallery = append(gallery, galleryItem{
			href:    image.href,
			caption: fmt.Sprintf("%s · %s", kindLabel(asset.Kind), s.entryName(asset.EntryID)),
		})
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeStoredZipEntry(zw, "mimetype", []byte("application/epub+zip")); err != nil {
		return nil, 0, err
	}
	container := `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
`
	if err := writeZipEntry(zw, "META-INF/container.xml", []byte(container)); err != nil {
		return nil, 0, err
	}
	if err := writeZipEntry(zw, "OEBPS/style.css", []byte(epubCSS)); err != nil {
		return nil, 0, err
	}

	for _, image := range order {
		if err := writeZipEntry(zw, "OEBPS/"+image.href, image.data); err != nil {
			return nil, 0, err
		}
	}

	manifest := []manifestItem{
		{id: "nav", href: "nav.xhtml", mime: "application/xhtml+xml", properties: "nav"},
		{id: "css", href: "style.css", mime: "text/css"},
	}
	spine := make([]spineItem, 0, len(chapters)+3)
	if cover != nil {
		manifest = append(manifest, manifestItem{id: "cover-xhtml", href: "cover.xhtml", mime: "application/xhtml+xml"})
		manifest = append(manifest, manifestItem{id: "cover-image", href: cover.href, mime: cover.mime, properties: "cover-image"})
		spine = append(spine, spineItem{id: "cover-xhtml"})
		if err := writeZipEntry(zw, "OEBPS/cover.xhtml", []byte(coverXHTML(title, cover.href))); err != nil {
			return nil, 0, err
		}
	}
	spine = append(spine, spineItem{id: "nav"})

	navItems := make([]string, 0, len(chapters))
	for index, chapter := range chapters {
		id := fmt.Sprintf("chapter-%d", index+1)
		href := id + ".xhtml"
		manifest = append(manifest, manifestItem{id: id, href: href, mime: "application/xhtml+xml"})
		spine = append(spine, spineItem{id: id})
		navItems = append(navItems, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, href, xmlEscape(chapter.Title)))
		if err := writeZipEntry(zw, "OEBPS/"+href, []byte(chapterXHTML(chapter, chapterFigures[chapter.ID]))); err != nil {
			return nil, 0, err
		}
	}

	for _, image := range order {
		manifest = append(manifest, manifestItem{id: "img-" + image.asset.ID, href: image.href, mime: image.mime})
	}

	if len(gallery) > 0 {
		manifest = append(manifest, manifestItem{id: "gallery", href: "gallery.xhtml", mime: "application/xhtml+xml"})
		spine = append(spine, spineItem{id: "gallery"})
		navItems = append(navItems, `<li><a href="gallery.xhtml">图像图鉴</a></li>`)
		if err := writeZipEntry(zw, "OEBPS/gallery.xhtml", []byte(galleryXHTML(title, gallery))); err != nil {
			return nil, 0, err
		}
	}

	nav := navXHTML(title, strings.Join(navItems, "\n"))
	if err := writeZipEntry(zw, "OEBPS/nav.xhtml", []byte(nav)); err != nil {
		return nil, 0, err
	}
	if err := writeZipEntry(zw, "OEBPS/content.opf", []byte(contentOPF(title, manifest, spine))); err != nil {
		return nil, 0, err
	}
	if err := zw.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), len(order), nil
}

func writeStoredZipEntry(zw *zip.Writer, name string, data []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Store, Modified: time.Now()}
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func contentOPF(title string, manifest []manifestItem, spine []spineItem) string {
	modified := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	var items strings.Builder
	for _, item := range manifest {
		props := ""
		if item.properties != "" {
			props = ` properties="` + item.properties + `"`
		}
		fmt.Fprintf(&items, `    <item id="%s" href="%s" media-type="%s"%s/>`+"\n", item.id, item.href, item.mime, props)
	}
	var refs strings.Builder
	for _, item := range spine {
		fmt.Fprintf(&refs, `    <itemref idref="%s"/>`+"\n", item.id)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">urn:uuid:%s</dc:identifier>
    <dc:title>%s</dc:title>
    <dc:language>zh-CN</dc:language>
    <meta property="dcterms:modified">%s</meta>
  </metadata>
  <manifest>
%s  </manifest>
  <spine>
%s  </spine>
</package>
`, uuid4(), xmlEscape(title), modified, items.String(), refs.String())
}

func navXHTML(title, items string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="zh-CN">
<head><title>目录</title><link rel="stylesheet" type="text/css" href="style.css"/></head>
<body>
  <nav epub:type="toc" id="toc">
    <h1>%s</h1>
    <ol>
%s
    </ol>
  </nav>
</body>
</html>
`, xmlEscape(title), indent(items, "      "))
}

func coverXHTML(title, href string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="zh-CN">
<head><title>封面</title><link rel="stylesheet" type="text/css" href="style.css"/></head>
<body>
  <div class="cover">
    <img src="%s" alt="%s"/>
  </div>
</body>
</html>
`, href, xmlEscape(title))
}

func chapterXHTML(chapter novel.Chapter, figure string) string {
	var body strings.Builder
	body.WriteString("  <h2>" + xmlEscape(chapter.Title) + "</h2>\n")
	for _, line := range strings.Split(strings.ReplaceAll(chapter.Content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		body.WriteString("  <p>" + xmlEscape(line) + "</p>\n")
	}
	if figure != "" {
		body.WriteString(`  <figure><img src="` + figure + `" alt="插图"/><figcaption>` + xmlEscape(chapter.Title) + ` · 插图</figcaption></figure>` + "\n")
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="zh-CN">
<head><title>%s</title><link rel="stylesheet" type="text/css" href="style.css"/></head>
<body>
%s</body>
</html>
`, xmlEscape(chapter.Title), body.String())
}

func galleryXHTML(title string, items []galleryItem) string {
	var body strings.Builder
	body.WriteString("  <h2>图像图鉴</h2>\n")
	for _, item := range items {
		body.WriteString(`  <div class="gallery-item"><img src="` + item.href + `" alt="` + xmlEscape(item.caption) + `"/><p class="caption">` + xmlEscape(item.caption) + "</p></div>\n")
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="zh-CN">
<head><title>图像图鉴</title><link rel="stylesheet" type="text/css" href="style.css"/></head>
<body>
%s</body>
</html>
`, body.String())
}

func indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}

func imageMIME(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/png"
	}
}

func (s *Service) entryName(entryID string) string {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return "全书"
	}
	if entry, err := s.w.GetEntry(entryID); err == nil && strings.TrimSpace(entry.Name) != "" {
		return entry.Name
	}
	return entryID
}

func kindLabel(kind string) string {
	switch kind {
	case "character":
		return "角色"
	case "scene":
		return "场景"
	case "item":
		return "物品"
	case "cover":
		return "封面"
	default:
		return kind
	}
}

func uuid4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", time.Now().UnixNano()%1e12)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16]))
}
