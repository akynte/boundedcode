package direct

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Skill is a folder with a SKILL.md: instructions the model loads on
// demand through the skill tool, or the user invokes as /name.
type Skill struct {
	Name, Description string
	Dir, File         string
	// UserOnly hides the skill from the model (disable-model-invocation).
	UserOnly bool
	// Body is the markdown after the frontmatter (loaded lazily).
	body string
}

// AgentDef is a custom sub-agent: a markdown file with frontmatter (name,
// description, tools) and a system prompt body.
type AgentDef struct {
	Name, Description string
	Tools             []string // "" = the default set for its kind
	ReadOnly          bool
	Prompt            string
	File              string
}

// Command is a prompt template invoked as /name args ($ARGUMENTS, $1…,
// and !`cmd` shell injection), the older form of a skill.
type Command struct {
	Name, Description, File string
}

// loadDefinitions discovers skills and agents in the configured dirs
// (project first, then user), without reading bodies.
func (r *Runner) loadDefinitions() {
	seen := map[string]bool{}
	for _, dir := range r.skillDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			file := filepath.Join(dir, e.Name(), "SKILL.md")
			fm, _, err := readFrontmatter(file)
			if err != nil {
				continue
			}
			name := firstNonEmpty(fm["name"], e.Name())
			if seen[name] {
				continue
			}
			seen[name] = true
			r.skills = append(r.skills, Skill{Name: name, Description: fm["description"], Dir: filepath.Dir(file), File: file,
				UserOnly: fm["disable-model-invocation"] == "true"})
		}
	}
	sort.Slice(r.skills, func(i, j int) bool { return r.skills[i].Name < r.skills[j].Name })
	seen = map[string]bool{}
	for _, dir := range r.agentDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			file := filepath.Join(dir, e.Name())
			fm, body, err := readFrontmatter(file)
			if err != nil {
				continue
			}
			name := firstNonEmpty(fm["name"], strings.TrimSuffix(e.Name(), ".md"))
			if seen[name] {
				continue
			}
			seen[name] = true
			def := AgentDef{Name: name, Description: fm["description"], Prompt: strings.TrimSpace(body), File: file}
			if t := fm["tools"]; t != "" {
				for _, x := range strings.Split(t, ",") {
					if x = strings.TrimSpace(x); x != "" {
						def.Tools = append(def.Tools, strings.ToLower(x))
					}
				}
			}
			def.ReadOnly = fm["read_only"] == "true" || fm["read-only"] == "true"
			r.agents = append(r.agents, def)
		}
	}
}

func (r *Runner) skillDirs() []string {
	if len(r.Opts.SkillDirs) > 0 {
		return r.Opts.SkillDirs
	}
	return []string{filepath.Join(r.Opts.Root, ".boundedcode", "skills"), filepath.Join(r.Opts.Root, ".agents", "skills"), filepath.Join(r.Opts.Root, ".claude", "skills")}
}

func (r *Runner) agentDirs() []string {
	if len(r.Opts.AgentDirs) > 0 {
		return r.Opts.AgentDirs
	}
	return []string{filepath.Join(r.Opts.Root, ".boundedcode", "agents"), filepath.Join(r.Opts.Root, ".claude", "agents")}
}

// Skills lists the discovered skills.
func (r *Runner) Skills() []Skill {
	r.loadOnce.Do(r.loadDefinitions)
	return r.skills
}

// Agents lists the custom sub-agent definitions.
func (r *Runner) Agents() []AgentDef {
	r.loadOnce.Do(r.loadDefinitions)
	return r.agents
}

// readFrontmatter parses "---\nkey: value\n---\nbody".
func readFrontmatter(file string) (map[string]string, string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, "", err
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	fm := map[string]string{}
	if !strings.HasPrefix(s, "---\n") {
		return fm, s, nil
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return fm, s, nil
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fm[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	body := rest[end+4:]
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	return fm, body, nil
}

func (s *Skill) load() (string, error) {
	if s.body != "" {
		return s.body, nil
	}
	_, body, err := readFrontmatter(s.File)
	if err != nil {
		return "", err
	}
	s.body = strings.ReplaceAll(strings.ReplaceAll(body, "${SKILL_DIR}", s.Dir), "${CLAUDE_SKILL_DIR}", s.Dir)
	return s.body, nil
}

func skillTool() Tool {
	return Tool{
		Name:        "skill",
		Description: "Load a skill's instructions by name (the available skills are listed in the system prompt). Use one when the task matches its description.",
		Schema:      schema(map[string]any{"name": prop("string", "Skill name"), "args": prop("string", "Optional arguments the skill asks for")}, "name"),
		Summary:     func(a map[string]any) string { return "skill " + str(a, "name") },
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			name := strings.TrimSpace(str(a, "name"))
			for i := range r.skills {
				s := &r.skills[i]
				if s.Name == name && !s.UserOnly {
					body, err := s.load()
					if err != nil {
						return "", err
					}
					return fmt.Sprintf("<skill name=%q dir=%q>\n%s\n</skill>%s", s.Name, s.Dir, substituteArgs(body, str(a, "args")), argsNote(body, str(a, "args"))), nil
				}
				if s.Name == name {
					return "", fmt.Errorf("skill %q is for the user to invoke (/%s), not the model", name, name)
				}
			}
			// A command template (.boundedcode/commands/NAME.md) works the
			// same way for the model as /NAME does for the user.
			prompt, ok, err := r.ExpandCommand(ctx, r.Opts.UserDir, name, str(a, "args"))
			if err != nil {
				return "", err
			}
			if ok {
				return fmt.Sprintf("<command name=%q>\n%s\n</command>", name, prompt), nil
			}
			return "", fmt.Errorf("no skill or command %q", name)
		},
	}
}

func argsNote(body, args string) string {
	if args != "" && !strings.Contains(body, "$ARGUMENTS") && !strings.Contains(body, "$1") {
		return "\n\nARGUMENTS: " + args
	}
	return ""
}

// skillsPrompt lists skills for the system prompt.
func (r *Runner) skillsPrompt() string {
	r.loadOnce.Do(r.loadDefinitions)
	var ls []string
	isSkill := map[string]bool{}
	for _, s := range r.skills {
		isSkill[s.Name] = true
		if s.UserOnly {
			continue
		}
		ls = append(ls, fmt.Sprintf("- %s: %s", s.Name, oneLine(s.Description, 200)))
	}
	for _, c := range r.Commands(r.Opts.UserDir) {
		if !isSkill[c.Name] {
			ls = append(ls, fmt.Sprintf("- %s (command template): %s", c.Name, oneLine(c.Description, 200)))
		}
	}
	if len(ls) == 0 {
		return ""
	}
	return "Skills (load one with the skill tool when a task matches):\n" + strings.Join(ls, "\n")
}

// ---- commands

// commandDirs are where /name templates live (project, then user).
func (r *Runner) commandDirs(userDir string) []string {
	dirs := []string{filepath.Join(r.Opts.Root, ".boundedcode", "commands"), filepath.Join(r.Opts.Root, ".claude", "commands")}
	if userDir != "" {
		dirs = append(dirs, filepath.Join(userDir, "commands"), filepath.Join(userDir, "skills"))
	}
	return dirs
}

// Commands lists the user-invocable commands: command files and skills.
func (r *Runner) Commands(userDir string) []Command {
	var out []Command
	seen := map[string]bool{}
	for _, dir := range r.commandDirs(userDir) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			var file, name string
			switch {
			case !e.IsDir() && strings.HasSuffix(e.Name(), ".md"):
				file, name = filepath.Join(dir, e.Name()), strings.TrimSuffix(e.Name(), ".md")
			case e.IsDir():
				file, name = filepath.Join(dir, e.Name(), "SKILL.md"), e.Name()
				if _, err := os.Stat(file); err != nil {
					continue
				}
			default:
				continue
			}
			fm, _, err := readFrontmatter(file)
			if err != nil {
				continue
			}
			name = firstNonEmpty(fm["name"], name)
			if seen[name] || fm["user-invocable"] == "false" {
				continue
			}
			seen[name] = true
			out = append(out, Command{Name: name, Description: fm["description"], File: file})
		}
	}
	for _, s := range r.Skills() {
		if !seen[s.Name] {
			seen[s.Name] = true
			out = append(out, Command{Name: s.Name, Description: s.Description, File: s.File})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ExpandCommand turns "/name args" into the prompt to send: the template
// with $ARGUMENTS, $1…$9 substituted and !`cmd` shell injections run in
// the project (their output replaces them). ok is false when no command
// has that name.
func (r *Runner) ExpandCommand(ctx context.Context, userDir, name, args string) (prompt string, ok bool, err error) {
	for _, c := range r.Commands(userDir) {
		if c.Name != name {
			continue
		}
		_, body, err := readFrontmatter(c.File)
		if err != nil {
			return "", true, err
		}
		body = strings.ReplaceAll(body, "${SKILL_DIR}", filepath.Dir(c.File))
		body = strings.ReplaceAll(body, "${PROJECT_DIR}", r.Opts.Root)
		template := body
		body = substituteArgs(body, args)
		body, err = r.injectShell(ctx, body)
		if err != nil {
			return "", true, err
		}
		// A template that does not mention its arguments still gets them.
		return strings.TrimSpace(body) + argsNote(template, args), true, nil
	}
	return "", false, nil
}

var (
	posArg      = regexp.MustCompile(`\$(ARGUMENTS\[(\d+)\]|(\d))`)
	shellInject = regexp.MustCompile("(^|\\s)!`([^`]+)`")
)

func substituteArgs(body, args string) string {
	fields := splitArgs(args)
	body = posArg.ReplaceAllStringFunc(body, func(m string) string {
		sub := posArg.FindStringSubmatch(m)
		n, _ := strconv.Atoi(firstNonEmpty(sub[2], sub[3]))
		if sub[2] == "" { // $1 is 1-based, $ARGUMENTS[0] 0-based
			n--
		}
		if n >= 0 && n < len(fields) {
			return fields[n]
		}
		return ""
	})
	return strings.ReplaceAll(body, "$ARGUMENTS", args)
}

// splitArgs splits on spaces, honouring double quotes.
func splitArgs(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Split(bufio.ScanRunes)
	var cur strings.Builder
	quoted, has := false, false
	for sc.Scan() {
		c := sc.Text()
		switch {
		case c == `"`:
			quoted, has = !quoted, true
		case c == " " && !quoted:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteString(c)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

func (r *Runner) injectShell(ctx context.Context, body string) (string, error) {
	var firstErr error
	out := shellInject.ReplaceAllStringFunc(body, func(m string) string {
		sub := shellInject.FindStringSubmatch(m)
		cmd := sub[2]
		argv := shellArgv(cmd)
		if err := policyCheck(argv); err != nil {
			firstErr = err
			return m
		}
		c := exec.CommandContext(ctx, argv[0], argv[1:]...)
		c.Dir = r.cwd()
		c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "NO_COLOR=1")
		b, err := c.CombinedOutput()
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("command %q in the template failed: %w", cmd, err)
		}
		return sub[1] + strings.TrimRight(string(b), "\n")
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}
