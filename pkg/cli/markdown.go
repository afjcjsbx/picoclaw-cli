package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
)

func renderAssistantMarkdown(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return styleMuted.Render("(empty response)")
	}

	extensions := (parser.CommonExtensions | parser.NoEmptyLineBeforeBlock) &^ parser.DefinitionLists
	p := parser.NewWithExtensions(extensions)
	doc := markdown.Parse([]byte(trimmed), p)
	if doc == nil {
		return trimmed
	}

	blocks := renderMarkdownBlocks(doc.GetChildren())
	if len(blocks) == 0 {
		return trimmed
	}

	return strings.Join(blocks, "\n\n")
}

func renderMarkdownBlocks(nodes []ast.Node) []string {
	blocks := make([]string, 0, len(nodes))
	for _, node := range nodes {
		block := strings.TrimRight(renderMarkdownBlock(node), "\n")
		if strings.TrimSpace(stripControlWhitespace(block)) == "" {
			continue
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func renderMarkdownBlock(node ast.Node) string {
	switch n := node.(type) {
	case *ast.Paragraph:
		return strings.TrimSpace(renderMarkdownInlineChildren(n.GetChildren()))
	case *ast.Heading:
		text := strings.TrimSpace(renderMarkdownInlineChildren(n.GetChildren()))
		if text == "" {
			return ""
		}
		switch {
		case n.Level <= 1:
			return styleMarkdownH1.Render(text)
		case n.Level == 2:
			return styleMarkdownH2.Render("## " + text)
		default:
			level := n.Level
			if level > 6 {
				level = 6
			}
			return styleMarkdownH3.Render(strings.Repeat("#", level) + " " + text)
		}
	case *ast.BlockQuote:
		inner := strings.Join(renderMarkdownBlocks(n.GetChildren()), "\n\n")
		if strings.TrimSpace(inner) == "" {
			return ""
		}
		prefix := styleMarkdownQuote.Render("│ ")
		return prefixMultiline(inner, prefix, prefix)
	case *ast.List:
		return renderMarkdownList(n)
	case *ast.CodeBlock:
		code := strings.TrimRight(string(n.Literal), "\n")
		if strings.TrimSpace(code) == "" {
			return ""
		}
		return renderCodeFence(code, strings.TrimSpace(string(n.Info)))
	case *ast.HorizontalRule:
		return styleMarkdownRule.Render(strings.Repeat("─", 36))
	case *ast.Table:
		return renderMarkdownTable(n)
	case *ast.HTMLBlock:
		return strings.TrimSpace(string(n.Literal))
	case *ast.Image:
		alt := strings.TrimSpace(renderMarkdownInlineChildren(n.GetChildren()))
		if alt == "" {
			alt = "image"
		}
		dest := strings.TrimSpace(string(n.Destination))
		if dest == "" {
			return styleMuted.Render("[image] " + alt)
		}
		return styleMuted.Render("[image] " + alt + " (" + dest + ")")
	default:
		if leaf := node.AsLeaf(); leaf != nil {
			if literal := strings.TrimSpace(string(leaf.Literal)); literal != "" {
				return literal
			}
		}
		return strings.TrimSpace(renderMarkdownInlineChildren(node.GetChildren()))
	}
}

func renderMarkdownList(list *ast.List) string {
	ordered := list.ListFlags&ast.ListTypeOrdered != 0
	index := list.Start
	if index <= 0 {
		index = 1
	}

	items := make([]string, 0, len(list.GetChildren()))
	for _, child := range list.GetChildren() {
		item, ok := child.(*ast.ListItem)
		if !ok {
			if block := renderMarkdownBlock(child); strings.TrimSpace(block) != "" {
				items = append(items, block)
			}
			continue
		}

		marker := "•"
		if ordered {
			marker = fmt.Sprintf("%d.", index)
			index++
		}
		items = append(items, renderMarkdownListItem(item, marker))
	}

	return strings.Join(items, "\n")
}

func renderMarkdownListItem(item *ast.ListItem, marker string) string {
	blocks := renderMarkdownBlocks(item.GetChildren())
	if len(blocks) == 0 {
		return styleMarkdownBullet.Render(marker)
	}

	parts := make([]string, 0, len(blocks))
	for idx, block := range blocks {
		if idx > 0 {
			parts = append(parts, "")
		}
		parts = append(parts, strings.Split(block, "\n")...)
	}

	firstPrefix := styleMarkdownBullet.Render(marker) + " "
	restPrefix := strings.Repeat(" ", lipgloss.Width(marker)+1)
	return prefixMultiline(strings.Join(parts, "\n"), firstPrefix, restPrefix)
}

func renderMarkdownInlineChildren(nodes []ast.Node) string {
	var b strings.Builder
	for _, node := range nodes {
		b.WriteString(renderMarkdownInline(node))
	}
	return b.String()
}

func renderMarkdownInline(node ast.Node) string {
	switch n := node.(type) {
	case *ast.Text:
		return string(n.Literal)
	case *ast.Code:
		return styleMarkdownCode.Render(string(n.Literal))
	case *ast.Emph:
		return lipgloss.NewStyle().Italic(true).Render(renderMarkdownInlineChildren(n.GetChildren()))
	case *ast.Strong:
		return lipgloss.NewStyle().Bold(true).Render(renderMarkdownInlineChildren(n.GetChildren()))
	case *ast.Del:
		return lipgloss.NewStyle().Strikethrough(true).Render(renderMarkdownInlineChildren(n.GetChildren()))
	case *ast.Link:
		label := strings.TrimSpace(renderMarkdownInlineChildren(n.GetChildren()))
		dest := strings.TrimSpace(string(n.Destination))
		switch {
		case label == "" && dest == "":
			return ""
		case label == "":
			return styleMarkdownLink.Render(dest)
		case dest == "" || label == dest:
			return styleMarkdownLink.Render(label)
		default:
			return styleMarkdownLink.Render(label) + styleMuted.Render(" ("+dest+")")
		}
	case *ast.Image:
		alt := strings.TrimSpace(renderMarkdownInlineChildren(n.GetChildren()))
		if alt == "" {
			alt = "image"
		}
		dest := strings.TrimSpace(string(n.Destination))
		if dest == "" {
			return styleMuted.Render("[image] " + alt)
		}
		return styleMuted.Render("[image] " + alt + " (" + dest + ")")
	case *ast.Softbreak, *ast.NonBlockingSpace:
		return " "
	case *ast.Hardbreak:
		return "\n"
	case *ast.HTMLSpan:
		return string(n.Literal)
	default:
		if leaf := node.AsLeaf(); leaf != nil && len(leaf.Literal) > 0 {
			return string(leaf.Literal)
		}
		return renderMarkdownInlineChildren(node.GetChildren())
	}
}

func renderMarkdownTable(table *ast.Table) string {
	rows := make([][]string, 0)
	headerRows := 0

	for _, child := range table.GetChildren() {
		switch section := child.(type) {
		case *ast.TableHeader:
			for _, rowNode := range section.GetChildren() {
				if row := renderMarkdownTableRow(rowNode); len(row) > 0 {
					rows = append(rows, row)
					headerRows++
				}
			}
		case *ast.TableBody:
			for _, rowNode := range section.GetChildren() {
				if row := renderMarkdownTableRow(rowNode); len(row) > 0 {
					rows = append(rows, row)
				}
			}
		case *ast.TableRow:
			if row := renderMarkdownTableRow(section); len(row) > 0 {
				rows = append(rows, row)
			}
		}
	}

	if len(rows) == 0 {
		return ""
	}
	if headerRows == 0 && len(rows) > 1 {
		headerRows = 1
	}

	widths := make([]int, 0)
	for _, row := range rows {
		for len(widths) < len(row) {
			widths = append(widths, 0)
		}
		for i, cell := range row {
			if width := lipgloss.Width(cell); width > widths[i] {
				widths[i] = width
			}
		}
	}

	lines := make([]string, 0, len(rows)+1)
	for idx, row := range rows {
		lines = append(lines, renderMarkdownTableLine(row, widths))
		if headerRows > 0 && idx == headerRows-1 {
			lines = append(lines, renderMarkdownTableDivider(widths))
		}
	}

	return strings.Join(lines, "\n")
}

func renderMarkdownTableRow(node ast.Node) []string {
	rowNode, ok := node.(*ast.TableRow)
	if !ok {
		return nil
	}

	cells := make([]string, 0, len(rowNode.GetChildren()))
	for _, child := range rowNode.GetChildren() {
		cell, ok := child.(*ast.TableCell)
		if !ok {
			continue
		}
		cells = append(cells, strings.TrimSpace(renderMarkdownPlainText(cell)))
	}
	return cells
}

func renderMarkdownTableLine(row []string, widths []int) string {
	cells := make([]string, len(widths))
	for i := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		cells[i] = padRightVisible(cell, widths[i])
	}
	return "| " + strings.Join(cells, " | ") + " |"
}

func renderMarkdownTableDivider(widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		if width < 3 {
			width = 3
		}
		parts[i] = strings.Repeat("-", width)
	}
	return "|-" + strings.Join(parts, "-|-") + "-|"
}

func renderMarkdownPlainText(node ast.Node) string {
	switch n := node.(type) {
	case *ast.Text:
		return string(n.Literal)
	case *ast.Code:
		return string(n.Literal)
	case *ast.Softbreak, *ast.Hardbreak, *ast.NonBlockingSpace:
		return " "
	case *ast.Link:
		label := strings.TrimSpace(renderMarkdownPlainTextChildren(n.GetChildren()))
		dest := strings.TrimSpace(string(n.Destination))
		switch {
		case label == "" && dest == "":
			return ""
		case label == "":
			return dest
		case dest == "" || label == dest:
			return label
		default:
			return label + " (" + dest + ")"
		}
	default:
		if leaf := node.AsLeaf(); leaf != nil && len(leaf.Literal) > 0 {
			return string(leaf.Literal)
		}
		return renderMarkdownPlainTextChildren(node.GetChildren())
	}
}

func renderMarkdownPlainTextChildren(nodes []ast.Node) string {
	var b strings.Builder
	for _, node := range nodes {
		b.WriteString(renderMarkdownPlainText(node))
	}
	return b.String()
}
