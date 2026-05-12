package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/bprendie/weazlcode/internal/storage"
)

type sessionItem struct {
	session storage.Session
}

func (i sessionItem) Title() string { return i.session.Title }
func (i sessionItem) Description() string {
	project := i.session.ProjectRoot
	if project != "" {
		project = "   " + project
	}
	return fmt.Sprintf("%s / %s   in %d / out %d%s", i.session.Provider, i.session.Model, i.session.InputTokens, i.session.OutputTokens, project)
}
func (i sessionItem) FilterValue() string { return i.session.Title }

type workspaceItem storage.WorkspaceSave

func (i workspaceItem) Title() string {
	save := storage.WorkspaceSave(i)
	return fmt.Sprintf("%s: %s", workspaceLabel(save.Name), workspaceTime(save.CreatedAt))
}
func (i workspaceItem) Description() string {
	return storage.WorkspaceSave(i).SessionID
}
func (i workspaceItem) FilterValue() string { return workspaceLabel(storage.WorkspaceSave(i).Name) }

type previousSessionMsg struct {
	session       storage.Session
	messages      []storage.Message
	checkpoint    storage.ContextCheckpoint
	hasCheckpoint bool
	rendered      string
	renderWidth   int
	err           error
}

func (m model) startChat() (tea.Model, tea.Cmd) {
	if m.cfg.UI.ResumeLastSession {
		m.mode = modeLoading
		m.status = "loading previous session"
		m.working.Spinner = spinner.Jump
		return m, tea.Batch(m.loadPreviousSession(), m.working.Tick)
	}
	return m.newSession()
}

func (m model) loadPreviousSession() tea.Cmd {
	renderer := m
	renderer.markdown.term = nil
	return func() tea.Msg {
		sess, ok, err := m.store.LatestSession()
		if err != nil || !ok {
			return previousSessionMsg{err: err}
		}
		msgs, err := m.store.Messages(sess.ID)
		if err != nil {
			return previousSessionMsg{err: err}
		}
		cp, hasCheckpoint, err := m.store.LatestContextCheckpoint(sess.ID)
		if err != nil {
			return previousSessionMsg{err: err}
		}
		rendered := ""
		if renderer.viewport.Width > 0 {
			rendered = renderer.renderTranscript(msgs)
		}
		return previousSessionMsg{
			session:       sess,
			messages:      msgs,
			checkpoint:    cp,
			hasCheckpoint: hasCheckpoint,
			rendered:      rendered,
			renderWidth:   renderer.viewport.Width,
		}
	}
}

func (m model) newSession() (tea.Model, tea.Cmd) {
	p := m.cfg.Active()
	sess := storage.Session{
		ID:          uuid.NewString(),
		Title:       "New session",
		Provider:    m.cfg.ActiveProvider,
		Model:       p.Model,
		ProjectRoot: m.project.Root,
	}
	if err := m.store.CreateProjectSession(sess.ID, sess.Title, sess.Provider, sess.Model, sess.ProjectRoot); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.session = sess
	m.messages = nil
	m.checkpoint = storage.ContextCheckpoint{}
	m.hasCheckpoint = false
	m.activeWorkspaceID = 0
	m.activeWorkspaceName = ""
	m.activeWorkspaceAt = time.Time{}
	m.historyIdx = 0
	m.historyDraft = ""
	m.mode = modeChat
	m.status = fmt.Sprintf("%s %s | %s", p.Type, p.Model, m.project.StatusLabel())
	m.renderMessages()
	return m, nil
}

func (m model) loadSession(sess storage.Session) (tea.Model, tea.Cmd) {
	m.session = sess
	msgs, err := m.store.Messages(sess.ID)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.messages = msgs
	m.refreshCheckpoint()
	m.historyIdx = 0
	m.historyDraft = ""
	m.activeWorkspaceID = 0
	m.activeWorkspaceName = ""
	m.activeWorkspaceAt = time.Time{}
	m.mode = modeChat
	m.status = "resumed " + sess.Title + " | " + m.project.StatusLabel()
	m.input.Focus()
	m.renderMessages()
	return m, nil
}

func (m model) showSessions() (tea.Model, tea.Cmd) {
	sessions, err := m.store.ListSessions(50)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	items := make([]list.Item, 0, len(sessions))
	for _, sess := range sessions {
		items = append(items, sessionItem{session: sess})
	}
	m.sessions.SetItems(items)
	m.mode = modeSessions
	m.status = "select session | ctrl+d delete"
	return m, nil
}

func (m model) deleteSelectedSession() (tea.Model, tea.Cmd) {
	item, ok := m.sessions.SelectedItem().(sessionItem)
	if !ok {
		return m, nil
	}
	deletedCurrent := item.session.ID == m.session.ID
	if err := m.store.DeleteSession(item.session.ID); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.err = ""
	m.status = "deleted " + item.session.Title
	updated, cmd := m.showSessions()
	m = updated.(model)
	m.status = "deleted " + item.session.Title
	if deletedCurrent {
		m.session = storage.Session{}
		m.messages = nil
		return m.newSession()
	}
	return m, cmd
}

func (m model) showWorkspaces() (tea.Model, tea.Cmd) {
	saves, err := m.store.WorkspaceSaves(50)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	items := make([]list.Item, 0, len(saves))
	for _, save := range saves {
		items = append(items, workspaceItem(save))
	}
	m.workspaces.SetItems(items)
	m.mode = modeWorkspace
	return m, nil
}

func (m model) loadWorkspace(save storage.WorkspaceSave) (tea.Model, tea.Cmd) {
	sess, ok, err := m.store.Session(save.SessionID)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	if !ok {
		m.err = "workspace session not found"
		return m, nil
	}
	m.session = sess
	m.activeWorkspaceID = save.ID
	m.activeWorkspaceName = save.Name
	m.activeWorkspaceAt = save.CreatedAt
	if save.ThroughMessageID > 0 {
		m.messages, err = m.store.MessagesThrough(save.SessionID, save.ThroughMessageID)
	} else {
		m.messages, err = m.store.Messages(save.SessionID)
	}
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.refreshCheckpoint()
	m.historyIdx = 0
	m.historyDraft = ""
	m.mode = modeChat
	m.input.Focus()
	m.renderMessages()
	m.status = "workspace replay " + save.Name
	return m, nil
}

func (m model) deleteSelectedWorkspace() (tea.Model, tea.Cmd) {
	item, ok := m.workspaces.SelectedItem().(workspaceItem)
	if !ok {
		return m, nil
	}
	save := storage.WorkspaceSave(item)
	if err := m.store.DeleteWorkspace(save.ID); err != nil {
		m.err = err.Error()
		return m, nil
	}
	if m.activeWorkspaceID == save.ID {
		m.activeWorkspaceID = 0
		m.activeWorkspaceName = ""
		m.activeWorkspaceAt = time.Time{}
	}
	m.err = ""
	m.status = "deleted workspace: " + workspaceLabel(save.Name)
	updated, cmd := m.showWorkspaces()
	m = updated.(model)
	m.status = "deleted workspace: " + workspaceLabel(save.Name)
	return m, cmd
}

func (m *model) saveWorkspace() {
	throughID := int64(0)
	if len(m.messages) > 0 {
		throughID = m.messages[len(m.messages)-1].ID
	}
	if m.activeWorkspaceID != 0 {
		if err := m.store.UpdateWorkspace(m.activeWorkspaceID, m.session.ID, m.viewport.View(), throughID); err != nil {
			m.err = err.Error()
			return
		}
		m.status = "workspace updated: " + m.activeWorkspaceName
		m.err = ""
		return
	}
	now := time.Now()
	name := workspaceName(m.session.Title)
	id, err := m.store.SaveWorkspace(name, m.session.ID, m.viewport.View(), throughID)
	if err != nil {
		m.err = err.Error()
		return
	}
	m.activeWorkspaceID = id
	m.activeWorkspaceName = name
	m.activeWorkspaceAt = now
	m.status = "workspace saved: " + name
	m.err = ""
}

func (m model) startRenameWorkspace() (tea.Model, tea.Cmd) {
	returnMode := m.mode
	save, ok := m.renameTargetWorkspace()
	if !ok {
		return m, nil
	}
	m.renameWorkspaceID = save.ID
	m.renameReturnMode = returnMode
	m.renameDraft = m.input.Value()
	m.input.SetValue(workspaceLabel(save.Name))
	m.input.CursorEnd()
	m.input.Placeholder = "workspace name"
	m.input.Focus()
	m.mode = modeRenameWorkspace
	m.status = "rename workspace"
	m.err = ""
	return m, nil
}

func (m *model) renameTargetWorkspace() (storage.WorkspaceSave, bool) {
	switch m.mode {
	case modeChat:
		if m.activeWorkspaceID == 0 {
			m.saveWorkspace()
			if m.err != "" {
				return storage.WorkspaceSave{}, false
			}
		}
		return storage.WorkspaceSave{
			ID:        m.activeWorkspaceID,
			Name:      m.activeWorkspaceName,
			SessionID: m.session.ID,
			CreatedAt: m.activeWorkspaceAt,
		}, true
	case modeWorkspace:
		item, ok := m.workspaces.SelectedItem().(workspaceItem)
		if !ok {
			return storage.WorkspaceSave{}, false
		}
		return storage.WorkspaceSave(item), true
	}
	return storage.WorkspaceSave{}, false
}

func (m model) finishRenameWorkspace() (tea.Model, tea.Cmd) {
	label := strings.Join(strings.Fields(m.input.Value()), " ")
	if label == "" {
		m.err = "workspace name is required"
		return m, nil
	}
	name := label
	if err := m.store.RenameWorkspace(m.renameWorkspaceID, name); err != nil {
		m.err = err.Error()
		return m, nil
	}
	renamedID := m.renameWorkspaceID
	returnMode := m.renameReturnMode
	m.renameWorkspaceID = 0
	m.input.SetValue(m.renameDraft)
	m.input.CursorEnd()
	m.renameDraft = ""
	m.input.Placeholder = "message " + m.cfg.Active().Model
	m.err = ""
	m.status = "renamed workspace: " + name
	if m.activeWorkspaceID == renamedID {
		m.activeWorkspaceName = name
	}
	if returnMode == modeWorkspace {
		updated, cmd := m.showWorkspaces()
		m = updated.(model)
		m.status = "renamed workspace: " + name
		return m, cmd
	}
	m.mode = modeChat
	m.input.Focus()
	return m, nil
}

func (m model) cancelRenameWorkspace() (tea.Model, tea.Cmd) {
	returnMode := m.renameReturnMode
	m.renameWorkspaceID = 0
	m.input.SetValue(m.renameDraft)
	m.input.CursorEnd()
	m.renameDraft = ""
	m.input.Placeholder = "message " + m.cfg.Active().Model
	m.mode = returnMode
	if m.mode == modeChat {
		m.input.Focus()
	}
	m.status = "rename canceled"
	m.err = ""
	return m, nil
}

func workspaceLabel(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if idx := strings.Index(name, ": "); idx >= len("2006-01-02 15:04:05") {
		name = strings.TrimSpace(name[idx+1:])
	}
	return name
}

func workspaceTime(t time.Time) string {
	if t.IsZero() {
		return "unknown time"
	}
	return t.Format("2006-01-02 15:04:05")
}

func workspaceName(label string) string {
	label = strings.Join(strings.Fields(label), " ")
	if label == "" {
		label = "workspace"
	}
	return label
}
