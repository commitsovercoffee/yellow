package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
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

// List Styling ----------------------------------------------------------------

const (
	listHeight    = 6
	memosFileName = ".memos.json"
)

var (
	titleStyle        = lipgloss.NewStyle().MarginLeft(2)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	paginationStyle   = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
	helpStyle         = list.DefaultStyles().HelpStyle.PaddingLeft(4).PaddingBottom(1)
	quitTextStyle     = lipgloss.NewStyle().Margin(1, 0, 2, 4)
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
	str := fmt.Sprintf("%d. [%s] %s", index+1, memoTime, i.Content)

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
	quitting bool
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit

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

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	return "\n" + m.list.View()
}

// Persistence Logic -----------------------------------------------------------

func createDefaultMemos() []Memo {
	now := time.Now()
	return []Memo{
		{Content: "Welcome! Add your first memo.", Created: now, Modified: now},
		{Content: "This list is saved to a JSON file.", Created: now, Modified: now},
		{Content: "Press 'q' or 'Ctrl+c' to quit.", Created: now, Modified: now},
	}
}

func loadMemos() ([]Memo, error) {
	data, err := os.ReadFile(memosFileName)

	// If the file is not found, create one with defaults.
	if os.IsNotExist(err) {
		fmt.Printf("Memos file '%s' not found. Creating and populating with defaults.\n", memosFileName)

		// Get defaults with current timestamps
		defaultMemos := createDefaultMemos()

		// Marshal the default memos into JSON
		defaultData, marshalErr := json.MarshalIndent(defaultMemos, "", "  ")
		if marshalErr != nil {
			return nil, fmt.Errorf("could not marshal default memos: %w", marshalErr)
		}

		// Write the default JSON data to the file
		if writeErr := os.WriteFile(memosFileName, defaultData, 0644); writeErr != nil {
			return nil, fmt.Errorf("could not create and write file: %w", writeErr)
		}

		// Return the defaults to the caller
		return defaultMemos, nil
	}

	// Handle other errors during ReadFile
	if err != nil {
		return nil, fmt.Errorf("could not read file: %w", err)
	}

	// Unmarshal data from an existing file
	var loadedItems items
	if err := json.Unmarshal(data, &loadedItems); err != nil {
		return nil, fmt.Errorf("could not unmarshal memos: %w", err)
	}

	return loadedItems, nil
}

func saveListToMemos(listItems []list.Item) error {
	// 1. Convert []list.Item to []Memo
	memos := make([]Memo, len(listItems))
	for i, li := range listItems {
		// li is item (Memo alias), we can safely cast it
		memoItem, ok := li.(item)
		if !ok {
			// Should not happen if itemDelegate is set correctly
			continue
		}
		memos[i] = Memo(memoItem)
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

func initializeList() list.Model {
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

	const defaultWidth = 20
	l := list.New(listItems, itemDelegate{}, defaultWidth, listHeight)
	l.Title = "Memos"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = titleStyle
	l.Styles.PaginationStyle = paginationStyle
	l.Styles.HelpStyle = helpStyle
	l.SetFilteringEnabled(true)

	return l
}

// Main ------------------------------------------------------------------------

func main() {
	l := initializeList()
	m := model{list: l}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
