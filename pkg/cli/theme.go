package cli

import "github.com/charmbracelet/lipgloss"

var (
	typingSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	colorUser           = lipgloss.Color("#14746F")
	colorAssistant      = lipgloss.Color("#3056D3")
	colorTool           = lipgloss.Color("#B45309")
	colorThought        = lipgloss.Color("#7C3AED")
	colorStatus         = lipgloss.Color("#6B7280")
	colorDanger         = lipgloss.Color("#D14343")
	colorBorder         = lipgloss.Color("#4B5563")
	colorTextMuted      = lipgloss.Color("#94A3B8")
	stylePrompt         = lipgloss.NewStyle().Foreground(colorUser).Bold(true)
	styleStatus         = lipgloss.NewStyle().Foreground(colorStatus)
	styleError          = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
	styleToolInline     = lipgloss.NewStyle().Foreground(colorTool).Bold(true)
	styleBody           = lipgloss.NewStyle()
	styleRule           = lipgloss.NewStyle().Foreground(colorBorder)
	styleMuted          = lipgloss.NewStyle().Foreground(colorTextMuted)
	styleMarkdownH1     = lipgloss.NewStyle().Foreground(colorAssistant).Bold(true).Underline(true)
	styleMarkdownH2     = lipgloss.NewStyle().Foreground(colorAssistant).Bold(true)
	styleMarkdownH3     = lipgloss.NewStyle().Foreground(colorAssistant)
	styleMarkdownCode   = lipgloss.NewStyle().Foreground(lipgloss.Color("#155E75")).Background(lipgloss.Color("#ECFEFF")).Bold(true)
	styleMarkdownLink   = lipgloss.NewStyle().Foreground(lipgloss.Color("#0F766E")).Underline(true)
	styleMarkdownQuote  = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B"))
	styleMarkdownRule   = lipgloss.NewStyle().Foreground(colorBorder)
	styleMarkdownBullet = lipgloss.NewStyle().Foreground(colorAssistant).Bold(true)
	styleSelection      = lipgloss.NewStyle().Foreground(lipgloss.Color("#111827")).Background(lipgloss.Color("#C7D2FE"))
)
