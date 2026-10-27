package policy

import (
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Sensitive operations are those that untrusted content (a README, an
// issue, a web page, an MCP result, a tool's output) must never be able to
// trigger on its own: reading credentials, sending data off the machine,
// installing packages, changing what instructs the agent, changing what
// git or the shell will execute later, gaining privileges, and changing
// CI or verification configuration. They are allowed, but only with the
// user's authorization (see internal/direct's guard); CheckCommand still
// refuses the destructive ones outright.
//
// Classification works on the shell command line and on paths, not on any
// programming language: a Python, Go, Rust or Java project, a shell script
// or a Terraform tree are judged alike. It resolves quoting and escapes,
// command and process substitution, simple variable assignments, globs,
// wrappers (env, sudo, timeout, xargs, nohup, …), sh -c and eval bodies,
// find -exec, and interpreter one-liners (python -c, node -e, …), so
// alternate spellings of the same operation are classified the same way.

// Category is a kind of sensitive operation.
type Category string

// Categories of sensitive operations.
const (
	CatCredentials  Category = "credentials"  // reading secret stores or the environment
	CatNetwork      Category = "network"      // sending or fetching data off the machine
	CatInstall      Category = "install"      // installing packages named on the command line
	CatInstructions Category = "instructions" // agent instruction files, memory, skills, MCP and agent config
	CatExecConfig   Category = "exec-config"  // git hooks and config, hook managers, shell startup, schedulers
	CatPrivilege    Category = "privilege"    // sudo, su, doas
	CatProtected    Category = "protected"    // CI and verification configuration (IsProtectedPath)
)

// Finding is one sensitive aspect of an operation.
type Finding struct {
	Category Category
	// Subject is what it concerns: a path, a host, a package, a git key.
	Subject string
	Reason  string
}

// CommandContext locates a command: paths in it are resolved against Dir
// and judged relative to Root (the project) or Home.
type CommandContext struct {
	Root      string
	Dir       string
	Home      string
	ConfigDir string // BoundedCode's configuration (user instructions, memory, skills, config.yaml)
	// Glob expands a pattern to the paths it matches (filepath.Glob when
	// nil); tests replace it.
	Glob func(pattern string) ([]string, error)
	// Written reports whether the agent wrote the file at abs during this
	// session. A script it wrote and then runs is classified by its
	// contents (write a script, then run it, is the same operation);
	// project code it did not write is not. ReadFile reads it.
	Written  func(abs string) bool
	ReadFile func(abs string) ([]byte, error)
}

// ClassifyPath returns the findings for changing the project file rel
// (slash-separated, relative to the project root).
func ClassifyPath(rel string) []Finding {
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	var out []Finding
	if isInstructionPath(rel) {
		out = append(out, Finding{CatInstructions, rel, "it instructs or configures the agent"})
	}
	if isExecConfigPath(rel) {
		out = append(out, Finding{CatExecConfig, rel, "git, a hook manager or the editor executes what it configures"})
	}
	if IsProtectedPath(rel) {
		out = append(out, Finding{CatProtected, rel, "it controls CI or how changes are verified"})
	}
	return out
}

// instructionNames are agent instruction files, at any depth.
var instructionNames = map[string]bool{
	"agents.md": true, "agent.md": true, "claude.md": true, "gemini.md": true, "boundedcode.md": true,
	".cursorrules": true, ".windsurfrules": true, "copilot-instructions.md": true,
}

// instructionDirs configure agents: skills, commands, sub-agents, MCP servers.
var instructionPrefixes = []string{
	".boundedcode/instructions.md", ".boundedcode/skills/", ".boundedcode/commands/", ".boundedcode/agents/",
	".boundedcode/mcp.json", ".mcp.json", ".claude/", ".agents/", ".cursor/rules/", ".github/instructions/",
	".vscode/mcp.json", ".gemini/", ".codex/",
}

func isInstructionPath(rel string) bool {
	if instructionNames[strings.ToLower(path.Base(rel))] {
		return true
	}
	return hasPrefixPath(rel, instructionPrefixes)
}

// execConfigPrefixes configure what runs later: git hooks and config,
// hook managers, editor tasks.
var execConfigPrefixes = []string{
	".git/hooks/", ".git/config", ".git/info/attributes", ".githooks/", ".husky/", ".pre-commit-config.yaml",
	".lefthook.yml", "lefthook.yml", "lefthook.yaml", ".gitattributes", ".vscode/tasks.json", ".envrc",
}

func isExecConfigPath(rel string) bool {
	return hasPrefixPath(rel, execConfigPrefixes) || strings.HasSuffix(rel, "/.gitattributes")
}

func hasPrefixPath(rel string, prefixes []string) bool {
	low := strings.ToLower(rel)
	for _, p := range prefixes {
		p = strings.ToLower(p)
		if strings.HasSuffix(p, "/") {
			if low+"/" == p || strings.HasPrefix(low, p) {
				return true
			}
		} else if low == p {
			return true
		}
	}
	return false
}

// homeCredentials are secret stores under the home directory, beyond what
// IsSecretPath recognises by name.
var homeCredentials = []string{
	".ssh/", ".aws/", ".azure/", ".config/gcloud/", ".kube/", ".docker/config.json", ".netrc", ".npmrc", ".pypirc",
	".git-credentials", ".gnupg/", ".config/gh/", ".password-store/", ".local/share/keyrings/", "Library/Keychains/",
	".cargo/credentials", ".cargo/credentials.toml", ".gem/credentials", ".config/hub", ".terraform.d/credentials.tfrc.json",
}

// homeExecConfig run on login, in every shell, or on a schedule.
var homeExecConfig = []string{
	".bashrc", ".bash_profile", ".bash_login", ".profile", ".zshrc", ".zprofile", ".zshenv", ".zlogin", ".config/fish/",
	".gitconfig", ".config/git/", ".config/autostart/", ".config/systemd/user/", "Library/LaunchAgents/", ".local/bin/",
	".config/powershell/", "Documents/PowerShell/",
}

// homeAgentConfig configures coding agents on this machine.
var homeAgentConfig = []string{".claude/", ".codex/", ".config/opencode/", ".gemini/", ".cursor/"}

var systemCredentials = regexp.MustCompile(`^/(etc/(shadow|gshadow|sudoers)|proc/[^/]+/environ|var/run/secrets/)`)
var systemExecConfig = regexp.MustCompile(`^/(etc/(cron|profile|bash\.bashrc|zsh|systemd|init\.d|rc\.local|ld\.so\.preload)|var/spool/cron)`)

// ClassifyCommand returns the findings for running a shell command line.
func ClassifyCommand(line string, cc CommandContext) []Finding {
	c := classifier{cc: cc, seen: map[string]bool{}}
	c.line(line, 0)
	sort.SliceStable(c.out, func(i, j int) bool { return c.out[i].Category < c.out[j].Category })
	return c.out
}

type classifier struct {
	cc   CommandContext
	out  []Finding
	seen map[string]bool
}

func (c *classifier) add(f Finding) {
	key := string(f.Category) + "\x00" + f.Subject
	if !c.seen[key] {
		c.seen[key] = true
		c.out = append(c.out, f)
	}
}

const maxDepth = 6

// line classifies a command line (nested substitutions included).
func (c *classifier) line(line string, depth int) {
	if depth > maxDepth {
		return
	}
	if devSocket.MatchString(line) {
		c.add(Finding{CatNetwork, devSocket.FindString(line), "a /dev/tcp or /dev/udp socket"})
	}
	vars := assignments(line)
	cmds, nested := lexShell(line)
	for _, n := range nested {
		c.line(n, depth+1)
	}
	for _, words := range cmds {
		for i := range words {
			words[i].text = expandVars(words[i].text, vars, c.cc.Home)
		}
		c.simple(words, depth)
	}
}

// simple classifies one simple command.
func (c *classifier) simple(ws []word, depth int) {
	var args, writes, reads []string
	for _, w := range ws {
		switch w.redirect {
		case '>':
			writes = append(writes, w.text)
		case '<':
			reads = append(reads, w.text)
		default:
			args = append(args, w.text)
		}
	}
	// Leading assignments and wrappers.
	for len(args) > 0 {
		a := args[0]
		if assignWord.MatchString(a) {
			args = args[1:]
			continue
		}
		tool := toolName(a)
		if privilegeTools[tool] {
			c.add(Finding{CatPrivilege, tool, "runs with raised privileges"})
		}
		if tool == "su" {
			// su -c 'cmd' runs a command line.
			for i := 1; i+1 < len(args); i++ {
				if args[i] == "-c" {
					c.line(args[i+1], depth+1)
				}
			}
			return
		}
		if !shellWrappers[tool] && !privilegeTools[tool] {
			break
		}
		args = args[1:]
		for len(args) > 0 && (strings.HasPrefix(args[0], "-") || durationArg.MatchString(args[0]) || assignWord.MatchString(args[0])) {
			// A flag that takes a value (sudo -u root, nice -n 10, timeout -s KILL).
			if flagTakesValue(tool, args[0]) && len(args) > 1 {
				args = args[1:]
			}
			args = args[1:]
		}
		if tool == "env" && len(args) == 0 {
			c.add(Finding{CatCredentials, "environment", "env prints environment variables"})
		}
	}
	// Reading and writing files: every path the command names (commands
	// that only name a file, ls or echo, do not read it).
	if len(args) == 0 || !nameOnlyTools[toolName(args[0])] {
		for _, p := range append(append([]string{}, reads...), args...) {
			c.pathRead(p)
		}
	} else {
		for _, p := range reads {
			c.pathRead(p)
		}
	}
	for _, p := range writes {
		c.pathWrite(p, "written by a redirection")
	}
	if len(args) == 0 {
		return
	}
	tool := toolName(args[0])
	rest := args[1:]
	switch {
	case shells[tool]:
		if code, ok := flagValue(rest, "-c"); ok {
			c.line(code, depth+1)
			return
		}
	case tool == "eval" || tool == "source" || tool == ".":
		if tool == "eval" {
			c.line(strings.Join(rest, " "), depth+1)
			return
		}
	case tool == "find":
		for i, a := range rest {
			if a == "-exec" || a == "-execdir" || a == "-ok" || a == "-okdir" {
				var sub []string
				for _, b := range rest[i+1:] {
					if b == ";" || b == "\\;" || b == "+" {
						break
					}
					sub = append(sub, b)
				}
				c.line(strings.Join(sub, " "), depth+1)
			}
		}
	case interpreters[tool]:
		for _, flag := range []string{"-c", "-e", "-r", "--eval", "-Command", "-command", "-EncodedCommand"} {
			if code, ok := flagValue(rest, flag); ok {
				c.code(code)
			}
		}
	}
	c.script(tool, args, depth)
	c.network(tool, rest)
	c.install(tool, rest)
	c.credentialsCommand(tool, rest)
	c.execConfig(tool, rest)
	if !readOnly(tool, rest) {
		for _, p := range rest {
			c.pathWrite(p, "changed by "+tool)
		}
	}
}

// nameOnlyTools take paths without reading the files' contents.
var nameOnlyTools = map[string]bool{"ls": true, "stat": true, "test": true, "[": true, "realpath": true, "readlink": true,
	"dirname": true, "basename": true, "echo": true, "printf": true, "du": true, "mkdir": true, "touch": true, "file": true,
	"get-childitem": true, "gci": true, "dir": true, "test-path": true}

// script classifies a script the agent wrote in this session when the
// command runs it: sh x.sh, ./x.sh, source x.sh, python x.py, npm run x
// (with a package.json it wrote).
func (c *classifier) script(tool string, args []string, depth int) {
	if c.cc.Written == nil || c.cc.ReadFile == nil {
		return
	}
	read := func(p string) (string, bool) {
		_, _, _, _, abs := c.resolve(p)
		if abs == "" || !c.cc.Written(abs) {
			return "", false
		}
		b, err := c.cc.ReadFile(abs)
		if err != nil {
			return "", false
		}
		if len(b) > 256<<10 {
			b = b[:256<<10]
		}
		return string(b), true
	}
	rest := args[1:]
	switch {
	case shells[tool] || tool == "source" || tool == ".":
		if f := firstNonFlag(rest); f != "" {
			if body, ok := read(f); ok {
				c.line(body, depth+1)
			}
		}
	case interpreters[tool]:
		if f := firstNonFlag(rest); f != "" && !strings.HasPrefix(f, "-") {
			if body, ok := read(f); ok {
				c.code(body)
			}
		}
	case tool == "npm" || tool == "pnpm" || tool == "yarn" || tool == "bun":
		name := firstNonFlag(rest)
		if name == "run" || name == "run-script" {
			name = firstNonFlag(afterFirst(rest, name))
		}
		if body, ok := read("package.json"); ok && name != "" {
			if m := regexp.MustCompile(`"` + regexp.QuoteMeta(name) + `"\s*:\s*"((?:[^"\\]|\\.)*)"`).FindStringSubmatch(body); m != nil {
				c.line(strings.ReplaceAll(m[1], `\"`, `"`), depth+1)
			}
		}
	default:
		// ./x.sh or a path to a script that has a shebang.
		if strings.Contains(args[0], "/") {
			if body, ok := read(args[0]); ok {
				if first, _, _ := strings.Cut(body, "\n"); strings.HasPrefix(first, "#!") && regexp.MustCompile(`\b(sh|bash|zsh|dash|ksh)\b`).MatchString(first) {
					c.line(body, depth+1)
				} else {
					c.code(body)
				}
			}
		}
	}
}

// code scans an interpreter one-liner (any language) as text.
func (c *classifier) code(code string) {
	for _, u := range urlRE.FindAllString(code, -1) {
		if h := hostOf(u); h != "" && !localHost(h) {
			c.add(Finding{CatNetwork, h, "an interpreter one-liner addresses " + h})
		}
	}
	if socketCode.MatchString(code) {
		c.add(Finding{CatNetwork, "", "an interpreter one-liner opens a network connection"})
	}
	for _, w := range pathWord.FindAllString(code, -1) {
		c.pathRead(w)
		c.pathWrite(w, "named in an interpreter one-liner")
	}
}

// resolve returns a path word as (relative-to-root, inside), (relative-
// to-home, inHome) and its absolute form.
func (c *classifier) resolve(p string) (rel string, inRoot bool, homeRel string, inHome bool, abs string) {
	p = strings.TrimSpace(p)
	if p == "" || strings.Contains(p, "://") {
		return
	}
	if strings.HasPrefix(p, "~/") || p == "~" {
		p = filepath.Join(c.cc.Home, strings.TrimPrefix(p, "~"))
	}
	abs = p
	if !filepath.IsAbs(abs) {
		dir := c.cc.Dir
		if dir == "" {
			dir = c.cc.Root
		}
		abs = filepath.Join(dir, p)
	}
	abs = filepath.Clean(abs)
	if c.cc.Root != "" {
		if r, err := filepath.Rel(c.cc.Root, abs); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			rel, inRoot = filepath.ToSlash(r), true
		}
	}
	if c.cc.Home != "" {
		if r, err := filepath.Rel(c.cc.Home, abs); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			homeRel, inHome = filepath.ToSlash(r), true
		}
	}
	return
}

// expand returns the paths a word names, globs expanded.
func (c *classifier) expand(p string) []string {
	if !strings.ContainsAny(p, "*?[") {
		return c.links([]string{p})
	}
	glob := c.cc.Glob
	if glob == nil {
		glob = filepath.Glob
	}
	pattern := p
	if !filepath.IsAbs(pattern) {
		dir := c.cc.Dir
		if dir == "" {
			dir = c.cc.Root
		}
		pattern = filepath.Join(dir, pattern)
	}
	m, err := glob(pattern)
	if err != nil || len(m) == 0 {
		return c.links([]string{p})
	}
	if len(m) > 200 {
		m = m[:200]
	}
	return c.links(append(m, p))
}

// links adds the targets of symbolic links: a link to .env is .env.
func (c *classifier) links(ps []string) []string {
	out := ps
	for _, p := range ps {
		_, _, _, _, abs := c.resolve(p)
		if abs == "" {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
			out = append(out, real)
		}
	}
	return out
}

func (c *classifier) pathRead(word string) {
	for _, p := range c.expand(word) {
		rel, inRoot, homeRel, inHome, abs := c.resolve(p)
		switch {
		case inRoot && rel != "." && IsSecretPath(rel):
			c.add(Finding{CatCredentials, rel, "a secret store"})
		case inHome && !inRoot && (IsSecretPath(homeRel) || hasPrefixPath(homeRel, homeCredentials)):
			c.add(Finding{CatCredentials, "~/" + homeRel, "a credential store in the home directory"})
		case abs != "" && systemCredentials.MatchString(filepath.ToSlash(abs)):
			c.add(Finding{CatCredentials, abs, "a system credential store"})
		}
	}
}

func (c *classifier) pathWrite(word, how string) {
	for _, p := range c.expand(word) {
		rel, inRoot, homeRel, inHome, abs := c.resolve(p)
		if inRoot && rel != "." {
			for _, f := range ClassifyPath(rel) {
				f.Reason += " (" + how + ")"
				c.add(f)
			}
		}
		if c.cc.ConfigDir != "" {
			if r, err := filepath.Rel(c.cc.ConfigDir, abs); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				c.add(Finding{CatInstructions, abs, "BoundedCode's configuration, instructions or memory (" + how + ")"})
			}
		}
		if inHome && !inRoot {
			switch {
			case hasPrefixPath(homeRel, homeExecConfig):
				c.add(Finding{CatExecConfig, "~/" + homeRel, "runs in every shell, at login or on a schedule (" + how + ")"})
			case hasPrefixPath(homeRel, homeAgentConfig):
				c.add(Finding{CatInstructions, "~/" + homeRel, "configures a coding agent on this machine (" + how + ")"})
			}
		}
		if abs != "" && systemExecConfig.MatchString(filepath.ToSlash(abs)) {
			c.add(Finding{CatExecConfig, abs, "system start-up or scheduler configuration (" + how + ")"})
		}
	}
}

var (
	urlRE      = regexp.MustCompile(`(?i)\b(?:https?|ftp|wss?)://[^\s'"<>()]+`)
	devSocket  = regexp.MustCompile(`/dev/(tcp|udp)/[^\s/]+`)
	socketCode = regexp.MustCompile(`(?i)\b(socket\.(socket|create_connection)|net\.(connect|createConnection)|IO::Socket|TCPSocket|fsockopen|new-object\s+net\.(webclient|sockets)|http\.(get|request)\(|fetch\(|urlopen\(|requests\.(get|post|put))`)
	pathWord   = regexp.MustCompile(`[~\w./-]*[./][\w./-]+`)
	assignWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	scpTarget  = regexp.MustCompile(`^(?:[^@\s/]+@)?([A-Za-z0-9.-]+):`)
)

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true, "ash": true, "busybox": true}

var interpreters = map[string]bool{"python": true, "python3": true, "python2": true, "node": true, "nodejs": true, "deno": true, "bun": true,
	"ruby": true, "perl": true, "php": true, "pwsh": true, "powershell": true, "lua": true, "tclsh": true, "osascript": true}

var shellWrappers = map[string]bool{"env": true, "nohup": true, "time": true, "nice": true, "ionice": true, "timeout": true, "xargs": true,
	"exec": true, "command": true, "builtin": true, "stdbuf": true, "unbuffer": true, "chrt": true, "taskset": true, "setsid": true,
	"flock": true, "watch": true, "caffeinate": true, "strace": true, "ltrace": true}

var privilegeTools = map[string]bool{"sudo": true, "doas": true, "su": true, "pkexec": true, "runas": true, "gsudo": true}

func flagTakesValue(tool, flag string) bool {
	switch tool {
	case "sudo", "doas":
		return flag == "-u" || flag == "-g" || flag == "-C" || flag == "-D"
	case "nice", "ionice":
		return flag == "-n" || flag == "-c"
	case "timeout":
		return flag == "-s" || flag == "-k" || flag == "--signal" || flag == "--kill-after"
	case "xargs":
		return flag == "-I" || flag == "-n" || flag == "-P" || flag == "-L" || flag == "-d" || flag == "-s" || flag == "-E"
	case "env":
		return flag == "-u" || flag == "-C" || flag == "--unset" || flag == "--chdir"
	case "flock":
		return flag == "-w" || flag == "-E"
	case "watch":
		return flag == "-n"
	}
	return false
}

func toolName(w string) string {
	w = strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(w, "\\", "/"))), ".exe")
	return w
}

func flagValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
		// Combined short flags: -lc, -ec.
		if len(flag) == 2 && len(a) > 2 && a[0] == '-' && a[1] != '-' && strings.HasSuffix(a, flag[1:]) && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// readOnlyTools only read the files they name (unless a flag says
// otherwise, see readOnly).
var readOnlyTools = map[string]bool{"cat": true, "head": true, "tail": true, "less": true, "more": true, "grep": true, "egrep": true,
	"fgrep": true, "rg": true, "ag": true, "ack": true, "wc": true, "diff": true, "cmp": true, "file": true, "stat": true, "ls": true,
	"tree": true, "sha256sum": true, "sha1sum": true, "md5sum": true, "shasum": true, "jq": true, "sort": true, "uniq": true,
	"cut": true, "nl": true, "od": true, "xxd": true, "hexdump": true, "strings": true, "bat": true, "view": true, "realpath": true,
	"readlink": true, "dirname": true, "basename": true, "test": true, "[": true, "echo": true, "printf": true, "type": true,
	"get-content": true, "gc": true, "select-string": true, "get-childitem": true, "gci": true, "dir": true}

func readOnly(tool string, args []string) bool {
	switch tool {
	case "sed", "perl", "yq", "awk", "gawk":
		for _, a := range args {
			if a == "-i" || strings.HasPrefix(a, "-i") && tool != "awk" || a == "--in-place" || strings.Contains(a, "inplace") {
				return false
			}
		}
		return tool == "sed" || tool == "yq" || tool == "awk" || tool == "gawk"
	case "git":
		sub := gitSubcommand(args)
		switch sub {
		case "log", "show", "diff", "blame", "status", "ls-files", "grep", "rev-parse", "cat-file", "shortlog", "describe":
			return true
		}
		return false
	}
	return readOnlyTools[tool]
}

// network: clients that move data off the machine.
var netTools = map[string]bool{"curl": true, "wget": true, "nc": true, "ncat": true, "netcat": true, "socat": true, "telnet": true,
	"ssh": true, "scp": true, "sftp": true, "ftp": true, "rsync": true, "http": true, "https": true, "xh": true, "aria2c": true,
	"httpie": true, "invoke-webrequest": true, "iwr": true, "invoke-restmethod": true, "irm": true, "start-bitstransfer": true,
	"lftp": true, "tftp": true, "smbclient": true, "rclone": true, "gsutil": true, "s3cmd": true}

func (c *classifier) network(tool string, args []string) {
	var hosts []string
	for _, a := range args {
		if u := urlRE.FindString(a); u != "" {
			hosts = append(hosts, hostOf(u))
		}
	}
	if httpClients[tool] && len(hosts) == 0 {
		// A target without a scheme: curl localhost:8080/health.
		if t := firstNonFlagValue(args, httpFlagWithValue); t != "" {
			hosts = append(hosts, hostOf("http://"+t))
		}
	}
	switch {
	case netTools[tool]:
		if tool == "ssh" || tool == "scp" || tool == "sftp" || tool == "rsync" || tool == "rclone" {
			for _, a := range args {
				if strings.HasPrefix(a, "-") {
					continue
				}
				if m := scpTarget.FindStringSubmatch(a); m != nil {
					hosts = append(hosts, strings.ToLower(m[1]))
				} else if tool == "ssh" || tool == "sftp" {
					hosts = append(hosts, strings.ToLower(strings.TrimPrefix(a[strings.LastIndex(a, "@")+1:], "")))
					break
				}
			}
			if (tool == "rsync" || tool == "scp") && len(hosts) == 0 {
				return // local copy
			}
		}
		if tool == "nc" || tool == "ncat" || tool == "netcat" || tool == "telnet" || tool == "socat" {
			for _, a := range args {
				if !strings.HasPrefix(a, "-") && !portArg.MatchString(a) {
					hosts = append(hosts, strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(a, "tcp:"), "TCP:")))
					break
				}
			}
		}
		h := firstRemote(hosts)
		if len(hosts) > 0 && h == "" {
			return // every target is this machine
		}
		c.add(Finding{CatNetwork, h, tool + " sends or fetches data over the network"})
	case tool == "git":
		switch gitSubcommand(args) {
		case "clone", "fetch", "pull", "ls-remote", "remote", "submodule", "archive":
			if h := firstRemote(hosts); h != "" {
				c.add(Finding{CatNetwork, h, "git talks to " + h})
			}
		}
	case tool == "docker" || tool == "podman":
		switch firstNonFlag(args) {
		case "login", "push":
			c.add(Finding{CatNetwork, firstRemote(hosts), tool + " sends to a registry"})
		}
	}
}

var portArg = regexp.MustCompile(`^\d+$`)

var httpClients = map[string]bool{"curl": true, "wget": true, "http": true, "https": true, "xh": true, "aria2c": true,
	"invoke-webrequest": true, "iwr": true, "invoke-restmethod": true, "irm": true}

// httpFlagWithValue are client flags whose value is not the target.
var httpFlagWithValue = map[string]bool{"-d": true, "--data": true, "--data-binary": true, "--data-raw": true, "--data-urlencode": true,
	"-F": true, "--form": true, "-H": true, "--header": true, "-o": true, "--output": true, "-O": true, "-X": true, "--request": true,
	"-u": true, "--user": true, "-T": true, "--upload-file": true, "-A": true, "--user-agent": true, "-e": true, "--referer": true,
	"-b": true, "--cookie": true, "-c": true, "--cookie-jar": true, "-w": true, "--write-out": true, "-m": true, "--max-time": true,
	"--connect-timeout": true, "-x": true, "--proxy": true, "--post-data": true, "--post-file": true, "--header=": true,
	"-Method": true, "-Uri": true, "-Body": true, "-InFile": true, "-OutFile": true, "-Headers": true}

func firstNonFlagValue(args []string, withValue map[string]bool) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if withValue[a] {
				i++
			}
			continue
		}
		return a
	}
	return ""
}

func firstRemote(hosts []string) string {
	for _, h := range hosts {
		if h != "" && !localHost(h) {
			return h
		}
	}
	return ""
}

func localHost(h string) bool {
	h = strings.Trim(strings.ToLower(h), "[]")
	return h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasPrefix(h, "127.") || h == "::1" || h == "0.0.0.0"
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// install: package managers installing packages named on the command line.
// Installing what the project's own manifest lists (npm ci, pip install
// -r requirements.txt, uv sync, go mod download) is ordinary work.
func (c *classifier) install(tool string, args []string) {
	names := func(from []string) []string {
		var out []string
		for i := 0; i < len(from); i++ {
			a := from[i]
			if strings.HasPrefix(a, "-") {
				if installFlagWithValue[a] {
					i++
				}
				continue
			}
			if a == "." || strings.HasPrefix(a, "./") || strings.HasPrefix(a, "../") || strings.HasPrefix(a, "/") {
				continue // a local directory
			}
			out = append(out, packageName(a))
		}
		return out
	}
	sub := firstNonFlag(args)
	rest := afterFirst(args, sub)
	var pkgs []string
	switch tool {
	case "npm", "pnpm", "yarn", "bun", "cnpm":
		switch sub {
		case "install", "i", "add", "isntall", "in", "ins":
			pkgs = names(rest)
		case "exec", "x", "dlx":
			pkgs = runnerPackages(rest)
		}
		if tool == "pnpm" && sub == "dlx" || tool == "yarn" && sub == "dlx" {
			pkgs = runnerPackages(rest)
		}
	case "npx", "pnpx", "bunx", "uvx":
		pkgs = runnerPackages(args)
	case "pip", "pip3", "pipx", "easy_install":
		if sub == "install" || sub == "run" && tool == "pipx" {
			if !hasAny(rest, "-r", "--requirement", "-e", "--editable") || len(names(stripReq(rest))) > 0 {
				pkgs = names(stripReq(rest))
			}
		}
	case "uv":
		switch sub {
		case "add", "tool":
			pkgs = names(rest)
		case "pip":
			if firstNonFlag(rest) == "install" {
				pkgs = names(stripReq(afterFirst(rest, "install")))
			}
		}
	case "python", "python3":
		if m, ok := flagValue(args, "-m"); ok && (m == "pip" || m == "pip3") {
			r := afterFirst(args, m)
			if firstNonFlag(r) == "install" {
				pkgs = names(stripReq(afterFirst(r, "install")))
			}
		}
	case "poetry", "pdm", "hatch", "rye":
		if sub == "add" {
			pkgs = names(rest)
		}
	case "pipenv":
		if sub == "install" {
			pkgs = names(rest)
		}
	case "conda", "mamba", "micromamba":
		if sub == "install" {
			pkgs = names(rest)
		}
	case "cargo":
		if sub == "add" || sub == "install" {
			pkgs = names(rest)
		}
	case "go":
		if sub == "install" || sub == "get" {
			for _, n := range names(rest) {
				if strings.Contains(strings.SplitN(n, "/", 2)[0], ".") {
					pkgs = append(pkgs, n) // a remote module path
				}
			}
		}
		if sub == "run" {
			for _, n := range names(rest) {
				if strings.Contains(n, "@") || strings.Contains(strings.SplitN(n, "/", 2)[0], ".") && !strings.HasSuffix(n, ".go") {
					pkgs = append(pkgs, n)
					break
				}
			}
		}
	case "gem":
		if sub == "install" {
			pkgs = names(rest)
		}
	case "bundle", "bundler":
		if sub == "add" {
			pkgs = names(rest)
		}
	case "composer":
		switch sub {
		case "require":
			pkgs = names(rest)
		case "global":
			pkgs = names(afterFirst(rest, "require"))
		}
	case "dotnet":
		if sub == "add" && firstNonFlag(rest) == "package" {
			pkgs = names(afterFirst(rest, "package"))
		}
		if sub == "tool" && firstNonFlag(rest) == "install" {
			pkgs = names(afterFirst(rest, "install"))
		}
	case "nuget", "deno":
		if sub == "install" || sub == "add" {
			pkgs = names(rest)
		}
	case "pacman", "yay", "paru":
		for _, a := range args {
			if strings.HasPrefix(a, "-S") && !strings.ContainsAny(a[2:], "sicgl") {
				pkgs = names(args)
				break
			}
		}
	case "nix-env":
		if hasAny(args, "-i", "-iA", "--install") {
			pkgs = names(args)
		}
	case "apt", "apt-get", "yum", "dnf", "apk", "brew", "port", "zypper", "snap", "choco", "winget", "scoop", "flatpak":
		switch sub {
		case "install", "add", "reinstall":
			pkgs = names(rest)
		}
	case "install-module", "install-package", "install-script":
		pkgs = names(args)
	}
	if len(pkgs) > 0 {
		sort.Strings(pkgs)
		c.add(Finding{CatInstall, strings.Join(pkgs, " "), tool + " installs " + strings.Join(pkgs, ", ")})
	}
}

var installFlagWithValue = map[string]bool{"-r": true, "--requirement": true, "-c": true, "--constraint": true, "-i": true,
	"--index-url": true, "--extra-index-url": true, "--registry": true, "-t": true, "--target": true, "--prefix": true,
	"--root": true, "-p": true, "--python": true, "--version": true, "-v": true, "--source": true}

// runnerPackages: npx PKG args — tools a project normally has installed
// are not new packages.
func runnerPackages(args []string) []string {
	p := firstNonFlag(args)
	if p == "" {
		return nil
	}
	if runnerAllowed[packageName(p)] {
		return nil
	}
	return []string{packageName(p)}
}

var runnerAllowed = map[string]bool{"tsc": true, "typescript": true, "eslint": true, "prettier": true, "vitest": true, "jest": true,
	"mocha": true, "playwright": true, "biome": true, "@biomejs/biome": true, "ts-node": true, "tsx": true, "ruff": true,
	"pytest": true, "mypy": true, "black": true}

func stripReq(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-r" || args[i] == "--requirement" || args[i] == "-e" || args[i] == "--editable" || args[i] == "-c" {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// packageName drops a version or extras: lodash@4, requests==2.0,
// pkg[extra]>=1, github.com/x/y@v1.2.
func packageName(s string) string {
	s = strings.Trim(s, `"'`)
	if strings.Contains(s, "://") {
		return s // a URL or VCS spec: authorized only as written
	}
	if strings.HasPrefix(s, "@") { // scoped npm package: @scope/name@1
		if i := strings.Index(s[1:], "@"); i >= 0 {
			return s[:i+1]
		}
		return s
	}
	for _, sep := range []string{"==", ">=", "<=", "~=", "!=", ">", "<", "[", "@", ":"} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	return s
}

func firstNonFlag(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

func afterFirst(args []string, w string) []string {
	for i, a := range args {
		if a == w {
			return args[i+1:]
		}
	}
	return nil
}

func hasAny(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

func gitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if globalFlagsWithValue["git"][a] {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}

// credentialsCommand: commands that print the environment (API keys and
// tokens live there).
func (c *classifier) credentialsCommand(tool string, args []string) {
	switch tool {
	case "env", "printenv":
		if firstNonFlag(args) == "" || tool == "printenv" {
			c.add(Finding{CatCredentials, "environment", tool + " prints environment variables"})
		}
	case "set", "export", "declare", "typeset":
		if len(args) == 0 || args[0] == "-p" || args[0] == "-x" && len(args) == 1 {
			c.add(Finding{CatCredentials, "environment", tool + " prints environment variables"})
		}
	case "get-childitem", "gci", "dir", "ls":
		for _, a := range args {
			if strings.EqualFold(strings.TrimSuffix(a, "\\"), "env:") {
				c.add(Finding{CatCredentials, "environment", tool + " lists environment variables"})
			}
		}
	}
}

// gitExecKeys are git settings that make git run a program.
var gitExecKeys = regexp.MustCompile(`(?i)^(core\.(hookspath|fsmonitor|sshcommand|editor|pager|askpass|gitproxy)|diff\.external|diff\..*\.(textconv|command)|filter\..*|merge\..*\.driver|alias\..*|credential\..*|include\.path|includeif\..*|protocol\..*\.allow|uploadpack\.packobjectshook|sequence\.editor|gpg\.(.*\.)?program|url\..*\.insteadof|http\.proxy|remote\..*\.(url|uploadpack|receivepack))$`)

func (c *classifier) execConfig(tool string, args []string) {
	switch tool {
	case "git":
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-c" || args[i] == "--config-env" {
				key, _, _ := strings.Cut(args[i+1], "=")
				if gitExecKeys.MatchString(key) {
					c.add(Finding{CatExecConfig, key, "git runs what " + key + " names"})
				}
			}
		}
		if gitSubcommand(args) == "config" {
			cfg := afterFirst(args, "config")
			read := hasAny(cfg, "--get", "--get-all", "--get-regexp", "-l", "--list", "--show-origin", "--show-scope", "get", "list")
			global := hasAny(cfg, "--global", "--system", "--file", "-f")
			var key string
			for _, a := range cfg {
				if !strings.HasPrefix(a, "-") && a != "set" {
					key = a
					break
				}
			}
			nonFlags := 0
			for _, a := range cfg {
				if !strings.HasPrefix(a, "-") {
					nonFlags++
				}
			}
			if !read && (nonFlags >= 2 || hasAny(cfg, "--unset", "--unset-all", "--add", "--replace-all", "--remove-section", "--rename-section", "-e", "--edit")) &&
				(global || gitExecKeys.MatchString(key)) {
				c.add(Finding{CatExecConfig, key, "git configuration that runs programs or applies beyond this project"})
			}
		}
		if gitSubcommand(args) == "update-index" && hasAny(args, "--chmod=+x") {
			c.add(Finding{CatExecConfig, "update-index", "marks a file executable in git"})
		}
	case "npm", "pnpm", "yarn", "pip", "pip3":
		if firstNonFlag(args) == "config" {
			if sub := firstNonFlag(afterFirst(args, "config")); sub == "set" || sub == "edit" || sub == "delete" {
				c.add(Finding{CatExecConfig, tool + " config", tool + "'s configuration (registry, scripts shell)"})
			}
		}
	case "crontab":
		if !hasAny(args, "-l") {
			c.add(Finding{CatExecConfig, "crontab", "schedules commands"})
		}
	case "systemctl":
		switch firstNonFlag(args) {
		case "enable", "start", "restart", "link", "edit", "daemon-reload", "set-environment":
			c.add(Finding{CatExecConfig, "systemctl", "starts or installs services"})
		}
	case "launchctl":
		switch firstNonFlag(args) {
		case "load", "bootstrap", "submit", "enable", "kickstart":
			c.add(Finding{CatExecConfig, "launchctl", "starts or installs services"})
		}
	case "schtasks", "register-scheduledtask":
		c.add(Finding{CatExecConfig, tool, "schedules commands"})
	}
}

// ---- shell lexing

type word struct {
	text     string
	redirect byte // '>' written, '<' read, 0 an argument
}

var assignRE = regexp.MustCompile(`(?:^|[\s;&|(])([A-Za-z_][A-Za-z0-9_]*)=('([^']*)'|"([^"]*)"|[^\s;&|)]*)`)

// assignments collects NAME=value assignments in a command line, so that
// "f=.env; cat $f" is read as "cat .env".
func assignments(line string) map[string]string {
	vars := map[string]string{}
	for _, m := range assignRE.FindAllStringSubmatch(line, -1) {
		v := m[2]
		switch {
		case strings.HasPrefix(v, "'"):
			v = m[3]
		case strings.HasPrefix(v, `"`):
			v = m[4]
		}
		vars[m[1]] = v
	}
	return vars
}

var varRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func expandVars(s string, vars map[string]string, home string) string {
	if strings.HasPrefix(s, "~/") && home != "" {
		s = home + s[1:]
	}
	return varRef.ReplaceAllStringFunc(s, func(m string) string {
		sub := varRef.FindStringSubmatch(m)
		name := sub[1] + sub[2]
		if name == "HOME" && home != "" {
			return home
		}
		if v, ok := vars[name]; ok {
			return v
		}
		return m
	})
}

// lexShell splits a command line into simple commands of words: quotes
// removed and joined ("c""at" is cat), escapes resolved, redirections
// marked. The bodies of $(…), `…`, <(…) and >(…) are returned apart, to be
// classified as command lines of their own.
func lexShell(line string) (cmds [][]word, nested []string) {
	var cur []word
	var b strings.Builder
	inWord := false
	var redir byte
	flush := func() {
		if inWord {
			cur = append(cur, word{text: b.String(), redirect: redir})
			redir = 0
		}
		b.Reset()
		inWord = false
	}
	endCmd := func() {
		flush()
		if len(cur) > 0 {
			cmds = append(cmds, cur)
		}
		cur = nil
	}
	rs := []rune(line)
	// balanced reads a $( or <( body up to its closing parenthesis.
	balanced := func(i int) (string, int) {
		depth := 1
		start := i
		for ; i < len(rs); i++ {
			switch rs[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return string(rs[start:i]), i
				}
			case '\'':
				for i++; i < len(rs) && rs[i] != '\''; i++ {
				}
			}
		}
		return string(rs[start:]), len(rs)
	}
	for i := 0; i < len(rs); i++ {
		ch := rs[i]
		switch {
		case ch == '\\' && i+1 < len(rs):
			if rs[i+1] == '\n' {
				i++
				continue
			}
			b.WriteRune(rs[i+1])
			inWord = true
			i++
		case ch == '\'':
			inWord = true
			for i++; i < len(rs) && rs[i] != '\''; i++ {
				b.WriteRune(rs[i])
			}
		case ch == '"':
			inWord = true
			for i++; i < len(rs) && rs[i] != '"'; i++ {
				switch {
				case rs[i] == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]):
					i++
					b.WriteRune(rs[i])
				case rs[i] == '$' && i+1 < len(rs) && rs[i+1] == '(':
					body, end := balanced(i + 2)
					nested = append(nested, body)
					b.WriteString("$(…)")
					i = end
				case rs[i] == '`':
					j := i + 1
					for j < len(rs) && rs[j] != '`' {
						j++
					}
					nested = append(nested, string(rs[i+1:min(j, len(rs))]))
					i = j
				default:
					b.WriteRune(rs[i])
				}
			}
		case ch == '$' && i+1 < len(rs) && rs[i+1] == '(':
			body, end := balanced(i + 2)
			nested = append(nested, body)
			b.WriteString("$(…)")
			inWord = true
			i = end
		case ch == '`':
			j := i + 1
			for j < len(rs) && rs[j] != '`' {
				j++
			}
			nested = append(nested, string(rs[i+1:min(j, len(rs))]))
			inWord = true
			b.WriteString("$(…)")
			i = j
		case (ch == '<' || ch == '>') && i+1 < len(rs) && rs[i+1] == '(':
			body, end := balanced(i + 2)
			nested = append(nested, body)
			flush()
			i = end
		case ch == '<' || ch == '>':
			// A file descriptor number before it belongs to the operator.
			if inWord && (isDigits(b.String()) || b.String() == "&") {
				b.Reset()
				inWord = false
			} else {
				flush()
			}
			op := ch
			for i+1 < len(rs) && strings.ContainsRune("<>|&", rs[i+1]) {
				i++
				if rs[i] == '&' && op == '>' && i+1 < len(rs) && (rs[i+1] >= '0' && rs[i+1] <= '9' || rs[i+1] == '-') {
					// >&2: a descriptor, not a file.
					for i+1 < len(rs) && (rs[i+1] >= '0' && rs[i+1] <= '9' || rs[i+1] == '-') {
						i++
					}
					op = 0
					break
				}
			}
			redir = byte(op)
		case ch == ';' || ch == '|' || ch == '&' || ch == '\n' || ch == '(' || ch == ')' || ch == '{' && !inWord || ch == '}' && !inWord:
			endCmd()
		case ch == ' ' || ch == '\t' || ch == '\r':
			flush()
		default:
			b.WriteRune(ch)
			inWord = true
		}
	}
	endCmd()
	return cmds, nested
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
