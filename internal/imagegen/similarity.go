package imagegen

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"novelagent/internal/store"
)

const (
	similarityGrid    = 32
	similarityMaxSide = 4096
	similarityMaxArea = 8 << 20
	pixelMethod       = "spatial_rgb_edges_v1"
	pixelNote         = "比较整张图的颜色、轮廓与宽高比；构图、背景和姿势会影响分数，仅供人工参考，不用于识别角色身份。"
)

var ErrInvalidImagePair = errors.New("请选择同一角色的两张不同图像")

// PixelSimilarity is an on-demand comparison of saved originals. A nil score is
// serialized as null so a missing measurement is distinguishable from zero.
type PixelSimilarity struct {
	AssetID   string   `json:"assetId"`
	OtherID   string   `json:"otherId"`
	Method    string   `json:"method"`
	Available bool     `json:"available"`
	Score     *float64 `json:"score"`
	Reason    string   `json:"reason,omitempty"`
	Note      string   `json:"note"`
}

func (s *Service) CompareCharacterImages(ctx context.Context, id, otherID string) (PixelSimilarity, error) {
	if id == "" || otherID == "" || id == otherID {
		return PixelSimilarity{}, ErrInvalidImagePair
	}
	first, err := s.st.GetImageAsset(id)
	if err != nil {
		return PixelSimilarity{}, err
	}
	second, err := s.st.GetImageAsset(otherID)
	if err != nil {
		return PixelSimilarity{}, err
	}
	if first.Kind != "character" || second.Kind != "character" || first.EntryID == "" || first.EntryID != second.EntryID {
		return PixelSimilarity{}, ErrInvalidImagePair
	}

	result := PixelSimilarity{AssetID: id, OtherID: otherID, Method: pixelMethod, Note: pixelNote}
	if first.Status != store.ImageAssetDone || second.Status != store.ImageAssetDone {
		result.Reason = "asset_not_done"
		return result, nil
	}
	select {
	case s.compareSlots <- struct{}{}:
		defer func() { <-s.compareSlots }()
	case <-ctx.Done():
		return PixelSimilarity{}, ctx.Err()
	}
	root, err := os.OpenRoot(s.st.Dir())
	if err != nil {
		result.Reason = "file_unavailable"
		return result, nil
	}
	defer root.Close()
	firstPixels, reason := readPixelSignature(root, first.FileKey)
	if reason == "" {
		if err := ctx.Err(); err != nil {
			return PixelSimilarity{}, err
		}
		var secondPixels pixelSignature
		secondPixels, reason = readPixelSignature(root, second.FileKey)
		if reason == "" {
			score := comparePixels(firstPixels, secondPixels)
			result.Available, result.Score = true, &score
		}
	}
	result.Reason = reason
	return result, nil
}

func readPixelSignature(root *os.Root, key string) (pixelSignature, string) {
	path := filepath.Clean(filepath.FromSlash(key))
	if !strings.HasPrefix(filepath.ToSlash(path), "images/") || !filepath.IsLocal(path) {
		return pixelSignature{}, "file_unavailable"
	}
	// Check the file before opening and recheck the opened handle. OpenRoot
	// prevents a key or symlink from resolving outside the project directory.
	info, err := root.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return pixelSignature{}, "file_unavailable"
	}
	if info.Size() > maxImageBytes {
		return pixelSignature{}, "image_too_large"
	}
	file, err := root.Open(path)
	if err != nil {
		return pixelSignature{}, "file_unavailable"
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return pixelSignature{}, "file_unavailable"
	}
	if info.Size() > maxImageBytes {
		return pixelSignature{}, "image_too_large"
	}
	data, err := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
	if err != nil {
		return pixelSignature{}, "file_unavailable"
	}
	if len(data) > maxImageBytes {
		return pixelSignature{}, "image_too_large"
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return pixelSignature{}, "unsupported_or_invalid_image"
	}
	if config.Width > similarityMaxSide || config.Height > similarityMaxSide || config.Width*config.Height > similarityMaxArea {
		return pixelSignature{}, "image_too_large"
	}
	img, _, err := image.Decode(bytes.NewReader(data)) // GIFs use the first frame.
	if err != nil || img.Bounds().Empty() {
		return pixelSignature{}, "unsupported_or_invalid_image"
	}
	return samplePixels(img, config.Width, config.Height), ""
}

type pixelSignature struct {
	cells  [similarityGrid * similarityGrid][3]float64
	aspect float64
}

func samplePixels(img image.Image, width, height int) pixelSignature {
	// GIF frames can occupy just part of the logical canvas. Sample the whole
	// canvas and treat missing pixels like transparent pixels on white.
	bounds := img.Bounds()
	signature := pixelSignature{aspect: float64(width) / float64(height)}
	const samplesPerSide = 4
	const sampleGrid = similarityGrid * samplesPerSide
	const normalization = 65535.0 * samplesPerSide * samplesPerSide
	for y := range sampleGrid {
		for x := range sampleGrid {
			point := image.Pt((2*x+1)*width/(2*sampleGrid), (2*y+1)*height/(2*sampleGrid))
			var r, g, b, a uint32
			if point.In(bounds) {
				r, g, b, a = img.At(point.X, point.Y).RGBA()
			}
			// RGBA is premultiplied; fill the remaining opacity with white.
			white := 65535 - a
			cell := &signature.cells[(y/samplesPerSide)*similarityGrid+x/samplesPerSide]
			cell[0] += float64(r+white) / normalization
			cell[1] += float64(g+white) / normalization
			cell[2] += float64(b+white) / normalization
		}
	}
	return signature
}

func comparePixels(a, b pixelSignature) float64 {
	// Compare aligned cells and their horizontal/vertical RGB gradients.
	// Root mean square distance keeps local changes visible in the score.
	var colorError, edgeError float64
	for i, cell := range a.cells {
		for channel, value := range cell {
			delta := value - b.cells[i][channel]
			colorError += delta * delta
			if i%similarityGrid > 0 {
				edge := (delta - (a.cells[i-1][channel] - b.cells[i-1][channel])) / 2
				edgeError += edge * edge
			}
			if i >= similarityGrid {
				edge := (delta - (a.cells[i-similarityGrid][channel] - b.cells[i-similarityGrid][channel])) / 2
				edgeError += edge * edge
			}
		}
	}
	colorDistance := math.Sqrt(colorError / (3 * similarityGrid * similarityGrid))
	edgeDistance := math.Sqrt(edgeError / (6 * similarityGrid * (similarityGrid - 1)))
	aspectSimilarity := math.Sqrt(min(a.aspect, b.aspect) / max(a.aspect, b.aspect))
	score := (1 - colorDistance) * (1 - 0.25*edgeDistance) * aspectSimilarity
	return max(0, min(1, score))
}
