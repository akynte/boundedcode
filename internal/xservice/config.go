package xservice

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// analyzeYAML finds environment variables supplied by deployment config:
// Kubernetes container `env:` lists, Helm values `env:` maps, docker-compose
// `environment:`, ConfigMap `data:` keys and kustomize configMapGenerator
// literals. Helm templates (which are not valid YAML) are skipped.
func analyzeYAML(repo, root string, relFiles []string) ([]Endpoint, []Diagnostic) {
	var out []Endpoint
	var diags []Diagnostic
	for _, rel := range relFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || len(b) > maxSpecBytes {
			continue
		}
		if bytes.Contains(b, []byte("{{")) {
			continue // Helm/Go template, not YAML
		}
		large := len(b) > 1<<20 // only API specifications are this large
		dec := yaml.NewDecoder(bytes.NewReader(b))
		for {
			var doc yaml.Node
			if err := dec.Decode(&doc); err != nil {
				if err.Error() != "EOF" {
					diags = append(diags, Diagnostic{File: rel, Message: "yaml: " + err.Error()})
				}
				break
			}
			if _, ok := isOpenAPI(&doc); ok {
				e, d := openAPIOperations(repo, rel, &doc)
				out, diags = append(out, e...), append(diags, d...)
				continue
			}
			if large {
				break
			}
			if isLiquibase(&doc) {
				out = append(out, liquibaseYAML(repo, rel, &doc)...)
				continue
			}
			isConfigMap := kindOf(&doc) == "ConfigMap"
			walkYAML(&doc, func(key string, val *yaml.Node, keyNode *yaml.Node) {
				emit := func(name string, line int, detail string) {
					if validEnvName(name) {
						out = append(out, Endpoint{Kind: EnvProvide, Repo: repo, File: filepath.ToSlash(rel), Line: line, Env: name, Confidence: Exact, Detail: detail})
					}
				}
				switch key {
				case "env":
					switch val.Kind {
					case yaml.SequenceNode: // k8s: - name: X
						for _, item := range val.Content {
							if n := mapGet(item, "name"); n != nil {
								emit(n.Value, n.Line, "k8s env")
							}
						}
					case yaml.MappingNode: // helm values: env: {X: ...}
						for i := 0; i+1 < len(val.Content); i += 2 {
							emit(val.Content[i].Value, val.Content[i].Line, "values env")
						}
					}
				case "environment": // compose
					switch val.Kind {
					case yaml.SequenceNode:
						for _, item := range val.Content {
							name, _, _ := strings.Cut(item.Value, "=")
							emit(name, item.Line, "compose environment")
						}
					case yaml.MappingNode:
						for i := 0; i+1 < len(val.Content); i += 2 {
							emit(val.Content[i].Value, val.Content[i].Line, "compose environment")
						}
					}
				case "data":
					if isConfigMap && val.Kind == yaml.MappingNode {
						for i := 0; i+1 < len(val.Content); i += 2 {
							emit(val.Content[i].Value, val.Content[i].Line, "ConfigMap data")
						}
					}
				case "literals": // kustomize configMapGenerator
					if val.Kind == yaml.SequenceNode {
						for _, item := range val.Content {
							name, _, _ := strings.Cut(item.Value, "=")
							emit(name, item.Line, "kustomize literal")
						}
					}
				}
			}, nil)
		}
	}
	return out, diags
}

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validEnvName(s string) bool { return envNameRE.MatchString(s) && len(s) <= 128 }

func kindOf(doc *yaml.Node) string {
	n := doc
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	if k := mapGet(n, "kind"); k != nil {
		return k.Value
	}
	return ""
}

func mapGet(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func walkYAML(n *yaml.Node, fn func(key string, val, keyNode *yaml.Node), _ *yaml.Node) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			walkYAML(c, fn, nil)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			fn(n.Content[i].Value, n.Content[i+1], n.Content[i])
			walkYAML(n.Content[i+1], fn, nil)
		}
	}
}

// analyzeDockerfile finds `ENV KEY=value` / `ENV KEY value`.
func analyzeDockerfile(repo, root string, relFiles []string) []Endpoint {
	var out []Endpoint
	for _, rel := range relFiles {
		f, err := os.Open(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		line := 0
		for sc.Scan() {
			line++
			l := strings.TrimSpace(sc.Text())
			if len(l) < 4 || !strings.EqualFold(l[:4], "ENV ") {
				continue
			}
			rest := strings.TrimSpace(l[4:])
			if !strings.Contains(strings.Fields(rest)[0], "=") { // ENV KEY value
				if f := strings.Fields(rest); validEnvName(f[0]) {
					out = append(out, Endpoint{Kind: EnvProvide, Repo: repo, File: filepath.ToSlash(rel), Line: line, Env: f[0], Confidence: Exact, Detail: "Dockerfile ENV"})
				}
				continue
			}
			for _, kv := range strings.Fields(rest) { // ENV A=1 B=2
				if k, _, ok := strings.Cut(kv, "="); ok && validEnvName(k) {
					out = append(out, Endpoint{Kind: EnvProvide, Repo: repo, File: filepath.ToSlash(rel), Line: line, Env: k, Confidence: Exact, Detail: "Dockerfile ENV"})
				}
			}
		}
		f.Close()
	}
	return out
}

// analyzeEnvTemplate reads committed env templates (.env.example etc.). Real
// .env files are never read: they may contain secrets.
func analyzeEnvTemplate(repo, root string, relFiles []string) []Endpoint {
	var out []Endpoint
	for _, rel := range relFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for i, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "export "))
			if l == "" || strings.HasPrefix(l, "#") {
				continue
			}
			if k, _, ok := strings.Cut(l, "="); ok && validEnvName(k) {
				out = append(out, Endpoint{Kind: EnvProvide, Repo: repo, File: filepath.ToSlash(rel), Line: i + 1, Env: k, Confidence: Exact, Detail: "env template"})
			}
		}
	}
	return out
}

// Terraform resources that declare topics/queues and the attribute naming them.
var tfTopicResources = map[string]string{
	"kafka_topic":              "name",
	"confluent_kafka_topic":    "topic_name",
	"aiven_kafka_topic":        "topic_name",
	"aws_msk_topic":            "name",
	"google_pubsub_topic":      "name",
	"aws_sns_topic":            "name",
	"aws_sqs_queue":            "name",
	"azurerm_servicebus_topic": "name",
	"redpanda_topic":           "name",
}

var tfResourceRE = regexp.MustCompile(`^\s*resource\s+"([A-Za-z0-9_]+)"\s+"([A-Za-z0-9_-]+)"\s*\{`)

// analyzeTerraform finds topic/queue resources with a literal name. It scans
// blocks by brace depth (strings and comments aware) rather than embedding
// an HCL parser, which would add an MPL-2.0 dependency.
func analyzeTerraform(repo, root string, relFiles []string) ([]Endpoint, []Diagnostic) {
	var out []Endpoint
	var diags []Diagnostic
	for _, rel := range relFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		lines := strings.Split(string(b), "\n")
		for i := 0; i < len(lines); i++ {
			m := tfResourceRE.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			attr, ok := tfTopicResources[m[1]]
			if !ok {
				continue
			}
			depth, start := 0, i
			found := false
			for j := i; j < len(lines); j++ {
				code := stripHCLComment(lines[j])
				depth += strings.Count(code, "{") - strings.Count(code, "}")
				if j > start && depth == 1 { // top-level attributes only
					if k, v, ok := strings.Cut(code, "="); ok && strings.TrimSpace(k) == attr {
						v = strings.TrimSpace(v)
						if uq, ok := hclString(v); ok {
							out = append(out, Endpoint{Kind: TopicProvision, Repo: repo, File: filepath.ToSlash(rel), Line: j + 1, Topic: uq,
								Symbol: m[1] + "." + m[2], Confidence: Exact, Detail: "terraform " + m[1]})
						} else {
							diags = append(diags, Diagnostic{File: rel, Line: j + 1, Message: m[1] + "." + m[2] + ": non-literal " + attr})
						}
						found = true
					}
				}
				if depth <= 0 && j > start {
					i = j
					break
				}
			}
			_ = found
		}
	}
	return out, diags
}

func stripHCLComment(l string) string {
	inStr := false
	for i := 0; i < len(l); i++ {
		switch {
		case l[i] == '"' && (i == 0 || l[i-1] != '\\'):
			inStr = !inStr
		case !inStr && (l[i] == '#' || (l[i] == '/' && i+1 < len(l) && l[i+1] == '/')):
			return l[:i]
		}
	}
	return l
}

func hclString(v string) (string, bool) {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' && !strings.Contains(v, "${") {
		return v[1 : len(v)-1], true
	}
	return "", false
}
