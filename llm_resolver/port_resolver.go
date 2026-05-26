package llm_resolver

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// compiledPatterns caches compiled regexes for matchesCommand keyed by the
// pattern string. Values are either *regexp.Regexp (successful compile) or
// invalidPatternSentinel for patterns that previously failed to compile.
var compiledPatterns sync.Map

// invalidPatternSentinel is stored in compiledPatterns when a pattern fails
// to compile, so subsequent calls skip regexp.Compile and fall back to
// substring matching immediately.
var invalidPatternSentinel = struct{}{}

// ResolveProcessPort finds the current port for a process identified by ProcessIdentifier.
// It uses the ProcessCache to get current processes and matches against the identifier.
func ResolveProcessPort(identifier *ProcessIdentifier, cache *ProcessCache) (int, error) {
	if identifier == nil || identifier.Workdir == "" {
		return 0, fmt.Errorf("process identifier with workdir is required")
	}

	processes, err := cache.Get()
	if err != nil {
		return 0, fmt.Errorf("failed to get processes: %w", err)
	}

	var candidates []LocalProcess

	// Filter by workdir (prefix match)
	for _, proc := range processes {
		if !matchesWorkdir(proc.Workdir, identifier.Workdir) {
			continue
		}

		// If CommandPattern is specified, filter further
		if identifier.CommandPattern != "" {
			if !matchesCommand(proc, identifier.CommandPattern) {
				continue
			}
		}

		candidates = append(candidates, proc)
	}

	if len(candidates) == 0 {
		return 0, fmt.Errorf("no process found matching workdir %q", identifier.Workdir)
	}

	// If multiple candidates, prefer the one with the lowest port
	// (common pattern: Vite uses lower ports for main dev server)
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.Port < best.Port {
			best = c
		}
	}

	return best.Port, nil
}

// matchesWorkdir checks if a process workdir matches the target workdir.
// Uses prefix matching so /home/user/project matches /home/user/project/frontend
func matchesWorkdir(processWorkdir, targetWorkdir string) bool {
	if processWorkdir == "" || targetWorkdir == "" {
		return false
	}

	// Normalize paths (remove trailing slashes)
	processWorkdir = strings.TrimSuffix(processWorkdir, "/")
	targetWorkdir = strings.TrimSuffix(targetWorkdir, "/")

	// Exact match
	if processWorkdir == targetWorkdir {
		return true
	}

	// Process workdir is under target workdir (target is parent)
	if strings.HasPrefix(processWorkdir, targetWorkdir+"/") {
		return true
	}

	// Target workdir is under process workdir (process is parent)
	if strings.HasPrefix(targetWorkdir, processWorkdir+"/") {
		return true
	}

	return false
}

// matchesCommand checks if a process matches the command pattern regex.
// Compiled regexes are cached per pattern so hot lookups don't repeatedly
// pay the regexp.Compile cost; patterns that fail to compile are remembered
// via invalidPatternSentinel and fall back to substring matching.
func matchesCommand(proc LocalProcess, pattern string) bool {
	if cached, ok := compiledPatterns.Load(pattern); ok {
		if re, ok := cached.(*regexp.Regexp); ok {
			return re.MatchString(proc.Command) || re.MatchString(proc.Args)
		}
		// Sentinel for an invalid pattern: fall back to substring match.
		return strings.Contains(proc.Command, pattern) || strings.Contains(proc.Args, pattern)
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		// Invalid regex, remember the failure and fall back to literal substring match.
		compiledPatterns.Store(pattern, invalidPatternSentinel)
		return strings.Contains(proc.Command, pattern) || strings.Contains(proc.Args, pattern)
	}

	compiledPatterns.Store(pattern, re)
	return re.MatchString(proc.Command) || re.MatchString(proc.Args)
}
