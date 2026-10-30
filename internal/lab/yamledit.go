package lab

import (
	"bytes"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/akynte/boundedcode/internal/config"
)

// Configuration files are edited as YAML trees, so that what the
// laboratory writes (an arm's configuration, a promoted change) changes the
// keys it sets and keeps everything else, comments included.

func parseYAML(b []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if len(bytes.TrimSpace(b)) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		return &doc, nil
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("not a YAML mapping")
	}
	return &doc, nil
}

func renderYAML(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), enc.Close()
}

// mapping returns the mapping under key in m, creating it.
func mapping(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			v := m.Content[i+1]
			if v.Kind != yaml.MappingNode {
				*v = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			return v
		}
	}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
	return v
}

// setKey sets key in m to value (encoded as YAML).
func setKey(m *yaml.Node, key string, value any) error {
	var v yaml.Node
	if err := v.Encode(value); err != nil {
		return err
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = &v
			return nil
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &v)
	return nil
}

// setPath sets a dotted key path ("direct.retrieval").
func setPath(doc *yaml.Node, path string, value any) error {
	parts := strings.Split(path, ".")
	m := doc.Content[0]
	for _, p := range parts[:len(parts)-1] {
		m = mapping(m, p)
	}
	return setKey(m, parts[len(parts)-1], value)
}

// settingsValues lists an ExperimentSettings' set fields as dotted paths
// and values, in the configuration's own key names.
func settingsValues(s config.ExperimentSettings) (map[string]any, error) {
	b, err := yaml.Marshal(s)
	if err != nil {
		return nil, err
	}
	var tree map[string]map[string]any
	if err := yaml.Unmarshal(b, &tree); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for section, kv := range tree {
		for k, v := range kv {
			out[section+"."+k] = v
		}
	}
	return out, nil
}

// applySettings sets s's fields in the configuration file text b.
func applySettings(b []byte, s config.ExperimentSettings) ([]byte, error) {
	doc, err := parseYAML(b)
	if err != nil {
		return nil, err
	}
	vals, err := settingsValues(s)
	if err != nil {
		return nil, err
	}
	for _, k := range sortedKeys(vals) {
		if err := setPath(doc, k, vals[k]); err != nil {
			return nil, err
		}
	}
	return renderYAML(doc)
}

// appendWorkload adds a workload override to the configuration text b.
func appendWorkload(b []byte, w config.WorkloadOverride) ([]byte, error) {
	doc, err := parseYAML(b)
	if err != nil {
		return nil, err
	}
	direct := mapping(doc.Content[0], "direct")
	var seq *yaml.Node
	for i := 0; i+1 < len(direct.Content); i += 2 {
		if direct.Content[i].Value == "workloads" && direct.Content[i+1].Kind == yaml.SequenceNode {
			seq = direct.Content[i+1]
		}
	}
	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		direct.Content = append(direct.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "workloads"}, seq)
	}
	var item yaml.Node
	if err := item.Encode(w); err != nil {
		return nil, err
	}
	seq.Content = append(seq.Content, &item)
	return renderYAML(doc)
}
