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

// Workdir match ranks, most specific first.
const (
	rankExact       = iota
	rankUnderTarget // process runs in a subdirectory of the mapped workdir
	rankAboveTarget // process runs in a parent of the mapped workdir
)

// maxParentDistance caps how far above the mapped workdir a process may run.
// Without it, a shell or agent parked in a high-level directory claims every mapping.
const maxParentDistance = 2

// workdirMatch describes how specifically a process workdir matches the target.
type workdirMatch struct {
	rank     int
	distance int // path segments between the two directories
}

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

	var best *LocalProcess
	var bestMatch workdirMatch

	for i, proc := range processes {
		match, ok := matchWorkdir(proc.Workdir, identifier.Workdir)
		if !ok {
			continue
		}

		// If CommandPattern is specified, filter further
		if identifier.CommandPattern != "" {
			if !matchesCommand(proc, identifier.CommandPattern) {
				continue
			}
		}

		if best == nil || betterMatch(match, proc.Port, bestMatch, best.Port) {
			best = &processes[i]
			bestMatch = match
		}
	}

	if best == nil {
		return 0, fmt.Errorf("no process found matching workdir %q", identifier.Workdir)
	}

	return best.Port, nil
}

// betterMatch reports whether candidate a beats b: closer workdir first, and
// only between equally close candidates the lowest port (Vite-style main server).
func betterMatch(a workdirMatch, aPort int, b workdirMatch, bPort int) bool {
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	if a.distance != b.distance {
		return a.distance < b.distance
	}
	return aPort < bPort
}

// matchWorkdir reports how a process workdir relates to the mapped workdir.
// Parent-direction matches are deliberately restricted: an unrestricted one lets
// any process running in a home directory hijack every process mapping.
func matchWorkdir(processWorkdir, targetWorkdir string) (workdirMatch, bool) {
	processWorkdir = strings.TrimSuffix(processWorkdir, "/")
	targetWorkdir = strings.TrimSuffix(targetWorkdir, "/")

	if processWorkdir == "" || targetWorkdir == "" {
		return workdirMatch{}, false
	}

	if processWorkdir == targetWorkdir {
		return workdirMatch{rank: rankExact}, true
	}

	if rest := strings.TrimPrefix(processWorkdir, targetWorkdir+"/"); rest != processWorkdir {
		return workdirMatch{rank: rankUnderTarget, distance: segmentCount(rest)}, true
	}

	if rest := strings.TrimPrefix(targetWorkdir, processWorkdir+"/"); rest != targetWorkdir {
		distance := segmentCount(rest)
		if distance > maxParentDistance || isUserHome(processWorkdir) {
			return workdirMatch{}, false
		}
		return workdirMatch{rank: rankAboveTarget, distance: distance}, true
	}

	return workdirMatch{}, false
}

// isUserHome reports whether dir is a user home directory (/home/x, /Users/x, /root).
func isUserHome(dir string) bool {
	if dir == "/root" {
		return true
	}
	parts := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	return len(parts) == 2 && (parts[0] == "home" || parts[0] == "Users")
}

// segmentCount counts the path segments of a relative path.
func segmentCount(relPath string) int {
	return strings.Count(relPath, "/") + 1
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
