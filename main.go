package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Data Structure --------------------------------------------------------------

type Memo struct {
	ID        string     `json:"id"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

func (m Memo) FilterValue() string { return m.Content }

func (m Memo) Title() string {
	if idx := strings.IndexByte(m.Content, '\n'); idx != -1 {
		return truncate(m.Content[:idx], 50)
	}
	if len(m.Content) == 0 {
		return "(empty memo)"
	}
	return truncate(m.Content, 50)
}

func (m Memo) Description() string { return m.UpdatedAt.Format("2006-01-02 15:04:05") }

type MemoData struct {
	Active  []Memo `json:"active"`
	Deleted []Memo `json:"deleted"`
}

// Data Persistence ------------------------------------------------------------

type Storage struct{ filepath string }

func NewStorage(filepath string) *Storage {
	return &Storage{filepath}
}

func (s *Storage) Load() (*MemoData, error) {
	data, err := os.ReadFile(s.filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return &MemoData{Active: make([]Memo, 0, 16), Deleted: make([]Memo, 0, 8)}, nil
		}
		return nil, err
	}

	var memoData MemoData
	if err := json.Unmarshal(data, &memoData); err != nil {
		var memos []Memo
		if err := json.Unmarshal(data, &memos); err != nil {
			return nil, err
		}
		return &MemoData{Active: memos, Deleted: make([]Memo, 0, 8)}, nil
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	n := 0
	for i := range memoData.Deleted {
		if memoData.Deleted[i].DeletedAt != nil && memoData.Deleted[i].DeletedAt.After(cutoff) {
			memoData.Deleted[n] = memoData.Deleted[i]
			n++
		}
	}

	if n != len(memoData.Deleted) {
		memoData.Deleted = memoData.Deleted[:n]
		go func() {
			if err := s.Save(&memoData); err != nil {
				log.Printf("Warning: failed to save cleaned deleted memos: %v", err)
			}
		}()
	}

	return &memoData, nil
}

func (s *Storage) Save(data *MemoData) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filepath, jsonData, 0644)
}

// Model -----------------------------------------------------------------------

type ViewMode uint8

const (
	ViewModeList ViewMode = iota
	ViewModeEdit
)

type Model struct {
	list     list.Model
	textarea textarea.Model
	storage  *Storage

	memos       []Memo
	deleted     []Memo
	currentMode ViewMode
	currentMemo *Memo

	flags uint8

	savedFilterValue string
	width, height    int
}

const (
	flagIsNewMemo   uint8 = 1 << 0
	flagWasFiltered uint8 = 1 << 1
)

func (m *Model) setFlag(flag uint8)      { m.flags |= flag }
func (m *Model) clearFlag(flag uint8)    { m.flags &^= flag }
func (m *Model) hasFlag(flag uint8) bool { return m.flags&flag != 0 }

func InitialModel() Model {
	return Model{
		list:        newList(make([]list.Item, 0, 32)),
		textarea:    newTextarea(),
		storage:     NewStorage(".yellow.json"),
		memos:       make([]Memo, 0, 32),
		deleted:     make([]Memo, 0, 8),
		currentMode: ViewModeList,
	}
}

func (m Model) Init() tea.Cmd {
	return loadMemos(m.storage)
}

// Update ----------------------------------------------------------------------

type loadMemosMsg struct {
	data *MemoData
	err  error
}

type saveCompleteMsg struct{ err error }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadMemosMsg:
		if msg.err != nil {
			log.Printf("Error loading: %v", msg.err)
			return m, nil
		}
		m.memos = msg.data.Active
		m.deleted = msg.data.Deleted
		sortMemosNewestFirst(m.memos)
		m.list.SetItems(memosToItems(m.memos))
		return m, nil

	case saveCompleteMsg:
		if msg.err != nil {
			log.Printf("Error saving: %v", msg.err)
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeComponents()
		return m, nil

	case tea.KeyMsg:
		if m.currentMode == ViewModeList {
			return m.handleListKeys(msg)
		}
		return m.handleEditKeys(msg)
	}

	return m.updateActiveComponent(msg)
}

func (m Model) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filterState := m.list.FilterState()

	if filterState == list.Filtering {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		if msg.String() == "esc" {
			m.list.ResetFilter()
		}
		return m, cmd
	}

	if filterState == list.FilterApplied {
		if msg.String() == "esc" {
			m.list.ResetFilter()
			return m, nil
		}
		if msg.String() == "enter" && len(m.memos) > 0 {
			return m.editSelected()
		}
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab":
		return m.createNew()
	case "delete", "backspace":
		if len(m.memos) > 0 {
			return m.deleteSelected()
		}
	case "enter":
		if len(m.memos) > 0 {
			return m.editSelected()
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) handleEditKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.saveAndExit()
	case "ctrl+c":
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m Model) updateActiveComponent(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.currentMode == ViewModeList {
		m.list, cmd = m.list.Update(msg)
	} else {
		m.textarea, cmd = m.textarea.Update(msg)
	}
	return m, cmd
}

// Update Commands -------------------------------------------------------------

func (m Model) createNew() (tea.Model, tea.Cmd) {
	m.saveFilterState()
	m.currentMemo = &Memo{
		ID:        generateID(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.setFlag(flagIsNewMemo)
	m.currentMode = ViewModeEdit
	m.textarea.SetValue("")
	m.textarea.Focus()
	m.resizeComponents()
	return m, textarea.Blink
}

func (m Model) editSelected() (tea.Model, tea.Cmd) {
	if item := m.list.SelectedItem(); item != nil {
		m.saveFilterState()
		memo := item.(Memo)
		m.currentMemo = &memo
		m.clearFlag(flagIsNewMemo)
		m.currentMode = ViewModeEdit
		m.textarea.SetValue(memo.Content)
		m.textarea.Focus()
		m.resizeComponents()
		return m, textarea.Blink
	}
	return m, nil
}

func (m Model) deleteSelected() (tea.Model, tea.Cmd) {
	item := m.list.SelectedItem()
	if item == nil {
		return m, nil
	}

	memo := item.(Memo)
	for i := range m.memos {
		if m.memos[i].ID == memo.ID {
			now := time.Now()
			memo.DeletedAt = &now
			m.deleted = append(m.deleted, memo)
			// Efficient slice deletion
			m.memos = append(m.memos[:i], m.memos[i+1:]...)
			break
		}
	}

	sortMemosNewestFirst(m.memos)
	m.list.SetItems(memosToItems(m.memos))
	return m, saveMemos(m.storage, &MemoData{
		Active:  m.memos,
		Deleted: m.deleted,
	})
}

func (m Model) saveAndExit() (tea.Model, tea.Cmd) {
	content := m.textarea.Value()

	if m.hasFlag(flagIsNewMemo) {
		if strings.TrimSpace(content) != "" {
			m.currentMemo.Content = content
			m.currentMemo.UpdatedAt = time.Now()
			m.memos = append(m.memos, *m.currentMemo)
		}
	} else {
		for i := range m.memos {
			if m.memos[i].ID == m.currentMemo.ID {
				m.memos[i].Content = content
				m.memos[i].UpdatedAt = time.Now()
				break
			}
		}
	}

	sortMemosNewestFirst(m.memos)
	m.list.SetItems(memosToItems(m.memos))
	m.restoreFilterState()

	m.currentMode = ViewModeList
	m.textarea.Blur()
	m.currentMemo = nil
	m.clearFlag(flagIsNewMemo)
	m.resizeComponents()

	return m, saveMemos(m.storage, &MemoData{
		Active:  m.memos,
		Deleted: m.deleted,
	})
}

func (m *Model) saveFilterState() {
	if m.list.FilterState() == list.FilterApplied {
		m.setFlag(flagWasFiltered)
		m.savedFilterValue = m.list.FilterValue()
	}
}

func (m *Model) restoreFilterState() {
	if m.hasFlag(flagWasFiltered) && m.savedFilterValue != "" {
		m.list.SetFilterText(m.savedFilterValue)
		m.clearFlag(flagWasFiltered)
		m.savedFilterValue = ""
	}
}

func (m *Model) resizeComponents() {
	if m.width == 0 || m.height == 0 {
		return
	}

	vm, hm := appStyle.GetFrameSize()
	helpHeight := lipgloss.Height(m.helpView())

	if m.currentMode == ViewModeList {
		m.list.SetSize(m.width-hm, m.height-vm-helpHeight)
	} else {
		titleHeight := lipgloss.Height(m.titleView())
		m.textarea.SetWidth(m.width - hm - 4)
		m.textarea.SetHeight(m.height - vm - titleHeight - helpHeight)
	}
}

// View ------------------------------------------------------------------------

func (m Model) View() string {
	if m.currentMode == ViewModeList {
		return appStyle.Render(
			lipgloss.JoinVertical(lipgloss.Left, m.list.View(), m.helpView()),
		)
	}
	return appStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left, m.titleView(), m.textarea.View(), m.helpView()),
	)
}

func (m Model) titleView() string {
	title := "Edit Memo"
	if m.hasFlag(flagIsNewMemo) {
		title = "New Memo"
	}
	return editTitleStyle.Render(title)
}

func (m Model) helpView() string {
	if m.currentMode == ViewModeList {
		filterState := m.list.FilterState()

		switch filterState {
		case list.Filtering:
			return helpStyle.Render("Esc: cancel filter")
		case list.FilterApplied:
			return helpStyle.Render("Enter: edit • Esc: return to list view")
		default:
			if len(m.memos) > 0 {
				return helpStyle.Render("Tab: new • Enter: edit • Delete: delete • ↑/k up • ↓/j down • / filter • q quit")
			}
			return helpStyle.Render("Tab: new • q quit")
		}
	}
	return helpStyle.Render("Esc: save changes")
}

func loadMemos(s *Storage) tea.Cmd {
	return func() tea.Msg {
		data, err := s.Load()
		return loadMemosMsg{data, err}
	}
}

func saveMemos(s *Storage, data *MemoData) tea.Cmd {
	return func() tea.Msg {
		return saveCompleteMsg{s.Save(data)}
	}
}

// UI --------------------------------------------------------------------------

var (
	colorPrimary    = lipgloss.Color("#FCB53B")
	colorLineNumber = lipgloss.Color("240")
	colorText       = lipgloss.Color("250")
	colorMuted      = lipgloss.Color("241")
	colorBackground = lipgloss.Color("#1c1b1c")
	colorEndBuffer  = lipgloss.Color("237")

	appStyle   = lipgloss.NewStyle().Padding(1, 2)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)

	editTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			PaddingLeft(2).
			PaddingBottom(1)

	helpStyle = lipgloss.NewStyle().Foreground(colorMuted).MarginTop(1)
)

func newList(items []list.Item) list.Model {
	d := list.NewDefaultDelegate()

	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Foreground(colorPrimary).
		BorderLeftForeground(colorPrimary)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		Foreground(colorPrimary).
		BorderLeftForeground(colorPrimary)

	l := list.New(items, d, 0, 0)
	l.Title = "Yellow"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.Styles.Title = titleStyle
	l.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(colorPrimary)
	l.Styles.DefaultFilterCharacterMatch = lipgloss.NewStyle().Foreground(colorPrimary).Underline(true).Bold(true)
	l.FilterInput.PromptStyle = lipgloss.NewStyle().Foreground(colorPrimary)
	l.FilterInput.Cursor.Style = lipgloss.NewStyle().Foreground(colorPrimary)

	return l
}

func newTextarea() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Start typing ..."
	ta.CharLimit = 0
	ta.ShowLineNumbers = true
	ta.SetWidth(80)

	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle().Background(colorBackground)
	ta.FocusedStyle.CursorLineNumber = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	ta.FocusedStyle.LineNumber = lipgloss.NewStyle().Foreground(colorLineNumber)
	ta.FocusedStyle.Text = lipgloss.NewStyle().Foreground(colorText)
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(colorLineNumber)
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(colorPrimary).UnsetBorderStyle()
	ta.FocusedStyle.EndOfBuffer = lipgloss.NewStyle().Foreground(colorEndBuffer)

	return ta
}

// Utils -----------------------------------------------------------------------

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func memosToItems(memos []Memo) []list.Item {
	items := make([]list.Item, len(memos))
	for i := range memos {
		items[i] = memos[i]
	}
	return items
}

func sortMemosNewestFirst(memos []Memo) {
	sort.Slice(memos, func(i, j int) bool {
		return memos[i].UpdatedAt.After(memos[j].UpdatedAt)
	})
}

func setupLogging() error {
	f, err := os.OpenFile(".yellow.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	log.SetOutput(f)
	return nil
}

// Main ------------------------------------------------------------------------

func main() {
	if err := setupLogging(); err != nil {
		fmt.Printf("Warning: Could not set up logging: %v\n", err)
	}

	p := tea.NewProgram(InitialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
