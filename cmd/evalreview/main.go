// Command evalreview 把能力轴开/关的 live 报告转成盲评包，并聚合人评结果。
//
// 生成盲评包（-axis memory 比较记忆开关，-axis recall 比较 R4 历史召回开关）：
//
//	go run ./cmd/evalreview -axis memory -on on.json -off off.json -seed 20261023 \
//	    -packet packet.json -key key.json -markdown packet.md
//
// 聚合评分（packet.json 的 score 字段需由评审填写，key 不应交给评审）：
//
//	go run ./cmd/evalreview -score packet.json -key key.json -summary summary.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"novelagent/internal/eval"
)

func main() {
	axis := flag.String("axis", eval.ReviewAxisMemory, "对照能力轴：memory 或 recall")
	onPath := flag.String("on", "", "开启该能力的 live 报告")
	offPath := flag.String("off", "", "关闭该能力的 live 报告")
	seed := flag.Int64("seed", 20261023, "X/Y 随机分配种子（需记录，供复核）")
	packetPath := flag.String("packet", "", "盲评包输出路径（生成模式必填）")
	keyPath := flag.String("key", "", "X/Y 映射输出路径（生成与评分模式必填）")
	markdownPath := flag.String("markdown", "", "人读版盲评表输出路径（可选）")
	scorePath := flag.String("score", "", "已评分的盲评包路径（评分模式）")
	summaryPath := flag.String("summary", "", "评分汇总输出路径（缺省打印 JSON）")
	flag.Parse()

	if *scorePath != "" {
		score(*scorePath, *keyPath, *summaryPath)
		return
	}
	if *onPath == "" || *offPath == "" || *packetPath == "" || *keyPath == "" {
		fail("生成模式需要 -on、-off、-packet、-key")
	}
	on := loadReport(*onPath)
	off := loadReport(*offPath)
	packet, key, err := eval.BuildReviewPacket(on, off, eval.ReviewOptions{Seed: *seed, Axis: *axis})
	if err != nil {
		fail(err.Error())
	}
	writeJSON(*packetPath, packet)
	writeJSON(*keyPath, key)
	if *markdownPath != "" {
		if err := os.WriteFile(*markdownPath, []byte(eval.RenderReviewMarkdown(packet)), 0o644); err != nil {
			fail("写盲评表失败: " + err.Error())
		}
	}
	fmt.Printf("packet: %s\nkey: %s\naxis: %s\nstops: %d\n", *packetPath, *keyPath, packet.Axis, len(packet.Stops))
}

func score(packetPath, keyPath, summaryPath string) {
	if keyPath == "" {
		fail("评分模式需要 -key")
	}
	var packet eval.ReviewPacket
	loadJSON(packetPath, &packet)
	var key eval.ReviewKey
	loadJSON(keyPath, &key)
	summary, err := eval.AggregateReview(packet, key)
	if err != nil {
		fail(err.Error())
	}
	if summaryPath != "" {
		writeJSON(summaryPath, summary)
		fmt.Printf("summary: %s\n", summaryPath)
	} else {
		data, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			fail("编码汇总失败: " + err.Error())
		}
		fmt.Println(string(data))
	}
	fmt.Printf("pass=%t axis=%s stops=%d onWins=%d offWins=%d ties=%d onCoverage=%.2f offCoverage=%.2f onUnauthorized=%d offUnauthorized=%d\n",
		summary.Pass, summary.Axis, summary.Stops, summary.OnWins, summary.OffWins, summary.Ties,
		summary.OnCoverage, summary.OffCoverage, summary.OnUnauthorized, summary.OffUnauthorized)
	for _, problem := range summary.Problems {
		fmt.Println("problem:", problem)
	}
	if !summary.Pass {
		os.Exit(4)
	}
}

func loadReport(path string) eval.Report {
	var report eval.Report
	loadJSON(path, &report)
	return report
}

func loadJSON(path string, value any) {
	data, err := os.ReadFile(path)
	if err != nil {
		fail("读取失败 " + path + ": " + err.Error())
	}
	if err := json.Unmarshal(data, value); err != nil {
		fail("解析失败 " + path + ": " + err.Error())
	}
}

func writeJSON(path string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fail("编码失败: " + err.Error())
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		fail("写入失败 " + path + ": " + err.Error())
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "evalreview:", message)
	os.Exit(1)
}
