package eval

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"strings"
	"time"
)

const (
	reviewSchemaVersion = 1
	reviewInstructions  = "X/Y 为同一停点的两版续写，顺序随机，两版仅记忆注入开关不同。请按 mustLand 三态（landed/summarized/missing）与 mustNot（clear/flagged）逐条评分，并填写人设崩坏、吞伏笔、失忆次数、未授权事实与偏好。评分完成前不要查看 key 文件。"
	recallInstructions  = "X/Y 为同一停点的两版续写，顺序随机，两版仅「前文相关片段」历史召回开关不同（记忆与其他设置一致）。请按 mustLand 三态（landed/summarized/missing）与 mustNot（clear/flagged）逐条评分，并填写人设崩坏、吞伏笔、失忆次数、未授权事实与偏好。评分完成前不要查看 key 文件。"
)

// 对照轴：R3 的记忆开/关与 R4 的召回开/关共用同一套盲评与聚合流程。
const (
	ReviewAxisMemory = "memory"
	ReviewAxisRecall = "recall"
)

type ReviewOptions struct {
	Seed int64
	Now  func() time.Time
	// Axis 为配对对照的能力轴，取 memory（缺省）或 recall。
	Axis string
}

type ReviewPacket struct {
	SchemaVersion int          `json:"schemaVersion"`
	CaseID        string       `json:"caseId"`
	Title         string       `json:"title,omitempty"`
	Axis          string       `json:"axis,omitempty"`
	Seed          int64        `json:"seed"`
	CreatedAt     time.Time    `json:"createdAt"`
	Instructions  string       `json:"instructions"`
	Stops         []ReviewStop `json:"stops"`
}

type ReviewStop struct {
	Name         string       `json:"name"`
	AfterChapter int          `json:"afterChapter"`
	Direction    string       `json:"direction,omitempty"`
	X            string       `json:"x"`
	Y            string       `json:"y"`
	MustLand     []ReviewFact `json:"mustLand,omitempty"`
	MustNot      []ReviewFact `json:"mustNot,omitempty"`
	Score        ReviewScore  `json:"score"`
}

type ReviewFact struct {
	Text string `json:"text"`
	X    string `json:"x,omitempty"`
	Y    string `json:"y,omitempty"`
}

type ReviewScore struct {
	XCharacterBreaks  int      `json:"xCharacterBreaks,omitempty"`
	YCharacterBreaks  int      `json:"yCharacterBreaks,omitempty"`
	XForeshadowMisses int      `json:"xForeshadowMisses,omitempty"`
	YForeshadowMisses int      `json:"yForeshadowMisses,omitempty"`
	XAmnesia          int      `json:"xAmnesia,omitempty"`
	YAmnesia          int      `json:"yAmnesia,omitempty"`
	XUnauthorized     []string `json:"xUnauthorized,omitempty"`
	YUnauthorized     []string `json:"yUnauthorized,omitempty"`
	Preference        string   `json:"preference,omitempty"`
	Reason            string   `json:"reason,omitempty"`
}

type ReviewKey struct {
	SchemaVersion int             `json:"schemaVersion"`
	CaseID        string          `json:"caseId"`
	Axis          string          `json:"axis,omitempty"`
	Seed          int64           `json:"seed"`
	Stops         []ReviewKeyStop `json:"stops"`
}

type ReviewKeyStop struct {
	Name     string `json:"name"`
	MemoryOn string `json:"memoryOn"`
}

type ReviewSummary struct {
	SchemaVersion       int                 `json:"schemaVersion"`
	CaseID              string              `json:"caseId"`
	Axis                string              `json:"axis,omitempty"`
	Stops               int                 `json:"stops"`
	OnWins              int                 `json:"onWins"`
	OffWins             int                 `json:"offWins"`
	Ties                int                 `json:"ties"`
	OnCoverage          float64             `json:"onCoverage"`
	OffCoverage         float64             `json:"offCoverage"`
	OnUnauthorized      int                 `json:"onUnauthorized"`
	OffUnauthorized     int                 `json:"offUnauthorized"`
	OnCharacterBreaks   int                 `json:"onCharacterBreaks"`
	OffCharacterBreaks  int                 `json:"offCharacterBreaks"`
	OnForeshadowMisses  int                 `json:"onForeshadowMisses"`
	OffForeshadowMisses int                 `json:"offForeshadowMisses"`
	OnAmnesia           int                 `json:"onAmnesia"`
	OffAmnesia          int                 `json:"offAmnesia"`
	Pass                bool                `json:"pass"`
	Problems            []string            `json:"problems,omitempty"`
	StopResults         []ReviewStopSummary `json:"stopResults"`
}

type ReviewStopSummary struct {
	Name            string  `json:"name"`
	OnSide          string  `json:"onSide"`
	Preference      string  `json:"preference"`
	OnCoverage      float64 `json:"onCoverage"`
	OffCoverage     float64 `json:"offCoverage"`
	OnUnauthorized  int     `json:"onUnauthorized"`
	OffUnauthorized int     `json:"offUnauthorized"`
}

func BuildReviewPacket(on, off Report, opts ReviewOptions) (ReviewPacket, ReviewKey, error) {
	if strings.TrimSpace(on.CaseID) == "" {
		return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: review requires a case id")
	}
	if on.CaseID != off.CaseID {
		return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: case mismatch: %q vs %q", on.CaseID, off.CaseID)
	}
	if len(on.Stops) == 0 {
		return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: review requires at least one stop")
	}
	if len(off.Stops) != len(on.Stops) {
		return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: stop count mismatch: %d vs %d", len(on.Stops), len(off.Stops))
	}
	axis := strings.ToLower(strings.TrimSpace(opts.Axis))
	if axis == "" {
		axis = ReviewAxisMemory
	}
	if axis != ReviewAxisMemory && axis != ReviewAxisRecall {
		return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: unknown review axis %q (want memory or recall)", opts.Axis)
	}
	offStops := make(map[string]StopReport, len(off.Stops))
	for _, stop := range off.Stops {
		offStops[stop.Name] = stop
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	packet := ReviewPacket{
		SchemaVersion: reviewSchemaVersion,
		CaseID:        on.CaseID,
		Title:         on.Title,
		Axis:          axis,
		Seed:          opts.Seed,
		CreatedAt:     now().UTC(),
		Instructions:  reviewInstructionsFor(axis),
		Stops:         make([]ReviewStop, 0, len(on.Stops)),
	}
	key := ReviewKey{
		SchemaVersion: reviewSchemaVersion,
		CaseID:        on.CaseID,
		Axis:          axis,
		Seed:          opts.Seed,
		Stops:         make([]ReviewKeyStop, 0, len(on.Stops)),
	}
	seen := make(map[string]bool, len(on.Stops))
	for _, stop := range on.Stops {
		if seen[stop.Name] {
			return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: duplicate stop %q", stop.Name)
		}
		seen[stop.Name] = true
		other, ok := offStops[stop.Name]
		if !ok {
			return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: stop %q missing from off report", stop.Name)
		}
		switch axis {
		case ReviewAxisRecall:
			if !stop.Recall.Enabled {
				return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: stop %q in on report must inject recall", stop.Name)
			}
			if other.Recall.Enabled {
				return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: stop %q in off report must not inject recall", stop.Name)
			}
			if stop.UseMemory != other.UseMemory {
				return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: stop %q differs in memory setting; recall axis needs both sides equal", stop.Name)
			}
		default:
			if !stop.UseMemory {
				return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: stop %q in on report must use memory", stop.Name)
			}
			if other.UseMemory {
				return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: stop %q in off report must not use memory", stop.Name)
			}
		}
		if err := reviewOutput(stop, "on"); err != nil {
			return ReviewPacket{}, ReviewKey{}, err
		}
		if err := reviewOutput(other, "off"); err != nil {
			return ReviewPacket{}, ReviewKey{}, err
		}
		if err := sameChecklist(stop.Checklist, other.Checklist); err != nil {
			return ReviewPacket{}, ReviewKey{}, fmt.Errorf("eval: stop %q: %w", stop.Name, err)
		}
		onSide := reviewSide(opts.Seed, stop.Name)
		x, y := stop.Live.Output, other.Live.Output
		if onSide == "y" {
			x, y = y, x
		}
		item := ReviewStop{
			Name:         stop.Name,
			AfterChapter: stop.AfterChapter,
			Direction:    stop.Direction,
			X:            x,
			Y:            y,
			MustLand:     reviewFacts(stop.Checklist, "must_land"),
			MustNot:      reviewFacts(stop.Checklist, "must_not"),
		}
		packet.Stops = append(packet.Stops, item)
		key.Stops = append(key.Stops, ReviewKeyStop{Name: stop.Name, MemoryOn: onSide})
	}
	return packet, key, nil
}

func reviewInstructionsFor(axis string) string {
	if axis == ReviewAxisRecall {
		return recallInstructions
	}
	return reviewInstructions
}

func reviewOutput(stop StopReport, side string) error {
	if stop.Live == nil {
		return fmt.Errorf("eval: stop %q in %s report has no live output", stop.Name, side)
	}
	if stop.Live.Error != "" {
		return fmt.Errorf("eval: stop %q in %s report failed: %s", stop.Name, side, stop.Live.Error)
	}
	if strings.TrimSpace(stop.Live.Output) == "" {
		return fmt.Errorf("eval: stop %q in %s report has empty output", stop.Name, side)
	}
	return nil
}

func sameChecklist(a, b []ChecklistItem) error {
	if len(a) != len(b) {
		return fmt.Errorf("checklist length mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].Text != b[i].Text {
			return fmt.Errorf("checklist item %d mismatch: %s/%q vs %s/%q", i, a[i].Kind, a[i].Text, b[i].Kind, b[i].Text)
		}
	}
	return nil
}

func reviewFacts(items []ChecklistItem, kind string) []ReviewFact {
	facts := make([]ReviewFact, 0, len(items))
	for _, item := range items {
		if item.Kind == kind {
			facts = append(facts, ReviewFact{Text: item.Text})
		}
	}
	return facts
}

func reviewSide(seed int64, name string) string {
	h := fnv.New64a()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(seed))
	_, _ = h.Write(buf[:])
	_, _ = h.Write([]byte(name))
	if h.Sum64()&1 == 0 {
		return "x"
	}
	return "y"
}

func RenderReviewMarkdown(packet ReviewPacket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 长篇续写盲评包：%s\n\n", packet.CaseID)
	if packet.Title != "" {
		fmt.Fprintf(&b, "- 用例：%s\n", packet.Title)
	}
	fmt.Fprintf(&b, "- 停点数：%d\n- 随机种子：%d\n- 生成时间：%s\n\n", len(packet.Stops), packet.Seed, packet.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "> %s\n\n", packet.Instructions)
	for _, stop := range packet.Stops {
		fmt.Fprintf(&b, "## %s（第 %d 章后）\n\n", stop.Name, stop.AfterChapter)
		if stop.Direction != "" {
			fmt.Fprintf(&b, "方向：%s\n\n", stop.Direction)
		}
		fmt.Fprintf(&b, "### X\n\n~~~text\n%s\n~~~\n\n", stop.X)
		fmt.Fprintf(&b, "### Y\n\n~~~text\n%s\n~~~\n\n", stop.Y)
		if len(stop.MustLand) > 0 || len(stop.MustNot) > 0 {
			b.WriteString("### 覆盖核对\n\n| 类型 | 事实 | X | Y |\n|---|---|---|---|\n")
			for _, fact := range stop.MustLand {
				fmt.Fprintf(&b, "| mustLand | %s | %s | %s |\n", fact.Text, fact.X, fact.Y)
			}
			for _, fact := range stop.MustNot {
				fmt.Fprintf(&b, "| mustNot | %s | %s | %s |\n", fact.Text, fact.X, fact.Y)
			}
			b.WriteString("\n")
		}
		b.WriteString("### 人评计数\n\n")
		fmt.Fprintf(&b, "| 指标 | X | Y |\n|---|---|---|\n")
		fmt.Fprintf(&b, "| 人设崩坏 | %s | %s |\n", numberCell(stop.Score.XCharacterBreaks), numberCell(stop.Score.YCharacterBreaks))
		fmt.Fprintf(&b, "| 吞伏笔 | %s | %s |\n", numberCell(stop.Score.XForeshadowMisses), numberCell(stop.Score.YForeshadowMisses))
		fmt.Fprintf(&b, "| 失忆 | %s | %s |\n", numberCell(stop.Score.XAmnesia), numberCell(stop.Score.YAmnesia))
		fmt.Fprintf(&b, "| 未授权事实 | %s | %s |\n", listCell(stop.Score.XUnauthorized), listCell(stop.Score.YUnauthorized))
		fmt.Fprintf(&b, "| 偏好（x/y/tie） | %s | |\n\n", stop.Score.Preference)
	}
	b.WriteString("评分完成后将本包 JSON 的 `score` 字段补齐，再用 `evalreview -score` 与 key 文件聚合。\n")
	return b.String()
}

func numberCell(value int) string {
	if value == 0 {
		return ""
	}
	return fmt.Sprintf("%d", value)
}

func listCell(values []string) string {
	return strings.Join(values, "; ")
}

func AggregateReview(packet ReviewPacket, key ReviewKey) (ReviewSummary, error) {
	if packet.SchemaVersion != reviewSchemaVersion || key.SchemaVersion != reviewSchemaVersion {
		return ReviewSummary{}, fmt.Errorf("eval: unsupported review schema")
	}
	if packet.CaseID == "" || packet.CaseID != key.CaseID {
		return ReviewSummary{}, fmt.Errorf("eval: packet/key case mismatch")
	}
	if packet.Seed != key.Seed {
		return ReviewSummary{}, fmt.Errorf("eval: packet/key seed mismatch")
	}
	if len(packet.Stops) == 0 || len(packet.Stops) != len(key.Stops) {
		return ReviewSummary{}, fmt.Errorf("eval: packet/key stop count mismatch")
	}
	keyStops := make(map[string]ReviewKeyStop, len(key.Stops))
	for _, item := range key.Stops {
		if item.MemoryOn != "x" && item.MemoryOn != "y" {
			return ReviewSummary{}, fmt.Errorf("eval: stop %q has invalid memoryOn %q", item.Name, item.MemoryOn)
		}
		keyStops[item.Name] = item
	}
	axis := key.Axis
	if axis == "" {
		axis = packet.Axis
	}
	if axis == "" {
		// 旧的盲评包没有 axis 字段，当时只可能有记忆对照轴。
		axis = ReviewAxisMemory
	}
	if axis != ReviewAxisMemory && axis != ReviewAxisRecall {
		return ReviewSummary{}, fmt.Errorf("eval: unknown review axis %q (want memory or recall)", axis)
	}
	onLabel, offLabel := "memory-on", "memory-off"
	if axis == ReviewAxisRecall {
		onLabel, offLabel = "recall-on", "recall-off"
	}
	summary := ReviewSummary{
		SchemaVersion: reviewSchemaVersion,
		CaseID:        packet.CaseID,
		Axis:          axis,
		Stops:         len(packet.Stops),
		StopResults:   make([]ReviewStopSummary, 0, len(packet.Stops)),
	}
	var onFactPoints, offFactPoints float64
	var factCount int
	for _, stop := range packet.Stops {
		keyStop, ok := keyStops[stop.Name]
		if !ok {
			return ReviewSummary{}, fmt.Errorf("eval: stop %q missing from key", stop.Name)
		}
		preference := strings.ToLower(strings.TrimSpace(stop.Score.Preference))
		if preference != "x" && preference != "y" && preference != "tie" {
			return ReviewSummary{}, fmt.Errorf("eval: stop %q preference must be x/y/tie", stop.Name)
		}
		var onPoints, offPoints float64
		var onFacts, offFacts int
		for _, fact := range stop.MustLand {
			xState, err := factState(fact.X, "landed", "summarized", "missing")
			if err != nil {
				return ReviewSummary{}, fmt.Errorf("eval: stop %q fact %q x: %w", stop.Name, fact.Text, err)
			}
			yState, err := factState(fact.Y, "landed", "summarized", "missing")
			if err != nil {
				return ReviewSummary{}, fmt.Errorf("eval: stop %q fact %q y: %w", stop.Name, fact.Text, err)
			}
			onState, offState := xState, yState
			if keyStop.MemoryOn == "y" {
				onState, offState = yState, xState
			}
			onPoints += coveragePoints(onState)
			offPoints += coveragePoints(offState)
			onFacts++
			offFacts++
		}
		for _, fact := range stop.MustNot {
			xState, err := factState(fact.X, "clear", "flagged")
			if err != nil {
				return ReviewSummary{}, fmt.Errorf("eval: stop %q fact %q x: %w", stop.Name, fact.Text, err)
			}
			yState, err := factState(fact.Y, "clear", "flagged")
			if err != nil {
				return ReviewSummary{}, fmt.Errorf("eval: stop %q fact %q y: %w", stop.Name, fact.Text, err)
			}
			onState, offState := xState, yState
			if keyStop.MemoryOn == "y" {
				onState, offState = yState, xState
			}
			onPoints += coveragePoints(onState)
			offPoints += coveragePoints(offState)
			onFacts++
			offFacts++
		}
		onCoverage := 0.0
		offCoverage := 0.0
		if onFacts > 0 {
			onCoverage = onPoints / float64(onFacts)
			offCoverage = offPoints / float64(offFacts)
			onFactPoints += onPoints
			offFactPoints += offPoints
			factCount += onFacts
		}
		onUnauthorized := len(stop.Score.XUnauthorized)
		offUnauthorized := len(stop.Score.YUnauthorized)
		if keyStop.MemoryOn == "y" {
			onUnauthorized, offUnauthorized = offUnauthorized, onUnauthorized
		}
		onBreaks, offBreaks := stop.Score.XCharacterBreaks, stop.Score.YCharacterBreaks
		onForeshadow, offForeshadow := stop.Score.XForeshadowMisses, stop.Score.YForeshadowMisses
		onAmnesia, offAmnesia := stop.Score.XAmnesia, stop.Score.YAmnesia
		if keyStop.MemoryOn == "y" {
			onBreaks, offBreaks = offBreaks, onBreaks
			onForeshadow, offForeshadow = offForeshadow, onForeshadow
			onAmnesia, offAmnesia = offAmnesia, onAmnesia
		}
		switch preference {
		case "tie":
			summary.Ties++
		default:
			if preference == keyStop.MemoryOn {
				summary.OnWins++
			} else {
				summary.OffWins++
			}
		}
		summary.OnUnauthorized += onUnauthorized
		summary.OffUnauthorized += offUnauthorized
		summary.OnCharacterBreaks += onBreaks
		summary.OffCharacterBreaks += offBreaks
		summary.OnForeshadowMisses += onForeshadow
		summary.OffForeshadowMisses += offForeshadow
		summary.OnAmnesia += onAmnesia
		summary.OffAmnesia += offAmnesia
		summary.StopResults = append(summary.StopResults, ReviewStopSummary{
			Name:            stop.Name,
			OnSide:          keyStop.MemoryOn,
			Preference:      preference,
			OnCoverage:      onCoverage,
			OffCoverage:     offCoverage,
			OnUnauthorized:  onUnauthorized,
			OffUnauthorized: offUnauthorized,
		})
	}
	if factCount > 0 {
		summary.OnCoverage = onFactPoints / float64(factCount)
		summary.OffCoverage = offFactPoints / float64(factCount)
	}
	if summary.Stops < 3 {
		summary.Problems = append(summary.Problems, "R3 requires at least 3 stops")
	}
	needed := (2*summary.Stops + 2) / 3
	if summary.OnWins+summary.Ties < needed {
		summary.Problems = append(summary.Problems, fmt.Sprintf("%s wins/ties %d < %d required", onLabel, summary.OnWins+summary.Ties, needed))
	}
	if summary.OnUnauthorized > summary.OffUnauthorized {
		summary.Problems = append(summary.Problems, fmt.Sprintf("%s introduces more unauthorized facts than %s", onLabel, offLabel))
	}
	summary.Pass = len(summary.Problems) == 0
	return summary, nil
}

func factState(value string, allowed ...string) (string, error) {
	state := strings.ToLower(strings.TrimSpace(value))
	if state == "" {
		return "", fmt.Errorf("unscored")
	}
	for _, item := range allowed {
		if state == item {
			return state, nil
		}
	}
	return "", fmt.Errorf("invalid state %q (want %s)", value, strings.Join(allowed, "/"))
}

func coveragePoints(state string) float64 {
	switch state {
	case "landed", "clear":
		return 1
	case "summarized":
		return 0.5
	default:
		return 0
	}
}
