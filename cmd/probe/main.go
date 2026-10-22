// Command probe 是独立的调试探针：读取指定数据目录的 knowledge.json，
// 调用 OpenAI 兼容接口生成「核心设定与当前进展」总览，用于对照主程序
// synthesis 流程的输出效果。
//
// 用法：
//
//	go run ./cmd/probe [-data <dataDir>] [-base-url <url>] [-model <name>]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"novelagent/internal/llm"
)

func main() {
	dataDir := flag.String("data", ".tmp_e2e/data", "知识库所在数据目录")
	baseURL := flag.String("base-url", "http://127.0.0.1:7897", "OpenAI 兼容接口地址")
	model := flag.String("model", "gpt-5.6-sol", "模型名称")
	flag.Parse()

	content, err := os.ReadFile(*dataDir + "/knowledge.json")
	if err != nil {
		panic(err)
	}
	var knowledge map[string]interface{}
	if err := json.Unmarshal(content, &knowledge); err != nil {
		panic(err)
	}
	client := llm.NewClient(*baseURL, "", *model)
	messages := []llm.Message{
		{Role: "system", Content: "你是一个中文小说分析师。你接收作品标题、章节总结集、角色卡集合、世界观条目集，产出精炼的【核心设定与当前进展】总览。不要罗列列表，要用连贯段落；忠于已知事实，允许合理推测但未确认信息标注存疑；不超过 400 字。"},
		{Role: "user", Content: fmt.Sprintf("作品：%v\n\n章节总结（按索引）：%s\n\n角色状态：%s\n\n世界设定：%s",
			knowledge["title"],
			jsonOrEmpty(knowledge, "summaries"),
			jsonOrEmpty(knowledge, "characters"),
			jsonOrEmpty(knowledge, "world")),
		},
	}
	resp, err := client.CompleteChat(context.Background(), messages, 0.3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "call failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(resp)
}

func jsonOrEmpty(m map[string]interface{}, k string) string {
	v, ok := m[k]
	if !ok {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}
