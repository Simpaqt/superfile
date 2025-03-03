package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Plugin is the exported plugin instance
var Plugin = &ZoxidePlugin{}

// ZoxidePlugin implements zoxide history functionality for superfile
type ZoxidePlugin struct {
	emptyFlag map[string]bool // Cache for directories with no history
	modal     *zoxideModal
}

// Modal for displaying zoxide history with search
type zoxideModal struct {
	open      bool
	width     int
	height    int
	cursor    int
	search    textinput.Model
	entries   []string
	allItems  []string
	render    int
	basePath  string
	searching bool
}

// Init initializes the plugin
func (z *ZoxidePlugin) Init() error {
	// Initialize the state
	z.emptyFlag = make(map[string]bool)
	z.modal = &zoxideModal{
		open:      false,
		width:     60,
		height:    20,
		cursor:    0,
		render:    0,
		entries:   []string{},
		allItems:  []string{},
		searching: false,
	}

	// Set up the search input
	search := textinput.New()
	search.Placeholder = "Search zoxide history..."
	search.CharLimit = 100
	search.Width = 40
	z.modal.search = search

	// Register the key binding to show zoxide history
	// We're using z.Show as the callback function
	return nil
}

// IsEmptyHistory checks if there's any zoxide history excluding the current directory
func (z *ZoxidePlugin) IsEmptyHistory(cwd string) bool {
	// Check cache first
	if val, ok := z.emptyFlag[cwd]; ok {
		return val
	}

	cmd := exec.Command("zoxide", "query", "-l", "--exclude", cwd)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()

	if err != nil || out.Len() == 0 {
		z.emptyFlag[cwd] = true
		return true
	}

	z.emptyFlag[cwd] = false
	return false
}

// GetFZFOptions returns options for fzf consistent with the Yazi implementation
func (z *ZoxidePlugin) GetFZFOptions() string {
	default_opts := []string{
		"--exact",
		"--no-sort",
		"--bind=ctrl-z:ignore,btab:up,tab:down",
		"--cycle",
		"--keep-right",
		"--layout=reverse",
		"--height=100%",
		"--border",
		"--scrollbar=▌",
		"--info=inline",
		"--tabstop=1",
		"--exit-0",
	}

	// Add OS-specific options
	if runtime.GOOS != "windows" {
		default_opts = append(default_opts, "--preview-window=down,30%,sharp")
		if runtime.GOOS == "linux" {
			default_opts = append(default_opts, `--preview='\command -p ls -Cp --color=always --group-directories-first {2..}'`)
		} else {
			default_opts = append(default_opts, `--preview='\command -p ls -Cp {2..}'`)
		}
	}

	// Combine with environment variables if they exist
	fzfDefaultOpts := os.Getenv("FZF_DEFAULT_OPTS")
	superfileZoxideOpts := os.Getenv("SUPERFILE_ZOXIDE_OPTS")

	return strings.Join([]string{
		fzfDefaultOpts,
		strings.Join(default_opts, " "),
		superfileZoxideOpts,
	}, " ")
}

// Show opens the zoxide history modal
// This function should be bound to a key in superfile
func (z *ZoxidePlugin) Show(baseDir string) {
	// Check if we have any history
	if z.IsEmptyHistory(baseDir) {
		// Show a notification that there's no history
		// We can use bubbletea commands to show a notification
		return
	}

	// Get zoxide history
	cmd := exec.Command("zoxide", "query", "-l", "--exclude", baseDir)
	output, err := cmd.Output()
	if err != nil {
		slog.Error("Error getting zoxide history", "error", err)
		return
	}

	// Process the output
	history := strings.TrimSpace(string(output))
	entries := strings.Split(history, "\n")

	// Update the modal
	z.modal.basePath = baseDir
	z.modal.entries = entries
	z.modal.allItems = entries
	z.modal.cursor = 0
	z.modal.render = 0
	z.modal.open = true
	z.modal.search.SetValue("")
	z.modal.searching = false
}

// HandleKeypress handles key presses when the modal is open
func (z *ZoxidePlugin) HandleKeypress(msg tea.KeyMsg) (bool, string) {
	// If modal isn't open, do nothing
	if !z.modal.open {
		return false, ""
	}

	// Check if we're in search mode
	if z.modal.searching {
		switch msg.String() {
		case "enter":
			// Apply the search filter
			z.applySearch()
			z.modal.searching = false
			return true, ""
		case "esc":
			// Cancel search
			z.modal.search.SetValue("")
			z.modal.entries = z.modal.allItems
			z.modal.searching = false
			return true, ""
		default:
			// Handle typing in the search field
			var cmd tea.Cmd
			z.modal.search, cmd = z.modal.search.Update(msg)
			if cmd != nil {
				// We would need to send the command somewhere for it to be processed
			}
			return true, ""
		}
	}

	// Normal modal navigation
	switch msg.String() {
	case "q", "esc":
		z.modal.open = false
		return true, ""
	case "enter":
		// If we have selections and cursor is valid, return the selected directory
		if len(z.modal.entries) > 0 && z.modal.cursor < len(z.modal.entries) {
			selectedDir := z.modal.entries[z.modal.cursor]
			z.modal.open = false
			return true, selectedDir
		}
		return true, ""
	case "up", "k":
		if z.modal.cursor > 0 {
			z.modal.cursor--
			if z.modal.cursor < z.modal.render {
				z.modal.render = z.modal.cursor
			}
		} else {
			// Wrap around to the bottom
			z.modal.cursor = len(z.modal.entries) - 1
			z.modal.render = len(z.modal.entries) - z.modal.height
			if z.modal.render < 0 {
				z.modal.render = 0
			}
		}
		return true, ""
	case "down", "j":
		if z.modal.cursor < len(z.modal.entries)-1 {
			z.modal.cursor++
			if z.modal.cursor >= z.modal.render+z.modal.height {
				z.modal.render++
			}
		} else {
			// Wrap around to the top
			z.modal.cursor = 0
			z.modal.render = 0
		}
		return true, ""
	case "/":
		// Activate search mode
		z.modal.search.Focus()
		z.modal.searching = true
		return true, ""
	}

	return true, ""
}

// Apply the current search filter to the entries
func (z *ZoxidePlugin) applySearch() {
	searchTerm := strings.ToLower(z.modal.search.Value())
	if searchTerm == "" {
		z.modal.entries = z.modal.allItems
		return
	}

	// Filter entries by search term
	filtered := []string{}
	for _, entry := range z.modal.allItems {
		if strings.Contains(strings.ToLower(entry), searchTerm) {
			filtered = append(filtered, entry)
		}
	}

	z.modal.entries = filtered
	z.modal.cursor = 0
	z.modal.render = 0
}

// RenderModal renders the zoxide history modal
func (z *ZoxidePlugin) RenderModal() string {
	if !z.modal.open {
		return ""
	}

	// Build the modal content
	var content strings.Builder

	// Add the search bar at the top
	if z.modal.searching {
		content.WriteString(z.modal.search.View() + "\n\n")
	} else {
		content.WriteString("Search: " + z.modal.search.Value() + "\n\n")
	}

	// Add the entries
	endIdx := z.modal.render + z.modal.height - 4 // Account for header and footer
	if endIdx > len(z.modal.entries) {
		endIdx = len(z.modal.entries)
	}

	for i := z.modal.render; i < endIdx; i++ {
		entry := z.modal.entries[i]
		
		// Highlight the cursor position
		if i == z.modal.cursor {
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
		BorderForeground(lipgloss.Color("#874BFD")).
		Padding(1, 2).
		Width(z.modal.width).
		Height(z.modal.height)

	return style.Render("Zoxide History\n\n" + content.String())
}

// UpdateModalSize updates the modal dimensions based on terminal size
func (z *ZoxidePlugin) UpdateModalSize(width, height int) {
	// Set reasonable dimensions based on terminal size
	z.modal.width = width * 2 / 3
	if z.modal.width > 100 {
		z.modal.width = 100
	} else if z.modal.width < 40 {
		z.modal.width = 40
	}

	z.modal.height = height * 2 / 3
	if z.modal.height > 30 {
		z.modal.height = 30
	} else if z.modal.height < 10 {
		z.modal.height = 10
	}
}

// IsOpen returns whether the modal is currently open
func (z *ZoxidePlugin) IsOpen() bool {
	return z.modal.open
}

// Close closes the modal
func (z *ZoxidePlugin) Close() {
	z.modal.open = false
}

func main() {
	// This function is called when loading the plugin
	Plugin.Init()
}
