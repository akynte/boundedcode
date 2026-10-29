package model

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// GGUFMeta is what BoundedCode reads from a GGUF file's header: enough to
// check a profile's declared capabilities against the weights.
type GGUFMeta struct {
	Architecture string `json:"architecture"`
	Name         string `json:"name,omitempty"`
	// Tokenizer is tokenizer.ggml.pre, the tokenizer family a draft model
	// must share.
	Tokenizer string `json:"tokenizer"`
	// ContextLength is <arch>.context_length, the trained context.
	ContextLength int `json:"context_length,omitempty"`
	Blocks        int `json:"blocks,omitempty"`
	Experts       int `json:"experts,omitempty"`
	// MTPLayers is <arch>.nextn_predict_layers: the multi-token prediction
	// layers the file carries (0: none, so draft-mtp cannot run).
	MTPLayers int `json:"mtp_layers"`
	Tensors   int `json:"tensors"`
}

// ReadGGUFMeta reads the header of a GGUF file (versions 2 and 3). Tensor
// data is not read.
func ReadGGUFMeta(path string) (GGUFMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return GGUFMeta{}, err
	}
	defer f.Close()
	return readGGUFMeta(bufio.NewReaderSize(f, 1<<20))
}

// ggufMaxString bounds a header string; tokenizer vocabularies are arrays
// of short strings, never one long one.
const ggufMaxString = 16 << 20

func readGGUFMeta(r io.Reader) (GGUFMeta, error) {
	var m GGUFMeta
	var head struct {
		Magic   [4]byte
		Version uint32
		Tensors uint64
		KVs     uint64
	}
	if err := binary.Read(r, binary.LittleEndian, &head); err != nil {
		return m, err
	}
	if string(head.Magic[:]) != "GGUF" {
		return m, errors.New("not a GGUF file")
	}
	if head.Version < 2 || head.Version > 3 {
		return m, fmt.Errorf("GGUF version %d is not supported", head.Version)
	}
	m.Tensors = int(head.Tensors)
	ints := map[string]int{}
	for range head.KVs {
		key, err := ggufString(r)
		if err != nil {
			return m, err
		}
		var typ uint32
		if err := binary.Read(r, binary.LittleEndian, &typ); err != nil {
			return m, err
		}
		v, err := ggufValue(r, typ)
		if err != nil {
			return m, fmt.Errorf("%s: %w", key, err)
		}
		switch x := v.(type) {
		case string:
			switch key {
			case "general.architecture":
				m.Architecture = x
			case "general.name":
				m.Name = x
			case "tokenizer.ggml.pre":
				m.Tokenizer = x
			}
		case int64:
			ints[key] = int(x)
		}
	}
	a := m.Architecture + "."
	m.ContextLength, m.Blocks, m.Experts, m.MTPLayers = ints[a+"context_length"], ints[a+"block_count"], ints[a+"expert_count"], ints[a+"nextn_predict_layers"]
	return m, nil
}

func ggufString(r io.Reader) (string, error) {
	var n uint64
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	if n > ggufMaxString {
		return "", fmt.Errorf("string of %d bytes", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

// ggufValue reads one value: integers come back as int64, strings as
// string; arrays and floats are read and dropped (nil).
func ggufValue(r io.Reader, typ uint32) (any, error) {
	size := map[uint32]int{0: 1, 1: 1, 2: 2, 3: 2, 4: 4, 5: 4, 6: 4, 7: 1, 10: 8, 11: 8, 12: 8}
	switch typ {
	case 8:
		return ggufString(r)
	case 9:
		var at uint32
		var n uint64
		if err := binary.Read(r, binary.LittleEndian, &at); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return nil, err
		}
		if sz, ok := size[at]; ok {
			_, err := io.CopyN(io.Discard, r, int64(n)*int64(sz))
			return nil, err
		}
		for range n {
			if _, err := ggufValue(r, at); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	sz, ok := size[typ]
	if !ok {
		return nil, fmt.Errorf("unknown GGUF value type %d", typ)
	}
	b := make([]byte, 8)
	if _, err := io.ReadFull(r, b[:sz]); err != nil {
		return nil, err
	}
	switch typ {
	case 0, 7:
		return int64(b[0]), nil
	case 1:
		return int64(int8(b[0])), nil
	case 2:
		return int64(binary.LittleEndian.Uint16(b)), nil
	case 3:
		return int64(int16(binary.LittleEndian.Uint16(b))), nil
	case 4:
		return int64(binary.LittleEndian.Uint32(b)), nil
	case 5:
		return int64(int32(binary.LittleEndian.Uint32(b))), nil
	case 10, 11:
		return int64(binary.LittleEndian.Uint64(b)), nil
	}
	return nil, nil // floats
}

// CheckDeclared compares a profile's declared capabilities with its
// weights' metadata and returns the disagreements, one sentence each.
func CheckDeclared(c Capabilities, m GGUFMeta) []string {
	var out []string
	if c.Tokenizer != "" && m.Tokenizer != "" && c.Tokenizer != m.Tokenizer {
		out = append(out, fmt.Sprintf("declares tokenizer %s; the weights use %s", c.Tokenizer, m.Tokenizer))
	}
	if c.ContextMax > 0 && m.ContextLength > 0 && c.ContextMax != m.ContextLength {
		out = append(out, fmt.Sprintf("declares a %d-token context; the weights say %d", c.ContextMax, m.ContextLength))
	}
	hasMTP := strings.Contains(strings.Join(c.Speculative, " "), "mtp")
	switch {
	case hasMTP && m.MTPLayers == 0:
		out = append(out, "declares multi-token prediction, but the weights have no MTP layers")
	case !hasMTP && m.MTPLayers > 0:
		out = append(out, fmt.Sprintf("the weights carry %d MTP layer(s) the profile does not declare", m.MTPLayers))
	}
	return out
}
