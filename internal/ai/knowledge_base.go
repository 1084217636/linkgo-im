package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const defaultKnowledgeTopK = 3

type KnowledgeBase struct {
	documents []knowledgeDocument
}

type knowledgeDocument struct {
	Path    string
	Title   string
	Content string
}

func NewKnowledgeBase(paths []string) (*KnowledgeBase, error) {
	if len(paths) == 0 {
		paths = defaultKnowledgePaths()
	}

	kb := &KnowledgeBase{}
	var failed []string
	for _, rawPath := range paths {
		path := strings.TrimSpace(rawPath)
		if path == "" {
			continue
		}
		docs, err := loadKnowledgeDocuments(path)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		kb.documents = append(kb.documents, docs...)
	}

	if len(kb.documents) == 0 {
		if len(failed) == 0 {
			return kb, fmt.Errorf("ai knowledge base is empty")
		}
		return kb, fmt.Errorf("ai knowledge base is empty: %s", strings.Join(failed, "; "))
	}
	if len(failed) > 0 {
		return kb, fmt.Errorf("ai knowledge base loaded with partial failures: %s", strings.Join(failed, "; "))
	}
	return kb, nil
}

func defaultKnowledgePaths() []string {
	return []string{
		"docs/AI_FAQ.md",
		"README.md",
		"docs/CODE_MAP.md",
		"docs/CORE_LINKS.md",
		"docs/INTERVIEW_QA.md",
	}
}

func (kb *KnowledgeBase) Search(question string, limit int) []KnowledgeSource {
	if kb == nil || len(kb.documents) == 0 {
		return nil
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil
	}
	if limit <= 0 {
		limit = defaultKnowledgeTopK
	}
	terms := extractSearchTerms(question)
	if len(terms) == 0 {
		return nil
	}

	type scoredSource struct {
		source KnowledgeSource
	}
	scored := make([]scoredSource, 0, len(kb.documents))
	for _, doc := range kb.documents {
		score := scoreKnowledgeDocument(doc, question, terms)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredSource{
			source: KnowledgeSource{
				Path:    doc.Path,
				Title:   doc.Title,
				Snippet: buildSnippet(doc.Content, terms),
				Score:   score,
			},
		})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].source.Score != scored[j].source.Score {
			return scored[i].source.Score > scored[j].source.Score
		}
		if scored[i].source.Path != scored[j].source.Path {
			return scored[i].source.Path < scored[j].source.Path
		}
		return scored[i].source.Title < scored[j].source.Title
	})

	if limit > len(scored) {
		limit = len(scored)
	}
	results := make([]KnowledgeSource, 0, limit)
	for i := 0; i < limit; i++ {
		results = append(results, scored[i].source)
	}
	return results
}

func loadKnowledgeDocuments(path string) ([]knowledgeDocument, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body := strings.TrimSpace(string(content))
	if body == "" {
		return nil, fmt.Errorf("empty file")
	}
	if strings.EqualFold(filepath.Ext(path), ".md") {
		return splitMarkdownKnowledge(path, body), nil
	}
	return []knowledgeDocument{{
		Path:    path,
		Title:   filepath.Base(path),
		Content: body,
	}}, nil
}

func splitMarkdownKnowledge(path, body string) []knowledgeDocument {
	lines := strings.Split(body, "\n")
	currentTitle := filepath.Base(path)
	var current []string
	var docs []knowledgeDocument
	flush := func() {
		content := strings.TrimSpace(strings.Join(current, "\n"))
		if content == "" {
			return
		}
		docs = append(docs, knowledgeDocument{
			Path:    path,
			Title:   currentTitle,
			Content: content,
		})
		current = current[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			flush()
			currentTitle = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			continue
		}
		current = append(current, line)
	}
	flush()
	if len(docs) == 0 {
		docs = append(docs, knowledgeDocument{
			Path:    path,
			Title:   filepath.Base(path),
			Content: body,
		})
	}
	return docs
}

func scoreKnowledgeDocument(doc knowledgeDocument, question string, terms []string) int {
	title := strings.ToLower(doc.Title)
	path := strings.ToLower(doc.Path)
	content := strings.ToLower(doc.Content)
	fullQuestion := strings.ToLower(strings.TrimSpace(question))
	score := 0
	if fullQuestion != "" && strings.Contains(content, fullQuestion) {
		score += 12
	}
	for _, term := range terms {
		if term == "" {
			continue
		}
		if strings.Contains(title, term) {
			score += 8
		}
		if strings.Contains(path, term) {
			score += 5
		}
		score += strings.Count(content, term) * 2
	}
	return score
}

func buildSnippet(content string, terms []string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	lowerContent := strings.ToLower(content)
	for _, term := range terms {
		if term == "" {
			continue
		}
		idx := strings.Index(lowerContent, term)
		if idx < 0 {
			continue
		}
		prefixRunes := utf8.RuneCountInString(content[:idx])
		runes := []rune(content)
		start := prefixRunes - 40
		if start < 0 {
			start = 0
		}
		end := prefixRunes + 120
		if end > len(runes) {
			end = len(runes)
		}
		snippet := strings.TrimSpace(string(runes[start:end]))
		if start > 0 {
			snippet = "..." + snippet
		}
		if end < len(runes) {
			snippet += "..."
		}
		return snippet
	}
	return truncateRunes(content, 180)
}

func extractSearchTerms(question string) []string {
	question = strings.ToLower(strings.TrimSpace(question))
	if question == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var terms []string
	add := func(term string) {
		term = strings.TrimSpace(term)
		if term == "" {
			return
		}
		if _, ok := seen[term]; ok {
			return
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}

	for _, token := range splitSearchTokens(question) {
		add(token)
		runes := []rune(token)
		if len(runes) >= 2 && containsHan(token) {
			for i := 0; i < len(runes)-1; i++ {
				add(string(runes[i : i+2]))
			}
		}
	}
	return terms
}

func splitSearchTokens(question string) []string {
	var tokens []string
	var current []rune
	currentClass := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		token := strings.TrimSpace(string(current))
		if utf8.RuneCountInString(token) > 1 || containsHan(token) {
			tokens = append(tokens, token)
		}
		current = current[:0]
		currentClass = 0
	}
	for _, r := range question {
		class := searchRuneClass(r)
		if class == 0 {
			flush()
			continue
		}
		if currentClass != 0 && class != currentClass {
			flush()
		}
		current = append(current, r)
		currentClass = class
	}
	flush()
	return tokens
}

func searchRuneClass(r rune) int {
	switch {
	case unicode.Is(unicode.Han, r):
		return 1
	case unicode.IsLetter(r), unicode.IsDigit(r):
		return 2
	default:
		return 0
	}
}

func containsHan(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
