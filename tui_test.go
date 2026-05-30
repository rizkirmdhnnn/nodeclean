package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// send drives the model through a sequence of messages and returns the result.
func send(m model, msgs ...tea.Msg) model {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(model)
	}
	return m
}

func sampleTargets() []target {
	return []target{
		{path: "/a/node_modules", size: 100},
		{path: "/b/node_modules", size: 300},
		{path: "/c/node_modules", size: 200},
	}
}

func TestScanTransitionsToSelecting(t *testing.T) {
	m := newModel(options{})
	m = send(m, scanDoneMsg{targets: sampleTargets()})
	if m.state != stateSelecting {
		t.Fatalf("state = %v, want selecting", m.state)
	}
	if len(m.items) != 3 {
		t.Fatalf("items = %d, want 3", len(m.items))
	}
	// All start unchecked.
	if c, _ := m.selection(); c != 0 {
		t.Errorf("selected = %d, want 0 (unchecked by default)", c)
	}
}

func TestEmptyScanGoesToDone(t *testing.T) {
	m := newModel(options{})
	m = send(m, scanDoneMsg{targets: nil})
	if m.state != stateDone {
		t.Fatalf("state = %v, want done", m.state)
	}
}

func TestToggleAndSelectAll(t *testing.T) {
	m := newModel(options{})
	m = send(m, scanDoneMsg{targets: sampleTargets()})

	// Toggle current item (cursor at 0).
	m = send(m, key(" "))
	if c, _ := m.selection(); c != 1 {
		t.Errorf("after space: selected = %d, want 1", c)
	}

	// Select all.
	m = send(m, key("a"))
	if c, _ := m.selection(); c != 3 {
		t.Errorf("after 'a': selected = %d, want 3", c)
	}

	// Select none.
	m = send(m, key("a"))
	if c, _ := m.selection(); c != 0 {
		t.Errorf("after second 'a': selected = %d, want 0", c)
	}
}

func TestSortBySize(t *testing.T) {
	m := newModel(options{})
	m = send(m, scanDoneMsg{targets: sampleTargets()})
	m = send(m, key("s")) // sort by size desc
	if !m.sortBySize {
		t.Fatal("sortBySize should be true")
	}
	if m.items[0].size != 300 || m.items[2].size != 100 {
		t.Errorf("not sorted by size desc: %v", []int64{m.items[0].size, m.items[1].size, m.items[2].size})
	}
}

func TestEnterWithNoSelectionStays(t *testing.T) {
	m := newModel(options{})
	m = send(m, scanDoneMsg{targets: sampleTargets()})
	m = send(m, key("enter"))
	if m.state != stateSelecting {
		t.Errorf("state = %v, want selecting (nothing selected)", m.state)
	}
}

func TestEnterStartsDeleting(t *testing.T) {
	m := newModel(options{})
	m = send(m, scanDoneMsg{targets: sampleTargets()})
	m = send(m, key("a"))     // select all
	m = send(m, key("enter")) // confirm
	if m.state != stateDeleting {
		t.Fatalf("state = %v, want deleting", m.state)
	}
	if len(m.queue) != 3 {
		t.Errorf("queue = %d, want 3", len(m.queue))
	}
}

func TestDeleteProgressAdvancesToDone(t *testing.T) {
	m := newModel(options{})
	m = send(m, scanDoneMsg{targets: sampleTargets()})
	m = send(m, key("a"), key("enter"))

	// Simulate successful deletion messages for each queued item.
	for i := 0; i < len(m.queue); i++ {
		it := m.queue[i]
		m = send(m, deleteDoneMsg{path: it.path, size: it.size})
	}
	if m.state != stateDone {
		t.Fatalf("state = %v, want done", m.state)
	}
	if m.freed != 600 {
		t.Errorf("freed = %d, want 600", m.freed)
	}
}

func TestViewsRenderWithoutPanic(t *testing.T) {
	m := newModel(options{})
	m.width, m.height = 80, 24
	for _, st := range []appState{stateScanning, stateSelecting, stateDeleting, stateDone} {
		m.state = st
		if st == stateSelecting || st == stateDeleting || st == stateDone {
			m.applySort(sampleTargets())
		}
		if got := m.View(); got == "" {
			t.Errorf("state %v rendered empty view", st)
		}
	}
}

func TestQuitAborts(t *testing.T) {
	m := newModel(options{})
	m = send(m, scanDoneMsg{targets: sampleTargets()})
	m = send(m, key("q"))
	if !m.aborted {
		t.Error("expected aborted = true after 'q'")
	}
}
