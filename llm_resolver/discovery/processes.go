package discovery

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Pre-compiled regexes for port and PID extraction
var (
	portExtractRegex = regexp.MustCompile(`:(\d+)$`)
	pidExtractRegex  = regexp.MustCompile(`pid=(\d+)`)
	socketRegex      = regexp.MustCompile(`socket:\[(\d+)\]`)
)

// LocalProcess represents a discovered local process
type LocalProcess struct {
	Port      int    `json:"port"`
	PID       int    `json:"pid"`
	PPID      int    `json:"-"` // Parent PID, used for internal filtering only
	BindAddr  string `json:"-"` // Bind address (e.g., "0.0.0.0", "127.0.0.1"), used for deduplication
	Command   string `json:"command"`
	Args      string `json:"args"`
	Workdir   string `json:"workdir"`
}

// Processes to filter out (system/non-dev)
var ignoredCommands = map[string]bool{
	"docker-proxy":    true,
	"com.docker.vpnki": true,
	"vpnkit":          true,
	"code":            true,
	"code-helper":     true,
	"spotify":         true,
	"Spotify":         true,
	"jetbrains-toolb": true,
	"phpstorm":        true,
	"webstorm":        true,
	"idea":            true,
	"goland":          true,
	"chrome":          true,
	"chromium":        true,
	"Google Chrome":   true,
	"firefox":         true,
	"Firefox":         true,
	"Safari":          true,
	"slack":           true,
	"Slack":           true,
	"discord":         true,
	"Discord":         true,
	"telegram":        true,
	"Telegram":        true,
	"signal":          true,
	"Signal":          true,
	"zoom":            true,
	"zoom.us":         true,
	"cupsd":           true,
	"caddy":           true,
	"systemd":         true,
	"dbus-daemon":     true,
	"pulseaudio":      true,
	"pipewire":        true,
	"fsnotifier":      true,
	"launchd":         true,
	"mDNSResponder":   true,
	"rapportd":        true,
	"sharingd":        true,
	"identityservices": true,
}

// Workdirs that indicate container/system processes
var ignoredWorkdirs = map[string]bool{
	"/":     true,
	"/app":  true,
	"/srv":  true,
	"/root": true,
}

// Ports to always ignore (debug/inspection ports)
var ignoredPorts = map[int]bool{
	9229: true, // Node.js debug port
	9222: true, // Chrome DevTools Protocol
}

// Patterns in args that indicate non-dev processes
var ignoredArgsPatterns = []string{
	"jetbrains",
	"intellij",
	"java.rmi.server",
	"idea.home",
	"phpstorm",
	"webstorm",
	"goland",
	"rider",
	"clion",
	"datagrip",
	"rubymine",
	"pycharm",
	"android studio",
	"com.apple.",
	"apple.systempreferences",
}

// DiscoverLocalProcesses discovers locally running processes with open ports
func DiscoverLocalProcesses() ([]LocalProcess, error) {
	var processes []LocalProcess
	var err error

	if runtime.GOOS == "darwin" {
		// macOS: try lsof first, fallback to netstat.
		// Both already batch the per-PID info (args/workdir/ppid) into a single
		// ps + lsof call each, so PPID is already populated.
		processes, err = discoverWithLsof()
		if err != nil || len(processes) == 0 {
			processes, err = discoverWithNetstat()
		}
	} else {
		// Linux: try ss first, fallback to /proc
		processes, err = tryWithSs()
		if err != nil || len(processes) == 0 {
			processes, err = parseFromProc()
		}
	}

	if err != nil {
		return nil, err
	}

	// Filter out system/non-dev processes
	var filtered []LocalProcess
	for _, p := range processes {
		if ignoredCommands[p.Command] {
			continue
		}
		if p.Workdir != "" && ignoredWorkdirs[p.Workdir] {
			continue
		}
		// Check if args contain ignored patterns (case-insensitive)
		if shouldIgnoreByArgs(p.Args) {
			continue
		}
		// Filter out known debug/inspection ports
		if ignoredPorts[p.Port] {
			continue
		}
		filtered = append(filtered, p)
	}

	// On Linux, PPID is free to read from /proc, so fill it in here.
	// On macOS, PPID is already populated by the batched discovery call.
	if runtime.GOOS != "darwin" {
		for i := range filtered {
			filtered[i].PPID = getProcessPPID(filtered[i].PID)
		}
	}

	// Build set of PIDs
	pidSet := make(map[int]bool)
	for _, p := range filtered {
		pidSet[p.PID] = true
	}

	// Filter out child processes: keep only processes whose parent is NOT in our list
	var rootProcesses []LocalProcess
	for _, p := range filtered {
		if !pidSet[p.PPID] {
			rootProcesses = append(rootProcesses, p)
		}
	}

	// Deduplicate by PID: keep only one port per process
	// Prefer 0.0.0.0 (all interfaces) over 127.0.0.1 (localhost), then prefer lower port numbers
	pidToProcess := make(map[int]LocalProcess)
	for _, p := range rootProcesses {
		existing, exists := pidToProcess[p.PID]
		if !exists {
			pidToProcess[p.PID] = p
			continue
		}
		// Prefer 0.0.0.0 or * (all interfaces) over localhost bindings
		existingIsPublic := existing.BindAddr == "0.0.0.0" || existing.BindAddr == "*" || existing.BindAddr == "[::]"
		newIsPublic := p.BindAddr == "0.0.0.0" || p.BindAddr == "*" || p.BindAddr == "[::]"
		if newIsPublic && !existingIsPublic {
			pidToProcess[p.PID] = p
		} else if existingIsPublic == newIsPublic && p.Port < existing.Port {
			// Same binding type, prefer lower port
			pidToProcess[p.PID] = p
		}
	}

	// Convert map back to slice
	var deduplicated []LocalProcess
	for _, p := range pidToProcess {
		deduplicated = append(deduplicated, p)
	}

	return deduplicated, nil
}

// shouldIgnoreByArgs checks if process args contain ignored patterns
func shouldIgnoreByArgs(args string) bool {
	argsLower := strings.ToLower(args)
	for _, pattern := range ignoredArgsPatterns {
		if strings.Contains(argsLower, pattern) {
			return true
		}
	}
	return false
}

// macProcessInfo holds per-PID info batched on macOS.
type macProcessInfo struct {
	Args    string
	Workdir string
	PPID    int
}

// discoverWithLsof uses lsof to discover listening processes (macOS)
// lsof -iTCP -sTCP:LISTEN -n -P
func discoverWithLsof() ([]LocalProcess, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lsof", "-iTCP", "-sTCP:LISTEN", "-n", "-P")
	output, err := cmd.Output()
	if err != nil {
		// lsof may exit non-zero even with valid output (e.g., permission warnings
		// when running as root via launchd). Parse output if we got any.
		if len(output) == 0 {
			return nil, err
		}
	}

	// First pass: collect (port, pid, command, bindAddr) tuples.
	type candidate struct {
		port     int
		pid      int
		command  string
		bindAddr string
	}
	var candidates []candidate
	seenPorts := make(map[int]bool)
	lines := strings.Split(string(output), "\n")

	// lsof output format:
	// COMMAND   PID   USER   FD   TYPE   DEVICE SIZE/OFF NODE NAME
	// node    12345   user   23u  IPv4   0x...      0t0  TCP *:3000 (LISTEN)

	for _, line := range lines[1:] { // Skip header
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 9 {
			continue
		}

		command := parts[0]
		pid, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		// Parse the NAME field (last or second-to-last) for port
		// Format: *:3000 or 127.0.0.1:3000 or [::1]:3000
		name := parts[len(parts)-1]
		if name == "(LISTEN)" && len(parts) >= 10 {
			name = parts[len(parts)-2]
		}

		// Extract port from name
		portMatch := portExtractRegex.FindStringSubmatch(name)
		if len(portMatch) < 2 {
			continue
		}
		port, _ := strconv.Atoi(portMatch[1])

		if port < 1024 || seenPorts[port] {
			continue
		}
		seenPorts[port] = true

		// Extract bind address (everything before the last colon)
		bindAddr := ""
		if lastColon := strings.LastIndex(name, ":"); lastColon > 0 {
			bindAddr = name[:lastColon]
		}

		candidates = append(candidates, candidate{
			port:     port,
			pid:      pid,
			command:  command,
			bindAddr: bindAddr,
		})
	}

	// Second pass: collect unique PIDs and batch-fetch their info.
	pids := uniquePIDs(candidates, func(i int) int { return candidates[i].pid })
	info := getProcessInfoBatchMac(pids)

	// Third pass: assemble LocalProcess entries.
	processes := make([]LocalProcess, 0, len(candidates))
	for _, c := range candidates {
		pi := info[c.pid]
		processes = append(processes, LocalProcess{
			Port:     c.port,
			PID:      c.pid,
			PPID:     pi.PPID,
			BindAddr: c.bindAddr,
			Command:  c.command,
			Args:     cleanArgs(pi.Args),
			Workdir:  pi.Workdir,
		})
	}

	return processes, nil
}

// discoverWithNetstat is a fallback for macOS when lsof fails (e.g., in launchd services).
// Uses netstat -anv which reads from kernel network tables and includes PID info.
func discoverWithNetstat() ([]LocalProcess, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "netstat", "-anv", "-p", "tcp")
	output, err := cmd.Output()
	if err != nil {
		if len(output) == 0 {
			return nil, err
		}
	}

	// First pass: collect (port, pid, command, bindAddr) tuples.
	type candidate struct {
		port     int
		pid      int
		command  string
		bindAddr string
	}
	var candidates []candidate
	seenPorts := make(map[int]bool)
	lines := strings.Split(string(output), "\n")

	// netstat -anv output format:
	// tcp4   0   0  *.5173   *.*   LISTEN   ...   node:5588   ...
	// Fields vary but we look for LISTEN state and command:PID at the end

	for _, line := range lines {
		if !strings.Contains(line, "LISTEN") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 9 {
			continue
		}

		// Local address is typically parts[3], format: *.port or addr.port
		localAddr := parts[3]

		// Extract port (last component after the last dot)
		lastDot := strings.LastIndex(localAddr, ".")
		if lastDot < 0 {
			continue
		}
		port, err := strconv.Atoi(localAddr[lastDot+1:])
		if err != nil || port < 1024 || seenPorts[port] {
			continue
		}
		seenPorts[port] = true

		// Extract bind address
		bindAddr := localAddr[:lastDot]
		if bindAddr == "*" {
			bindAddr = "0.0.0.0"
		}

		// Find command:PID field (format: "node:5588" or "bun:5773")
		var command string
		var pid int
		for _, field := range parts[8:] {
			if idx := strings.LastIndex(field, ":"); idx > 0 {
				name := field[:idx]
				if p, err := strconv.Atoi(field[idx+1:]); err == nil && p > 0 {
					command = name
					pid = p
					break
				}
			}
		}
		if pid == 0 {
			continue
		}

		candidates = append(candidates, candidate{
			port:     port,
			pid:      pid,
			command:  command,
			bindAddr: bindAddr,
		})
	}

	// Second pass: collect unique PIDs and batch-fetch their info.
	pids := uniquePIDs(candidates, func(i int) int { return candidates[i].pid })
	info := getProcessInfoBatchMac(pids)

	// Third pass: assemble LocalProcess entries.
	processes := make([]LocalProcess, 0, len(candidates))
	for _, c := range candidates {
		pi := info[c.pid]
		processes = append(processes, LocalProcess{
			Port:     c.port,
			PID:      c.pid,
			PPID:     pi.PPID,
			BindAddr: c.bindAddr,
			Command:  c.command,
			Args:     cleanArgs(pi.Args),
			Workdir:  pi.Workdir,
		})
	}

	return processes, nil
}

// uniquePIDs returns a slice of unique PIDs from a slice of candidates,
// using getPID to extract the PID from each element.
func uniquePIDs[T any](items []T, getPID func(int) int) []int {
	seen := make(map[int]bool, len(items))
	pids := make([]int, 0, len(items))
	for i := range items {
		pid := getPID(i)
		if pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		pids = append(pids, pid)
	}
	return pids
}

// getProcessInfoBatchMac fetches args, workdir, and ppid for a list of PIDs
// using a single `ps` call and a single `lsof` call. Returns a map keyed by PID.
//
// `ps -p pid1,pid2,... -o pid=,ppid=,args=` returns one line per PID with the
// fields requested (pid, ppid, args).
//
// `lsof -a -p pid1,pid2,... -Fpn -d cwd` returns interleaved `p<pid>` /
// `n<path>` records for each PID.
func getProcessInfoBatchMac(pids []int) map[int]macProcessInfo {
	result := make(map[int]macProcessInfo, len(pids))
	if len(pids) == 0 {
		return result
	}

	pidArgs := make([]string, len(pids))
	for i, p := range pids {
		pidArgs[i] = strconv.Itoa(p)
	}
	pidList := strings.Join(pidArgs, ",")

	// Batched ps call: pid, ppid, args.
	psCtx, psCancel := context.WithTimeout(context.Background(), commandTimeout)
	defer psCancel()

	psCmd := exec.CommandContext(psCtx, "ps", "-p", pidList, "-o", "pid=,ppid=,args=")
	psOutput, err := psCmd.Output()
	if err == nil || len(psOutput) > 0 {
		for _, line := range strings.Split(string(psOutput), "\n") {
			line = strings.TrimLeft(line, " ")
			if line == "" {
				continue
			}
			// Parse: "<pid> <ppid> <args...>"
			// pid is the first whitespace-separated token, ppid is the second,
			// the remainder is args (may contain whitespace).
			sp1 := strings.IndexByte(line, ' ')
			if sp1 < 0 {
				continue
			}
			pid, err := strconv.Atoi(line[:sp1])
			if err != nil {
				continue
			}
			rest := strings.TrimLeft(line[sp1+1:], " ")
			sp2 := strings.IndexByte(rest, ' ')
			if sp2 < 0 {
				// No args, just ppid.
				ppid, _ := strconv.Atoi(strings.TrimSpace(rest))
				info := result[pid]
				info.PPID = ppid
				result[pid] = info
				continue
			}
			ppid, _ := strconv.Atoi(rest[:sp2])
			args := strings.TrimSpace(rest[sp2+1:])
			info := result[pid]
			info.PPID = ppid
			info.Args = args
			result[pid] = info
		}
	}

	// Batched lsof call: cwd for each PID. -Fpn outputs `p<pid>` then `n<path>`.
	lsofCtx, lsofCancel := context.WithTimeout(context.Background(), commandTimeout)
	defer lsofCancel()

	lsofCmd := exec.CommandContext(lsofCtx, "lsof", "-a", "-p", pidList, "-Fpn", "-d", "cwd")
	lsofOutput, err := lsofCmd.Output()
	if err == nil || len(lsofOutput) > 0 {
		var currentPID int
		for _, line := range strings.Split(string(lsofOutput), "\n") {
			if line == "" {
				continue
			}
			switch line[0] {
			case 'p':
				if p, err := strconv.Atoi(line[1:]); err == nil {
					currentPID = p
				} else {
					currentPID = 0
				}
			case 'n':
				if currentPID == 0 {
					continue
				}
				info := result[currentPID]
				info.Workdir = line[1:]
				result[currentPID] = info
			}
		}
	}

	return result
}

// tryWithSs uses the ss command to discover processes (Linux, faster)
func tryWithSs() ([]LocalProcess, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ss", "-tlnp")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var processes []LocalProcess
	lines := strings.Split(string(output), "\n")

	// Skip header line
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 5 {
			continue
		}

		localAddr := parts[3]
		processInfo := strings.Join(parts[5:], " ")

		// Extract port from local address (format: "0.0.0.0:5174" or "127.0.0.1:34549" or "*:3000")
		portMatch := portExtractRegex.FindStringSubmatch(localAddr)
		if len(portMatch) < 2 {
			continue
		}
		port, _ := strconv.Atoi(portMatch[1])
		if port < 1024 {
			continue
		}

		// Extract bind address (everything before the last colon)
		bindAddr := ""
		if lastColon := strings.LastIndex(localAddr, ":"); lastColon > 0 {
			bindAddr = localAddr[:lastColon]
		}

		// Extract PID from process info
		pidMatch := pidExtractRegex.FindStringSubmatch(processInfo)
		if len(pidMatch) < 2 {
			continue
		}

		pid, _ := strconv.Atoi(pidMatch[1])

		// Get command from /proc instead of ss output (ss shows thread name which is often unhelpful)
		command := getProcessCommand(pid)
		args := cleanArgs(getProcessArgs(pid))
		workdir := getProcessWorkdir(pid)

		processes = append(processes, LocalProcess{
			Port:     port,
			PID:      pid,
			BindAddr: bindAddr,
			Command:  command,
			Args:     args,
			Workdir:  workdir,
		})
	}

	return processes, nil
}

// parseFromProc parses /proc/net/tcp to discover processes (Linux, slower fallback)
func parseFromProc() ([]LocalProcess, error) {
	var processes []LocalProcess
	seenPorts := make(map[int]bool)

	// Build inode -> pid map first
	inodeToPid := buildInodePidMap()

	for _, tcpFile := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		procs, err := parseTcpFile(tcpFile, seenPorts, inodeToPid)
		if err != nil {
			continue
		}
		processes = append(processes, procs...)
	}

	return processes, nil
}

// parseTcpFile parses a single /proc/net/tcp file
func parseTcpFile(tcpFile string, seenPorts map[int]bool, inodeToPid map[string]int) ([]LocalProcess, error) {
	file, err := os.Open(tcpFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var processes []LocalProcess
	scanner := bufio.NewScanner(file)
	// Skip header
	scanner.Scan()

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 10 {
			continue
		}

		localAddr := parts[1]
		state := parts[3]

		// Only LISTEN state (0A)
		if state != "0A" {
			continue
		}

		// Extract port (hex) and address
		addrParts := strings.Split(localAddr, ":")
		if len(addrParts) < 2 {
			continue
		}
		port64, _ := strconv.ParseInt(addrParts[1], 16, 32)
		port := int(port64)

		// Parse hex IP address to determine bind type
		hexAddr := addrParts[0]
		bindAddr := "127.0.0.1" // default
		if hexAddr == "00000000" || hexAddr == "00000000000000000000000000000000" {
			bindAddr = "0.0.0.0"
		}

		if port < 1024 || seenPorts[port] {
			continue
		}
		seenPorts[port] = true

		inode := parts[9]
		pid, ok := inodeToPid[inode]
		if !ok {
			continue
		}

		command := getProcessCommand(pid)
		args := cleanArgs(getProcessArgs(pid))
		workdir := getProcessWorkdir(pid)

		processes = append(processes, LocalProcess{
			Port:     port,
			PID:      pid,
			BindAddr: bindAddr,
			Command:  command,
			Args:     args,
			Workdir:  workdir,
		})
	}

	return processes, nil
}

// buildInodePidMap builds a map of socket inode -> PID (Linux only)
func buildInodePidMap() map[string]int {
	result := make(map[string]int)

	// Read /proc directory
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory name is a PID (all digits)
		name := entry.Name()
		pid, err := strconv.Atoi(name)
		if err != nil {
			continue
		}

		// Read fd directory
		fdDir := filepath.Join("/proc", name, "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}

		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}

			if match := socketRegex.FindStringSubmatch(link); len(match) > 1 {
				result[match[1]] = pid
			}
		}
	}

	return result
}

// getProcessCommand gets the command name for a PID (Linux)
func getProcessCommand(pid int) string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// getProcessArgs gets the command line arguments for a PID (Linux)
func getProcessArgs(pid int) string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return ""
	}
	// Replace null bytes with spaces
	args := strings.ReplaceAll(string(data), "\x00", " ")
	return strings.TrimSpace(args)
}

// getProcessWorkdir gets the working directory for a PID (Linux)
func getProcessWorkdir(pid int) string {
	link, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
	if err != nil {
		return ""
	}
	return link
}

// getProcessPPID gets the parent PID for a process (Linux)
func getProcessPPID(pid int) int {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			ppid, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
			return ppid
		}
	}
	return 0
}

// cleanArgs cleans up command line args for better readability
// - Removes full paths to node_modules/.bin/, keeping just the binary name
// - Removes common interpreter paths
func cleanArgs(args string) string {
	if args == "" {
		return args
	}

	parts := strings.Split(args, " ")
	var cleaned []string

	for i, part := range parts {
		// Skip empty parts
		if part == "" {
			continue
		}

		// For the first part (command), simplify common patterns
		if i == 0 {
			// node/bun/python etc - keep as is
			if part == "node" || part == "bun" || part == "python" || part == "python3" ||
				part == "ruby" || part == "php" || part == "java" || part == "go" {
				cleaned = append(cleaned, part)
				continue
			}
			// Full path to interpreter - extract just the name
			if strings.HasPrefix(part, "/") {
				part = filepath.Base(part)
			}
		}

		// For other args, clean up node_modules/.bin paths
		if strings.Contains(part, "node_modules/.bin/") {
			// Extract just the binary name after node_modules/.bin/
			idx := strings.Index(part, "node_modules/.bin/")
			part = part[idx+len("node_modules/.bin/"):]
		} else if strings.Contains(part, "node_modules/") && strings.HasSuffix(part, "/bin/") {
			// Handle other node_modules bin patterns
			part = filepath.Base(strings.TrimSuffix(part, "/"))
		}

		cleaned = append(cleaned, part)
	}

	return strings.Join(cleaned, " ")
}
