package ui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"pvc/internal/command"
)

// errSource says who an error comes from.
type errSource int

const (
	// sourceProxmox: a command run on the node failed, or a Proxmox task ended in error.
	sourceProxmox errSource = iota + 1
	// sourceConsole: pve-console itself failed (invalid input, timeout, unreadable answer...).
	sourceConsole
)

func (s errSource) label() string {
	if s == sourceProxmox {
		return "proxmox"
	}
	return "pve-console"
}

// maxLogEntries is the number of entries kept in the log. When it is full the
// oldest success goes first, so a burst of successes does not push errors out.
const maxLogEntries = 500

// clock gives the time of an entry (a variable so tests are deterministic).
var clock = time.Now

// logEntry is one line of the log: an error, or the successful result of a task
// or of an action.
type logEntry struct {
	time    time.Time
	source  errSource
	kind    string
	context string
	message string
	detail  []string
	// ok is true for a success; the zero value is an error.
	ok bool
	// count is the number of times the same error happened in a row (always 1
	// for a success: two successes are two facts).
	count int
}

// logLevel filters the log by outcome.
type logLevel int

const (
	levelAll logLevel = iota
	levelErrorsOnly
	levelSuccessOnly
)

func (l logLevel) label() string {
	switch l {
	case levelErrorsOnly:
		return "errors"
	case levelSuccessOnly:
		return "success"
	}
	return "all"
}

// oneLine flattens text on one line: messages shown in a status row must not
// contain line breaks.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// classify tells where an error comes from. A command that failed is Proxmox's;
// but when the command could not even run or finish (timeout, program missing),
// pve-console is the one that failed.
func classify(err error) (errSource, string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return sourceConsole, "timeout"
	case errors.Is(err, context.Canceled):
		return sourceConsole, "canceled"
	case errors.Is(err, exec.ErrNotFound):
		return sourceConsole, "program not installed"
	}
	var commandErr command.CommandError
	if errors.As(err, &commandErr) {
		return sourceProxmox, "command failed"
	}
	return sourceConsole, "internal error"
}

// commandErrors collects every failed command inside err (errors.Join can hold several).
func commandErrors(err error) []command.CommandError {
	var found []command.CommandError
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if commandErr, ok := e.(command.CommandError); ok {
			found = append(found, commandErr)
			return
		}
		switch wrapped := e.(type) {
		case interface{ Unwrap() []error }:
			for _, inner := range wrapped.Unwrap() {
				walk(inner)
			}
		case interface{ Unwrap() error }:
			walk(wrapped.Unwrap())
		}
	}
	walk(err)
	return found
}

// errorDetails describes the commands behind an error: the command line, the
// exit code, how long it ran and what it printed on its error output.
func errorDetails(err error) []string {
	var lines []string
	for i, commandErr := range commandErrors(err) {
		if i > 0 {
			lines = append(lines, "")
		}
		result := commandErr.Result
		if line := strings.TrimSpace(command.Key(result.Command, result.Args...)); line != "" {
			lines = append(lines, "command: "+line)
		}
		lines = append(lines, fmt.Sprintf("exit code: %d", result.ExitCode))
		if result.Duration > 0 {
			lines = append(lines, "duration: "+result.Duration.Round(time.Millisecond).String())
		}
		if stderr := strings.TrimSpace(string(result.Stderr)); stderr != "" {
			lines = append(lines, "output:")
			for i, line := range strings.Split(stderr, "\n") {
				if i >= 20 {
					lines = append(lines, "  …")
					break
				}
				lines = append(lines, "  "+strings.TrimRight(line, "\r "))
			}
		}
	}
	return lines
}

// logError records err and shows it in the status row. context says what was
// being done ("create snapshot t1 for 300"). The model is passed by value, so a
// new journal is built instead of changing the old one.
func (m Model) logError(context string, err error) Model {
	source, kind := classify(err)
	return m.logProblem(source, kind, context, oneLine(err.Error()), errorDetails(err))
}

// logErrorQuiet records err without touching the status row: for background
// refreshes, whose error is already shown in their panel and must not overwrite
// the result of what the user just did.
func (m Model) logErrorQuiet(context string, err error) Model {
	message := m.message
	m = m.logError(context, err)
	m.message = message
	return m
}

// actionContext describes a guest, snapshot or backup action for the journal,
// e.g. "stop guest 300" or "delete snapshot snap1 (guest 300)".
func actionContext(msg destructiveActionMsg) string {
	text := strings.ReplaceAll(msg.action, "-", " ") + " " + msg.name
	if msg.vmid > 0 && msg.name != fmt.Sprint(msg.vmid) && !strings.Contains(msg.name, fmt.Sprint(msg.vmid)) {
		text += fmt.Sprintf(" (guest %d)", msg.vmid)
	}
	return strings.TrimSpace(text)
}

// logConsole records an error that comes from pve-console itself, such as an
// invalid input.
func (m Model) logConsole(kind string, context string, message string) Model {
	return m.logProblem(sourceConsole, kind, context, message, nil)
}

// logProblem adds an error to the log and shows its message. An error that
// repeats the previous one only counts one more occurrence and updates its time,
// so a failure that persists does not fill the log at every refresh.
func (m Model) logProblem(source errSource, kind string, context string, message string, detail []string) Model {
	return m.logProblemAt(clock(), source, kind, context, message, detail)
}

// logProblemAt is logProblem for an error that happened at a given time, such
// as a task that ended before it was noticed.
func (m Model) logProblemAt(at time.Time, source errSource, kind string, context string, message string, detail []string) Model {
	m.message = message

	if n := len(m.entries); n > 0 {
		last := m.entries[n-1]
		if !last.ok && last.source == source && last.context == context && last.message == message {
			entries := make([]logEntry, n)
			copy(entries, m.entries)
			last.count++
			last.time = at
			last.detail = detail
			entries[n-1] = last
			m.entries = entries
			return m
		}
	}

	if m.viewMode != viewLog {
		m.unreadErrors++
	}
	return m.addEntry(logEntry{time: at, source: source, kind: kind, context: context, message: message, detail: detail, count: 1})
}

// logOK records a success, such as a task that ended well or an action that
// completed. It leaves the status row alone (the caller already shows its own
// message) and never counts as unread: a success must not look like a problem.
func (m Model) logOK(at time.Time, source errSource, kind string, context string, message string, detail []string) Model {
	return m.addEntry(logEntry{time: at, source: source, kind: kind, context: context, message: message, detail: detail, ok: true, count: 1})
}

// logDone records an action of the user that pve-console ran and that ended at
// once (no Proxmox task to wait for).
func (m Model) logDone(context string, message string) Model {
	return m.logOK(clock(), sourceConsole, "action done", context, message, nil)
}

// addEntry appends an entry to a copy of the log, dropping one entry if the
// log is full: the oldest success, else the oldest entry.
func (m Model) addEntry(entry logEntry) Model {
	drop := -1
	if len(m.entries) >= maxLogEntries {
		drop = 0
		for i, old := range m.entries {
			if old.ok {
				drop = i
				break
			}
		}
	}
	entries := make([]logEntry, 0, len(m.entries)+1)
	for i, old := range m.entries {
		if i != drop {
			entries = append(entries, old)
		}
	}
	m.entries = append(entries, entry)
	return m
}

// filteredLog returns the log, most recent first, filtered by the source, the
// level and the search.
func (m Model) filteredLog() []logEntry {
	var out []logEntry
	query := normalizedQuery(m.query)
	for i := len(m.entries) - 1; i >= 0; i-- {
		entry := m.entries[i]
		if m.logSource != 0 && entry.source != m.logSource {
			continue
		}
		if (m.logLevel == levelErrorsOnly && entry.ok) || (m.logLevel == levelSuccessOnly && !entry.ok) {
			continue
		}
		if query != "" && !containsAnyFold([]string{entry.context, entry.message, entry.kind, entry.source.label()}, query) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// nextLogSource cycles the source filter: all, proxmox, pve-console.
func (m *Model) nextLogSource() {
	switch m.logSource {
	case 0:
		m.logSource = sourceProxmox
	case sourceProxmox:
		m.logSource = sourceConsole
	default:
		m.logSource = 0
	}
}

// nextLogLevel cycles the level filter: all, errors, success.
func (m *Model) nextLogLevel() {
	m.logLevel = (m.logLevel + 1) % 3
}

// logCounts returns how many entries come from each source.
func (m Model) logCounts() (proxmox int, console int) {
	for _, entry := range m.entries {
		if entry.source == sourceProxmox {
			proxmox++
		} else {
			console++
		}
	}
	return proxmox, console
}
