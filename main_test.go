package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestParseDeps(t *testing.T) {
	input := `
{"Path":"mymod","Version":"v0.0.0","Main":true}
{"Path":"github.com/foo/bar","Version":"v1.2.3","Update":{"Path":"github.com/foo/bar","Version":"v1.4.0"}}
{"Path":"github.com/baz/qux","Version":"v0.3.1","Update":{"Path":"github.com/baz/qux","Version":"v1.0.0"},"Indirect":true}
{"Path":"github.com/direct/nodep","Version":"v2.0.0"}
{"Path":"github.com/has/update","Version":"v0.1.0","Update":{"Path":"github.com/has/update","Version":"v0.2.0"},"Deprecated":"use something else"}
`
	deps, err := parseDeps(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(deps))
	}

	// First: foo/bar (direct, has update)
	if deps[0].Path != "github.com/foo/bar" {
		t.Errorf("deps[0].Path = %q, want github.com/foo/bar", deps[0].Path)
	}
	if deps[0].Current != "v1.2.3" {
		t.Errorf("deps[0].Current = %q, want v1.2.3", deps[0].Current)
	}
	if deps[0].NewVersion != "v1.4.0" {
		t.Errorf("deps[0].NewVersion = %q, want v1.4.0", deps[0].NewVersion)
	}

	// Second: has/update (direct, has update, deprecated)
	if deps[1].Path != "github.com/has/update" {
		t.Errorf("deps[1].Path = %q, want github.com/has/update", deps[1].Path)
	}
	if deps[1].Deprecated != "use something else" {
		t.Errorf("deps[1].Deprecated = %q, want 'use something else'", deps[1].Deprecated)
	}
}

func TestParseDepsEmpty(t *testing.T) {
	deps, err := parseDeps(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

func TestParseDepsNoUpdates(t *testing.T) {
	input := `{"Path":"mymod","Version":"v0.0.0","Main":true}
{"Path":"github.com/foo/bar","Version":"v1.2.3"}
`
	deps, err := parseDeps(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

func TestParseDepsMalformed(t *testing.T) {
	input := `{"Path":"ok","Version":"v1.0.0"}
not json at all
`
	_, err := parseDeps(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestToggleSelection(t *testing.T) {
	m := initialModel()
	m.phase = phaseSelect
	m.deps = []Dependency{
		{Path: "a", Current: "v1", NewVersion: "v2"},
		{Path: "b", Current: "v1", NewVersion: "v2"},
		{Path: "c", Current: "v1", NewVersion: "v2"},
	}

	// Toggle first item with space
	result, _ := m.Update(tea.KeyPressMsg{Code: ' '})
	m = result.(model)
	if !m.selected[0] {
		t.Error("expected item 0 to be selected after space")
	}

	// Toggle again to deselect
	result, _ = m.Update(tea.KeyPressMsg{Code: ' '})
	m = result.(model)
	if m.selected[0] {
		t.Error("expected item 0 to be deselected after second space")
	}

	// Select all with 'a'
	result, _ = m.Update(tea.KeyPressMsg{Code: 'a'})
	m = result.(model)
	for i := range m.deps {
		if !m.selected[i] {
			t.Errorf("expected item %d to be selected after 'a'", i)
		}
	}

	// Toggle all off with 'a' again
	result, _ = m.Update(tea.KeyPressMsg{Code: 'a'})
	m = result.(model)
	for i := range m.deps {
		if m.selected[i] {
			t.Errorf("expected item %d to be deselected after second 'a'", i)
		}
	}
}

func TestCursorMovement(t *testing.T) {
	m := initialModel()
	m.phase = phaseSelect
	m.deps = []Dependency{
		{Path: "a", Current: "v1", NewVersion: "v2"},
		{Path: "b", Current: "v1", NewVersion: "v2"},
		{Path: "c", Current: "v1", NewVersion: "v2"},
	}

	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}

	// Move down
	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = result.(model)
	if m.cursor != 1 {
		t.Errorf("cursor = %d after down, want 1", m.cursor)
	}

	// Move down again
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = result.(model)
	if m.cursor != 2 {
		t.Errorf("cursor = %d after second down, want 2", m.cursor)
	}

	// Move down at bottom - should stay
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = result.(model)
	if m.cursor != 2 {
		t.Errorf("cursor = %d at bottom, want 2", m.cursor)
	}

	// Move up
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = result.(model)
	if m.cursor != 1 {
		t.Errorf("cursor = %d after up, want 1", m.cursor)
	}
}

func TestUpdateSequencing(t *testing.T) {
	m := initialModel()
	m.phase = phaseUpdating
	m.deps = []Dependency{
		{Path: "a", Current: "v1", NewVersion: "v2"},
		{Path: "b", Current: "v1", NewVersion: "v2"},
	}
	m.updateOrder = []int{0, 1}
	m.updatePos = 0
	m.updateErrs = make([]string, 2)

	// First dep completes successfully
	result, cmd := m.Update(depUpdatedMsg{index: 0, err: nil})
	m = result.(model)
	if m.updatePos != 1 {
		t.Errorf("updatePos = %d after first update, want 1", m.updatePos)
	}
	if m.phase != phaseUpdating {
		t.Errorf("phase = %d after first update, want phaseUpdating", m.phase)
	}
	if cmd == nil {
		t.Error("expected command after first update (to start next dep)")
	}

	// Second dep completes
	result, cmd = m.Update(depUpdatedMsg{index: 1, err: nil})
	m = result.(model)
	if m.updatePos != 2 {
		t.Errorf("updatePos = %d after second update, want 2", m.updatePos)
	}
	// Should still be updating (waiting for tidy)
	if m.phase != phaseUpdating {
		t.Errorf("phase = %d after all updates, want phaseUpdating (waiting for tidy)", m.phase)
	}
	if cmd == nil {
		t.Error("expected tidy command after all deps updated")
	}

	// Tidy completes
	result, _ = m.Update(tidyDoneMsg{err: nil})
	m = result.(model)
	if m.phase != phaseDone {
		t.Errorf("phase = %d after tidy, want phaseDone", m.phase)
	}
}

func TestUpdateWithErrors(t *testing.T) {
	m := initialModel()
	m.phase = phaseUpdating
	m.deps = []Dependency{
		{Path: "a", Current: "v1", NewVersion: "v2"},
	}
	m.updateOrder = []int{0}
	m.updatePos = 0
	m.updateErrs = make([]string, 1)

	// Dep fails
	result, _ := m.Update(depUpdatedMsg{index: 0, err: fmt.Errorf("network error")})
	m = result.(model)
	if m.updateErrs[0] != "network error" {
		t.Errorf("updateErrs[0] = %q, want 'network error'", m.updateErrs[0])
	}
}

func TestEnterWithNoSelection(t *testing.T) {
	m := initialModel()
	m.phase = phaseSelect
	m.deps = []Dependency{
		{Path: "a", Current: "v1", NewVersion: "v2"},
	}

	// Press enter with nothing selected
	result, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(model)
	if m.phase != phaseSelect {
		t.Errorf("phase = %d after enter with no selection, want phaseSelect", m.phase)
	}
	if cmd != nil {
		t.Error("expected nil command when pressing enter with no selection")
	}
}
