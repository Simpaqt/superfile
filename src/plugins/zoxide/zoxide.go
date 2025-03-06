package zoxide

import (
	"bytes"
	"os/exec"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/reinhrst/fzf-lib"
)

// ZoxidePlugin implements a zoxide integration for superfile
type ZoxidePlugin struct {
	Modal      *ZoxideModal
	isOpen     bool
	initialized bool
	mu         sync.Mutex
}

// ZoxideModal represents the popup showing zoxide history
type ZoxideModal struct {
	Width       int
	Height      int
	Cursor      int
	RenderIndex int
	Entries     []string
	AllEntries  []string
	SearchBar   textinput.Model
	SearchMode  bool
}

// Message types
type ZoxideMsg struct {
	Entries []string
}

type DirSelectedMsg struct {
	Path string
}

// Init initializes the zoxide plugin
func (z *ZoxidePlugin) Init() error {
	if z.initialized {
		return nil
	}

	z.Modal = &ZoxideModal{
		Width:       60,
		Height:      20,
		Cursor:      0,
		RenderIndex: 0,
		Entries:     []string{},
		AllEntries:  []string{},
		SearchMode:  false,
	}

	// Initialize search bar
	searchBar := textinput.New()
	searchBar.Placeholder = "Search zoxide history..."
	searchBar.CharLimit = 100
	searchBar.Width = z.Modal.Width - 4
	z.Modal.SearchBar = searchBar

	z.initialized = true
	return nil
}

// IsOpen returns whether the modal is currently open
func (z *ZoxidePlugin) IsOpen() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.isOpen
}

// Open opens the zoxide history modal
func (z *ZoxidePlugin) Open(excludePath string) tea.Cmd {
	z.mu.Lock()
	defer z.mu.Unlock()

	if z.isOpen {
		return nil
	}

	if !z.initialized {
		z.Init()
	}

	z.isOpen = true
	z.Modal.Cursor = 0
	z.Modal.RenderIndex = 0
	z.Modal.SearchBar.SetValue("")
	z.Modal.SearchMode = false

	return func() tea.Msg {
		entries, err := z.getZoxideHistory(excludePath)
		if err != nil || len(entries) == 0 {
			z.isOpen = false
			return nil
		}
		
		z.Modal.Entries = entries
		z.Modal.AllEntries = entries
		return ZoxideMsg{Entries: entries}
	}
}

// Close closes the zoxide modal
func (z *ZoxidePlugin) Close() {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.isOpen = false
}

// getZoxideHistory fetches the zoxide history excluding the current path
func (z *ZoxidePlugin) getZoxideHistory(excludePath string) ([]string, error) {
	cmd := exec.Command("zoxide", "query", "-l", "--exclude", excludePath)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	if out.Len() == 0 {
		return nil, nil
	}

	entries := strings.Split(strings.TrimSpace(out.String()), "\n")
	return entries, nil
}

// Update handles input events for the modal
func (z *ZoxidePlugin) Update(msg tea.Msg) (tea.Cmd, string) {
	if !z.isOpen {
		return nil, ""
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return z.handleKeypress(msg)
	}

	return nil, ""
}

// handleKeypress processes key input when the modal is open
func (z *ZoxidePlugin) handleKeypress(msg tea.KeyMsg) (tea.Cmd, string) {
	if z.Modal.SearchMode {
		switch msg.String() {
		case "enter":
			z.applySearch()
			z.Modal.SearchMode = false
			return nil, ""
		case "esc":
			z.Modal.SearchBar.SetValue("")
			z.Modal.Entries = z.Modal.AllEntries
			z.Modal.SearchMode = false
			return nil, ""
		default:
			var cmd tea.Cmd
			z.Modal.SearchBar, cmd = z.Modal.SearchBar.Update(msg)
			return cmd, ""
		}
	}

	switch msg.String() {
	case "q", "esc":
		z.Close()
		return nil, ""
	case "enter":
		if len(z.Modal.Entries) > 0 && z.Modal.Cursor < len(z.Modal.Entries) {
			selectedDir := z.Modal.Entries[z.Modal.Cursor]
			z.Close()
			return nil, selectedDir
		}
		return nil, ""
	case "up", "k":
		if z.Modal.Cursor > 0 {
			z.Modal.Cursor--
			if z.Modal.Cursor < z.Modal.RenderIndex {
				z.Modal.RenderIndex = z.Modal.Cursor
			}
		} else {
			// Wrap around to the bottom
			z.Modal.Cursor = len(z.Modal.Entries) - 1
			z.Modal.RenderIndex = maxInt(0, len(z.Modal.Entries) - z.Modal.Height + 4)
		}
		return nil, ""
	case "down", "j":
		if z.Modal.Cursor < len(z.Modal.Entries)-1 {
			z.Modal.Cursor++
			if z.Modal.Cursor >= z.Modal.RenderIndex+z.Modal.Height-4 {
				z.Modal.RenderIndex++
			}
		} else {
			// Wrap around to the top
			z.Modal.Cursor = 0
			z.Modal.RenderIndex = 0
		}
		return nil, ""
	case "/":
		// Activate search mode
		z.Modal.SearchBar.Focus()
		z.Modal.SearchMode = true
		return nil, ""
	}

	return nil, ""
}

// applySearch filters entries based on the search term
func (z *ZoxidePlugin) applySearch() {
	searchTerm := strings.ToLower(z.Modal.SearchBar.Value())
	if searchTerm == "" {
		z.Modal.Entries = z.Modal.AllEntries
		z.Modal.Cursor = 0
		z.Modal.RenderIndex = 0
		return
	}

	// Use fzf-lib for fuzzy search
	fzfSearcher := fzf.New(z.Modal.AllEntries, fzf.DefaultOptions())
	fzfSearcher.Search(searchTerm)
	results := <-fzfSearcher.GetResultChannel()
	fzfSearcher.End()

	if len(results.Matches) == 0 {
		// No results, don't change anything except cursor position
		z.Modal.Cursor = 0
		z.Modal.RenderIndex = 0
		return
	}

	// Extract the matched entries
	filtered := make([]string, len(results.Matches))
	for i, match := range results.Matches {
		filtered[i] = match.Key
	}

	z.Modal.Entries = filtered
	z.Modal.Cursor = 0
	z.Modal.RenderIndex = 0
}

// UpdateModalSize updates the modal dimensions based on terminal size
func (z *ZoxidePlugin) UpdateModalSize(width, height int) {
	// Set reasonable dimensions based on terminal size
	z.Modal.Width = minInt(maxInt(width*2/3, 40), 100)
	z.Modal.Height = minInt(maxInt(height*2/3, 10), 30)
	z.Modal.SearchBar.Width = z.Modal.Width - 4
}

// View renders the zoxide modal
func (z *ZoxidePlugin) View() string {
	if !z.isOpen || z.Modal == nil {
		return ""
	}

	var content strings.Builder

	// Add the search bar at the top
	if z.Modal.SearchMode {
		content.WriteString(z.Modal.SearchBar.View() + "\n\n")
	} else {
		content.WriteString("Search: " + z.Modal.SearchBar.Value() + "\n\n")
	}

	// Add the entries
	endIdx := z.Modal.RenderIndex + z.Modal.Height - 4 // Account for header and footer
	if endIdx > len(z.Modal.Entries) {
		endIdx = len(z.Modal.Entries)
	}

	for i := z.Modal.RenderIndex; i < endIdx; i++ {
		entry := z.Modal.Entries[i]
		
		// Highlight the cursor position
		if i == z.Modal.Cursor {
			content.WriteString("> " + entry + "\n")
		} else {
			content.WriteString("  " + entry + "\n")
		}
	}

	// Add help text at the bottom
	content.WriteString("\nEnter: select, Esc: cancel, /: search")

	// Create a styled modal
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#b4befe")).
		Padding(1, 2).
		Width(z.Modal.Width).
		Height(z.Modal.Height)

	return style.Render("Zoxide History\n\n" + content.String())
}

// Helper functions
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
