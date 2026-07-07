package ai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKnowledgeBaseSearch(t *testing.T) {
	dir := t.TempDir()
	docPath := filepath.Join(dir, "faq.md")
	content := `# 群聊为什么用 Kafka

群聊扩散是高扇出操作，如果 Logic 同步 for 循环发送，会直接拖慢发送链路。

# ACK 做什么

ACK 是投递确认，不是已读回执。`
	if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	kb, err := NewKnowledgeBase([]string{docPath})
	if err != nil {
		t.Fatalf("NewKnowledgeBase error = %v", err)
	}
	results := kb.Search("群聊为什么用 Kafka", 2)
	if len(results) == 0 {
		t.Fatalf("expected search results")
	}
	if results[0].Title != "群聊为什么用 Kafka" {
		t.Fatalf("unexpected title: %+v", results[0])
	}
	if results[0].Score <= 0 {
		t.Fatalf("expected positive score: %+v", results[0])
	}
}
