package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Memo ------------------------------------------------------------------------

type Memo struct {
	Content  string    `json:"content"`
	Created  time.Time `json:"created"`
	Modified time.Time `json:"modified"`
}

// Slice for JSON encoding/decoding.
type items []Memo

// Alias for Memo for the list.Item interface.
type item Memo

// FilterValue implements the list.Item interface.
func (i item) FilterValue() string {
	return i.Content
}

// Global Constants and Styling ------------------------------------------------

const (
	listHeight    = 10 // Increased height for better view
	maxWidth      = 80 // Max width for the entire view
	memosFileName = ".memos.json"
	accentColor   = lipgloss.Color("214") // Yellow/Orange for borders and highlights
)

var (
	// Container style for the entire main area (list or textarea)
	containerStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), true).
			BorderForeground(accentColor).
			Width(maxWidth).
			Padding(0, 1)

	// List item styles
	titleStyle        = lipgloss.NewStyle().MarginLeft(2).Foreground(accentColor).Bold(true)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(2)
	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(0).
				Foreground(accentColor).
				Bold(true)

	// Textarea styles
	textareaStyle = lipgloss.NewStyle().
			Width(maxWidth - containerStyle.GetHorizontalFrameSize()).
			Height(listHeight + 3). // Gives textarea a bit more vertical space
			UnsetBorderStyle()      // Textarea will inherit the container's border

	// Misc styles
	paginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(2).Foreground(lipgloss.Color("240"))
	helpStyle       = list.DefaultStyles().HelpStyle.PaddingLeft(2).PaddingBottom(1).Foreground(lipgloss.Color("240"))
)

// Render List -----------------------------------------------------------------

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	// Timestamp prefix (Modified time)
	memoTime := i.Modified.Format("01/02 15:04")
	// Show only the first line of content in the list view
	content := strings.SplitN(i.Content, "\n", 2)[0]
	str := fmt.Sprintf("[%s] %s", memoTime, content)

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

// Bubble Tea Model ------------------------------------------------------------

type model struct {
	list     list.Model
	textarea textarea.Model

	current  int
	spawning bool
	editing  bool
	quitting bool
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// On resize, restrict list width to maxWidth
		m.list.SetWidth(min(msg.Width, maxWidth))

		// Adjust textarea width/height to fit inside the container style
		taWidth := min(msg.Width, maxWidth) - containerStyle.GetHorizontalFrameSize()
		taHeight := min(msg.Height, listHeight+5)
		m.textarea.SetWidth(taWidth)
		m.textarea.SetHeight(taHeight)

		return m, nil

	case tea.KeyMsg:
		keypress := msg.String()

		if m.editing {
			// Editing Mode Keymaps --------------------------------------------
			switch keypress {
			case "esc": // Save and exit edit mode
				newContent := strings.TrimSpace(m.textarea.Value())

				if newContent == "" {
					// Don't create blank memos
					if m.spawning && m.current >= 0 {
						m.list.RemoveItem(m.current)
					}
				} else {
					// Save/Update the memo
					now := time.Now()

					if m.spawning {
						// This was a new memo
						newItem := item{
							Content:  newContent,
							Created:  now,
							Modified: now,
						}
						m.list.SetItem(m.current, newItem)
					} else if m.current >= 0 {
						// This was an existing memo being edited
						selectedItem := m.list.Items()[m.current].(item)

						newItem := item{
							Content:  newContent,
							Created:  selectedItem.Created,
							Modified: now, // Update modification time
						}
						m.list.SetItem(m.current, newItem)
					}

					// Save the updated list
					if err := saveListToMemos(m.list.Items()); err != nil {
						fmt.Fprintf(os.Stderr, "Error saving memo file after edit: %v\n", err)
					}
				}

				// Reset state and switch back to list view
				m.editing = false
				m.spawning = false
				m.current = -1
				m.textarea.Blur()
				return m, nil

			// All other keys are passed to the textarea
			default:
				m.textarea, cmd = m.textarea.Update(msg)
				return m, cmd
			}

		} else {
			// List View Keymaps -----------------------------------------------
			switch keypress {
			case "esc", "ctrl+c":
				m.quitting = true
				return m, tea.Quit

			case "enter": // Edit selected item
				if len(m.list.Items()) > 0 {
					selectedItem := m.list.SelectedItem()
					if item, ok := selectedItem.(item); ok {
						m.editing = true
						m.spawning = false
						m.current = m.list.Index()
						m.textarea.SetValue(item.Content)
						m.textarea.Focus()
						m.textarea.SetCursor(len(m.textarea.Value()))
						return m, nil
					}
				}

			case "tab": // Create a new memo
				m.editing = true
				m.current = m.list.Index()

				// Check if the currently selected item is essentially blank/empty
				isCurrentItemBlank := false
				if len(m.list.Items()) > 0 {
					if item, ok := m.list.SelectedItem().(item); ok && strings.TrimSpace(item.Content) == "" {
						isCurrentItemBlank = true
					}
				}

				if len(m.list.Items()) == 0 || !isCurrentItemBlank {
					// Insert a blank placeholder item at the current position
					placeholderItem := item{Content: "", Created: time.Now(), Modified: time.Now()}
					m.list.InsertItem(m.current, placeholderItem)
					m.list.Select(m.current) // Ensure we are editing the new item
					m.spawning = true
					m.textarea.SetValue("")
				} else {
					// If it's a blank item, just open it for editing
					m.spawning = true
					if item, ok := m.list.SelectedItem().(item); ok {
						m.textarea.SetValue(item.Content)
					}
				}

				m.textarea.Focus()
				return m, nil

			case "delete":
				if len(m.list.Items()) > 0 {
					m.list.RemoveItem(m.list.Index())
					if err := saveListToMemos(m.list.Items()); err != nil {
						fmt.Fprintf(os.Stderr, "Error saving memo file after deletion: %v\n", err)
					}
				}
				return m, nil
			}
		}
	}

	// Update list model only when NOT editing
	if !m.editing {
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	if m.editing {
		// Render the textarea inside the styled box
		header := "EDITING (ESC to save)"
		if m.spawning {
			header = "NEW MEMO (ESC to save, or clear/ESC to discard)"
		}

		content := fmt.Sprintf("%s\n\n%s", header, m.textarea.View())

		return containerStyle.Render(content)
	}

	// Render the list inside the styled box
	return containerStyle.Render(m.list.View())
}

// Persistence Logic -----------------------------------------------------------

func createDefaultMemos() []Memo {
	now := time.Now()
	return []Memo{
		{Content: "Welcome! Press 'Enter' to edit this memo.", Created: now, Modified: now},
		{Content: "Press 'Tab' to quickly create a new memo.\nHit 'Enter' in edit mode for newlines.", Created: now, Modified: now},
		{Content: "Press 'Esc' to quite."},
	}
}

func loadMemos() ([]Memo, error) {
	data, err := os.ReadFile(memosFileName)

	if os.IsNotExist(err) {
		fmt.Printf("Memos file '%s' not found. Creating and populating with defaults.\n", memosFileName)

		defaultMemos := createDefaultMemos()
		defaultData, marshalErr := json.MarshalIndent(defaultMemos, "", "  ")
		if marshalErr != nil {
			return nil, fmt.Errorf("could not marshal default memos: %w", marshalErr)
		}

		if writeErr := os.WriteFile(memosFileName, defaultData, 0644); writeErr != nil {
			return nil, fmt.Errorf("could not create and write file: %w", writeErr)
		}

		return defaultMemos, nil
	}

	if err != nil {
		return nil, fmt.Errorf("could not read file: %w", err)
	}

	var loadedItems items
	if err := json.Unmarshal(data, &loadedItems); err != nil {
		return nil, fmt.Errorf("could not unmarshal memos: %w", err)
	}

	return loadedItems, nil
}

func saveListToMemos(listItems []list.Item) error {
	// 1. Convert []list.Item to []Memo
	memos := make([]Memo, 0, len(listItems))
	for _, li := range listItems {
		memoItem, ok := li.(item)
		if !ok {
			continue
		}
		// Only save non-blank memos to the file
		if strings.TrimSpace(memoItem.Content) != "" {
			memos = append(memos, Memo(memoItem))
		}
	}

	// 2. Marshal the []Memo into JSON
	data, err := json.MarshalIndent(memos, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal memos for saving: %w", err)
	}

	// 3. Write the updated data to the file
	if err := os.WriteFile(memosFileName, data, 0644); err != nil {
		return fmt.Errorf("could not write file %s: %w", memosFileName, err)
	}
	return nil
}

// Initialize List -------------------------------------------------------------

func initializeList() model {
	memos, err := loadMemos()
	if err != nil {
		fmt.Printf("Fatal error during memo initialization: %v\n", err)
		os.Exit(1)
	}

	// Convert the []Memo to []list.Item
	listItems := make([]list.Item, len(memos))
	for i, m := range memos {
		listItems[i] = item(m)
	}

	// Calculate list dimensions based on the container style
	listWidth := maxWidth - containerStyle.GetHorizontalFrameSize()

	l := list.New(listItems, itemDelegate{}, listWidth, listHeight)
	l.Title = "Memos"
	l.SetShowStatusBar(false)
	l.Styles.Title = titleStyle
	l.Styles.PaginationStyle = paginationStyle
	l.Styles.HelpStyle = helpStyle
	l.SetFilteringEnabled(true)

	// Initialize textarea with container-friendly dimensions
	ta := textarea.New()
	ta.Placeholder = "Start typing your new memo..."
	ta.Focus()
	ta.Prompt = "┃ "
	ta.ShowLineNumbers = true
	ta.KeyMap.InsertNewline.SetKeys("enter")
	ta.SetWidth(listWidth)
	ta.SetHeight(listHeight + 3)

	return model{
		list: l,
		// Edit state initialization
		editing:  false,
		spawning: false,
		textarea: ta,
		current:  -1,
	}
}

// Main ------------------------------------------------------------------------

func main() {
	m := initializeList()

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
