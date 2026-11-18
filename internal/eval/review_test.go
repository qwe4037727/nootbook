package eval

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func reviewChecklist() []ChecklistItem {
	return []ChecklistItem{
		{Kind: "must_land", Text: "左臂旧伤", Hint: "observed"},
		{Kind: "must_not", Text: "完全痊愈", Hint: "flagged"},
	}
}

func reviewStop(name string, useMemory bool, output string) StopReport {
	return StopReport{
		Name:         name,
		AfterChapter: 10,
		Direction:    "继续追查。",
		UseMemory:    useMemory,
		Checklist:    reviewChecklist(),
		Live:         &LiveReport{Output: output},
	}
}

func reviewReports() (Report, Report) {
	on := Report{CaseID: "review-case", Title: "审阅用例", Mode: "live", Stops: []StopReport{
		reviewStop("early", true, "output-alpha"),
		reviewStop("middle", true, "output-beta"),
		reviewStop("late", true, "output-gamma"),
	}}
	off := Report{CaseID: "review-case", Title: "审阅用例", Mode: "live", Stops: []StopReport{
		reviewStop("early", false, "output-delta"),
		reviewStop("middle", false, "output-epsilon"),
		reviewStop("late", false, "output-zeta"),
	}}
	return on, off
}

func TestBuildReviewPacketIsDeterministicAndBlind(t *testing.T) {
	on, off := reviewReports()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	opts := ReviewOptions{Seed: 42, Now: func() time.Time { return now }}
	packet, key, err := BuildReviewPacket(on, off, opts)
	if err != nil {
		t.Fatalf("BuildReviewPacket: %v", err)
	}
	packetAgain, keyAgain, err := BuildReviewPacket(on, off, opts)
	if err != nil {
		t.Fatalf("BuildReviewPacket again: %v", err)
	}
	if !reflect.DeepEqual(packet, packetAgain) || !reflect.DeepEqual(key, keyAgain) {
		t.Fatalf("packet/key not deterministic")
	}
	if packet.SchemaVersion != reviewSchemaVersion || key.SchemaVersion != reviewSchemaVersion {
		t.Fatalf("schema = %d/%d", packet.SchemaVersion, key.SchemaVersion)
	}
	if len(packet.Stops) != 3 || len(key.Stops) != 3 {
		t.Fatalf("stops = %d/%d", len(packet.Stops), len(key.Stops))
	}
	for i, stop := range packet.Stops {
		wantOn := on.Stops[i].Live.Output
		wantOff := off.Stops[i].Live.Output
		gotOn, gotOff := stop.X, stop.Y
		if key.Stops[i].MemoryOn == "y" {
			gotOn, gotOff = stop.Y, stop.X
		}
		if gotOn != wantOn || gotOff != wantOff {
			t.Fatalf("stop %s mapping on=%q off=%q, want %q/%q", stop.Name, gotOn, gotOff, wantOn, wantOff)
		}
		if len(stop.MustLand) != 1 || len(stop.MustNot) != 1 {
			t.Fatalf("stop %s facts = %+v/%+v", stop.Name, stop.MustLand, stop.MustNot)
		}
	}
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatalf("marshal packet: %v", err)
	}
	for _, forbidden := range []string{"memoryOn", "useMemory", "memory_on"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("packet leaks %q: %s", forbidden, data)
		}
	}
	markdown := RenderReviewMarkdown(packet)
	for _, want := range []string{"长篇续写盲评包", "output-alpha", "output-delta", "左臂旧伤", "偏好", "未授权事实"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

func TestAggregateReviewMapsSidesAndPasses(t *testing.T) {
	on, off := reviewReports()
	packet, key, err := BuildReviewPacket(on, off, ReviewOptions{Seed: 7})
	if err != nil {
		t.Fatalf("BuildReviewPacket: %v", err)
	}
	for i := range packet.Stops {
		onSide := key.Stops[i].MemoryOn
		for j := range packet.Stops[i].MustLand {
			setFact(&packet.Stops[i].MustLand[j], onSide, "landed", "missing")
		}
		for j := range packet.Stops[i].MustNot {
			setFact(&packet.Stops[i].MustNot[j], onSide, "clear", "flagged")
		}
		if i < 2 {
			packet.Stops[i].Score.Preference = onSide
		} else {
			packet.Stops[i].Score.Preference = "tie"
		}
	}
	if key.Stops[0].MemoryOn == "x" {
		packet.Stops[0].Score.YUnauthorized = []string{"提前揭示旧案主谋"}
	} else {
		packet.Stops[0].Score.XUnauthorized = []string{"提前揭示旧案主谋"}
	}
	summary, err := AggregateReview(packet, key)
	if err != nil {
		t.Fatalf("AggregateReview: %v", err)
	}
	if summary.OnWins != 2 || summary.OffWins != 0 || summary.Ties != 1 {
		t.Fatalf("wins = %d/%d/%d", summary.OnWins, summary.OffWins, summary.Ties)
	}
	if summary.OnCoverage != 1 || summary.OffCoverage != 0 {
		t.Fatalf("coverage = %.2f/%.2f", summary.OnCoverage, summary.OffCoverage)
	}
	if summary.OnUnauthorized != 0 || summary.OffUnauthorized != 1 {
		t.Fatalf("unauthorized = %d/%d, want 0/1", summary.OnUnauthorized, summary.OffUnauthorized)
	}
	if !summary.Pass {
		t.Fatalf("summary should pass: %+v", summary)
	}
	if len(summary.StopResults) != 3 || summary.StopResults[0].OnSide != key.Stops[0].MemoryOn {
		t.Fatalf("stop results = %+v", summary.StopResults)
	}
}

func setFact(fact *ReviewFact, onSide, onState, offState string) {
	if onSide == "x" {
		fact.X, fact.Y = onState, offState
		return
	}
	fact.X, fact.Y = offState, onState
}

func TestAggregateReviewFlagsUnauthorizedAndUnscored(t *testing.T) {
	on, off := reviewReports()
	packet, key, err := BuildReviewPacket(on, off, ReviewOptions{Seed: 9})
	if err != nil {
		t.Fatalf("BuildReviewPacket: %v", err)
	}
	if _, err := AggregateReview(packet, key); err == nil {
		t.Fatal("expected unscored error")
	}
	scorePacket(t, &packet, key)
	packet.Stops[0].Score.Preference = key.Stops[0].MemoryOn
	if key.Stops[0].MemoryOn == "x" {
		packet.Stops[0].Score.XUnauthorized = []string{"新增事实"}
	} else {
		packet.Stops[0].Score.YUnauthorized = []string{"新增事实"}
	}
	summary, err := AggregateReview(packet, key)
	if err != nil {
		t.Fatalf("AggregateReview: %v", err)
	}
	if summary.Pass || len(summary.Problems) == 0 {
		t.Fatalf("expected unauthorized failure: %+v", summary)
	}
}

func scorePacket(t *testing.T, packet *ReviewPacket, key ReviewKey) {
	t.Helper()
	for i := range packet.Stops {
		onSide := key.Stops[i].MemoryOn
		for j := range packet.Stops[i].MustLand {
			setFact(&packet.Stops[i].MustLand[j], onSide, "landed", "landed")
		}
		for j := range packet.Stops[i].MustNot {
			setFact(&packet.Stops[i].MustNot[j], onSide, "clear", "clear")
		}
		packet.Stops[i].Score.Preference = "tie"
	}
}

func TestBuildReviewPacketRejectsInvalidInputs(t *testing.T) {
	on, off := reviewReports()
	if _, _, err := BuildReviewPacket(on, off, ReviewOptions{Seed: 1}); err != nil {
		t.Fatalf("valid reports rejected: %v", err)
	}
	badCase := off
	badCase.CaseID = "other"
	if _, _, err := BuildReviewPacket(on, badCase, ReviewOptions{}); err == nil {
		t.Fatal("expected case mismatch error")
	}
	badMemory := off
	badMemory.Stops = append([]StopReport(nil), off.Stops...)
	badMemory.Stops[0].UseMemory = true
	if _, _, err := BuildReviewPacket(on, badMemory, ReviewOptions{}); err == nil {
		t.Fatal("expected useMemory mismatch error")
	}
	noOutput := off
	noOutput.Stops = append([]StopReport(nil), off.Stops...)
	noOutput.Stops[0].Live = &LiveReport{Error: "timeout"}
	if _, _, err := BuildReviewPacket(on, noOutput, ReviewOptions{}); err == nil {
		t.Fatal("expected failed live output error")
	}
}

func recallStop(name string, useMemory, recallOn bool, output string) StopReport {
	stop := reviewStop(name, useMemory, output)
	stop.Recall.Requested = "off"
	if recallOn {
		stop.Recall = RecallMetrics{
			Requested:     "on",
			Enabled:       true,
			AutoEligible:  true,
			BudgetChars:   800,
			InjectedChars: 640,
			Hits:          1,
			Chapters:      []int{3},
		}
	}
	return stop
}

func recallReports() (Report, Report) {
	newReport := func(recallOn bool, outputs [3]string) Report {
		stops := make([]StopReport, 0, 3)
		for i, name := range []string{"early", "middle", "late"} {
			stops = append(stops, recallStop(name, false, recallOn, outputs[i]))
		}
		return Report{CaseID: "recall-case", Title: "召回用例", Mode: "live", Stops: stops}
	}
	return newReport(true, [3]string{"recall-alpha", "recall-beta", "recall-gamma"}),
		newReport(false, [3]string{"recall-delta", "recall-epsilon", "recall-zeta"})
}

func TestBuildReviewPacketRecallAxisPairsRecallSwitch(t *testing.T) {
	on, off := recallReports()
	packet, key, err := BuildReviewPacket(on, off, ReviewOptions{Seed: 11, Axis: ReviewAxisRecall})
	if err != nil {
		t.Fatalf("recall axis rejected: %v", err)
	}
	if packet.Axis != ReviewAxisRecall || key.Axis != ReviewAxisRecall {
		t.Fatalf("axis = %q/%q", packet.Axis, key.Axis)
	}
	if packet.Instructions != recallInstructions || !strings.Contains(packet.Instructions, "前文相关片段") {
		t.Fatalf("unexpected recall instructions: %q", packet.Instructions)
	}
	if packet.Instructions == reviewInstructions {
		t.Fatal("recall and memory axes share identical instructions")
	}
	if !strings.Contains(RenderReviewMarkdown(packet), "前文相关片段") {
		t.Fatal("markdown missing recall wording")
	}
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatalf("marshal packet: %v", err)
	}
	for _, forbidden := range []string{"memoryOn", "useMemory", "recallOn", "injectedChars"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("packet leaks %q: %s", forbidden, data)
		}
	}
	for i, stop := range packet.Stops {
		wantOn, wantOff := on.Stops[i].Live.Output, off.Stops[i].Live.Output
		gotOn, gotOff := stop.X, stop.Y
		if key.Stops[i].MemoryOn == "y" {
			gotOn, gotOff = stop.Y, stop.X
		}
		if gotOn != wantOn || gotOff != wantOff {
			t.Fatalf("stop %s mapping on=%q off=%q, want %q/%q", stop.Name, gotOn, gotOff, wantOn, wantOff)
		}
	}
}

func TestBuildReviewPacketRejectsInvalidRecallPairs(t *testing.T) {
	on, off := recallReports()
	recallOpts := ReviewOptions{Seed: 3, Axis: ReviewAxisRecall}
	if _, _, err := BuildReviewPacket(on, off, recallOpts); err != nil {
		t.Fatalf("valid recall pair rejected: %v", err)
	}
	if _, _, err := BuildReviewPacket(on, off, ReviewOptions{Seed: 3}); err == nil {
		t.Fatal("memory axis accepted a recall-only pair")
	}
	bothOn := off
	bothOn.Stops = append([]StopReport(nil), off.Stops...)
	bothOn.Stops[0].Recall.Enabled = true
	if _, _, err := BuildReviewPacket(on, bothOn, recallOpts); err == nil {
		t.Fatal("expected off-side recall rejection")
	}
	neitherOn := on
	neitherOn.Stops = append([]StopReport(nil), on.Stops...)
	neitherOn.Stops[0].Recall = RecallMetrics{Requested: "off"}
	if _, _, err := BuildReviewPacket(neitherOn, off, recallOpts); err == nil {
		t.Fatal("expected on-side recall rejection")
	}
	mixedMemory := off
	mixedMemory.Stops = append([]StopReport(nil), off.Stops...)
	mixedMemory.Stops[1].UseMemory = true
	if _, _, err := BuildReviewPacket(on, mixedMemory, recallOpts); err == nil {
		t.Fatal("expected memory setting mismatch rejection on recall axis")
	}
	if _, _, err := BuildReviewPacket(on, off, ReviewOptions{Seed: 3, Axis: "plan"}); err == nil {
		t.Fatal("expected unknown axis rejection")
	} else if !strings.Contains(err.Error(), "want memory or recall") {
		t.Fatalf("unhelpful axis error: %v", err)
	}
}

func TestAggregateReviewRecallAxisUsesRecallLabels(t *testing.T) {
	on, off := recallReports()
	packet, key, err := BuildReviewPacket(on, off, ReviewOptions{Seed: 5, Axis: ReviewAxisRecall})
	if err != nil {
		t.Fatalf("BuildReviewPacket: %v", err)
	}
	scorePacket(t, &packet, key)
	summary, err := AggregateReview(packet, key)
	if err != nil {
		t.Fatalf("AggregateReview: %v", err)
	}
	if summary.Axis != ReviewAxisRecall || !summary.Pass {
		t.Fatalf("summary = %+v", summary)
	}
	for i := range packet.Stops {
		packet.Stops[i].Score.Preference = oppositeSide(key.Stops[i].MemoryOn)
	}
	if key.Stops[0].MemoryOn == "x" {
		packet.Stops[0].Score.XUnauthorized = []string{"提前兑现埋设"}
	} else {
		packet.Stops[0].Score.YUnauthorized = []string{"提前兑现埋设"}
	}
	summary, err = AggregateReview(packet, key)
	if err != nil {
		t.Fatalf("AggregateReview failing pair: %v", err)
	}
	if summary.Pass || summary.OnUnauthorized != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	var labeled int
	for _, problem := range summary.Problems {
		if strings.HasPrefix(problem, "recall-on ") {
			labeled++
		}
		if strings.Contains(problem, "memory-on") {
			t.Fatalf("recall axis reported memory label: %q", problem)
		}
	}
	if labeled != 2 {
		t.Fatalf("problems = %+v", summary.Problems)
	}
}

func TestAggregateReviewDefaultsLegacyPacketToMemoryAxis(t *testing.T) {
	on, off := reviewReports()
	packet, key, err := BuildReviewPacket(on, off, ReviewOptions{Seed: 13})
	if err != nil {
		t.Fatalf("BuildReviewPacket: %v", err)
	}
	scorePacket(t, &packet, key)
	packet.Axis = ""
	key.Axis = ""
	summary, err := AggregateReview(packet, key)
	if err != nil {
		t.Fatalf("AggregateReview: %v", err)
	}
	if summary.Axis != ReviewAxisMemory || !summary.Pass {
		t.Fatalf("legacy summary = %+v", summary)
	}
	if _, err := AggregateReview(packet, ReviewKey{SchemaVersion: reviewSchemaVersion, CaseID: packet.CaseID, Seed: packet.Seed, Stops: []ReviewKeyStop{{Name: "early", MemoryOn: "x"}, {Name: "middle", MemoryOn: "y"}, {Name: "late", MemoryOn: "y"}}, Axis: "plan"}); err == nil {
		t.Fatal("expected unknown axis error from key")
	}
}

func oppositeSide(side string) string {
	if side == "x" {
		return "y"
	}
	return "x"
}
