package spec

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse decodes, defaults and validates a manifest.
//
// It returns the manifest as the engine understands it — fully defaulted, so
// an author can see what the engine actually works from (FR-2) — together with
// every problem found, in a deterministic order (FR-3).
//
// A non-empty Errors does not mean the Environment is nil: a manifest that
// decoded but failed validation is still worth showing back.
//
// Parse is pure. It reads no files, so a manifest referring to a local file is
// not checked for that file's existence here (issue 04 adds that, alongside
// resolving relative paths against the manifest's directory).
func Parse(data []byte) (*Environment, Errors) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, Errors{{
			Rule: "manifest is not valid YAML: " + trimYAMLPrefix(err.Error()),
			Hint: "check the indentation and quoting around the line named above",
		}}
	}

	doc := documentRoot(&root)
	if doc == nil || doc.Kind == 0 {
		return nil, Errors{{
			Rule: "manifest is empty",
			Hint: "a manifest needs at least apiVersion, kind, name and one machine",
		}}
	}
	if doc.Kind != yaml.MappingNode {
		return nil, Errors{{
			Rule: "manifest must be a mapping of sections",
			Hint: "the top level holds apiVersion, kind, name, backend and the sections machines, tasks and checks",
		}}
	}

	var errs Errors
	lines := lineIndex{}
	walkSchema(doc, reflect.TypeOf(Environment{}), "", &errs, lines)

	var env Environment
	decodeErrs := decodeNode(doc, &env, lines)
	env.applyDefaults()

	// Validation is skipped when a value could not be read at all: rules
	// applied to fields the decoder could not populate produce errors that
	// point at the wrong thing. Unknown fields do not have that problem and are
	// reported alongside validation.
	if len(decodeErrs) > 0 {
		errs = append(errs, decodeErrs...)
		return &env, errs.Sorted()
	}

	errs = append(errs, env.validate()...)
	return &env, errs.Sorted()
}

// documentRoot unwraps the document node yaml.Unmarshal produces.
func documentRoot(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		return n.Content[0]
	}
	return n
}

// decodeNode decodes the manifest and turns any decoding failure into errors
// carrying a field path.
func decodeNode(doc *yaml.Node, env *Environment, lines lineIndex) Errors {
	err := doc.Decode(env)
	if err == nil {
		return nil
	}

	var out Errors
	var te *yaml.TypeError
	if asTypeError(err, &te) {
		for _, msg := range te.Errors {
			line, text := splitLine(msg)
			out.add(lines.pathFor(line), "value cannot be read: "+text,
				"give this field a value of the type the field expects")
		}
		return out
	}
	// A custom unmarshaler (Duration, Section) returned its own message.
	line, text := splitLine(err.Error())
	out.add(lines.pathFor(line), trimYAMLPrefix(text),
		"correct the value named above")
	return out
}

// asTypeError reports whether err is a *yaml.TypeError, storing it in dst.
func asTypeError(err error, dst **yaml.TypeError) bool {
	te, ok := err.(*yaml.TypeError)
	if ok {
		*dst = te
	}
	return ok
}

// lineIndex maps a source line to the field paths that start on it, so that a
// decoding failure reported by line can be reported by path instead.
type lineIndex map[int][]string

// pathFor returns the best path for a line: the only one on it, or the
// outermost when several fields share a line, or empty when none is known.
func (li lineIndex) pathFor(line int) string {
	paths := li[line]
	switch len(paths) {
	case 0:
		return ""
	case 1:
		return paths[0]
	}
	best := paths[0]
	for _, p := range paths[1:] {
		if len(p) < len(best) {
			best = p
		}
	}
	return best
}

// walkSchema walks the decoded YAML tree against the manifest types, reporting
// every key the types do not declare, with its full path.
//
// This exists instead of yaml's own KnownFields because KnownFields reports a
// bare field name and a line, and FR-3 requires a field path. Walking the tree
// against the types by reflection also means the check cannot drift from the
// types the decoder actually uses.
func walkSchema(n *yaml.Node, t reflect.Type, path string, errs *Errors, lines lineIndex) {
	if n == nil {
		return
	}
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return
		}
		n = n.Alias
	}
	if path != "" {
		lines[n.Line] = append(lines[n.Line], path)
	}
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return
	}

	// A Section holds author-chosen keys, so every key is allowed and each
	// value is an entry.
	if sec, ok := reflect.New(t).Elem().Interface().(elemTyper); ok {
		if n.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			walkSchema(n.Content[i+1], sec.elemType(), join(path, n.Content[i].Value), errs, lines)
		}
		return
	}

	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return
		}
		known := yamlFields(t)
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			ft, ok := known[key]
			if !ok {
				errs.add(join(path, key), "unknown field", unknownFieldHint(key, known))
				continue
			}
			walkSchema(n.Content[i+1], ft, join(path, key), errs, lines)
		}
	case reflect.Slice, reflect.Array:
		if n.Kind != yaml.SequenceNode {
			return
		}
		for i, item := range n.Content {
			walkSchema(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i), errs, lines)
		}
	}
}

// yamlFields returns the manifest field names a struct declares, mapped to
// their types.
func yamlFields(t reflect.Type) map[string]reflect.Type {
	out := make(map[string]reflect.Type, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		name := strings.Split(f.Tag.Get("yaml"), ",")[0]
		switch name {
		case "-":
			continue
		case "":
			name = strings.ToLower(f.Name)
		}
		out[name] = f.Type
	}
	return out
}

// unknownFieldHint suggests the field the author probably meant.
func unknownFieldHint(key string, known map[string]reflect.Type) string {
	names := make([]string, 0, len(known))
	for n := range known {
		names = append(names, n)
	}
	sort.Strings(names)

	best, bestDist := "", 3 // only suggest a near miss
	for _, n := range names {
		if d := editDistance(key, n); d < bestDist {
			best, bestDist = n, d
		}
	}
	if best != "" {
		return fmt.Sprintf("did you mean %q? known fields here: %s", best, strings.Join(names, ", "))
	}
	return "known fields here: " + strings.Join(names, ", ")
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// join builds a field path.
func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// splitLine pulls a leading "line N: " off a yaml message.
func splitLine(msg string) (int, string) {
	msg = trimYAMLPrefix(msg)
	rest, ok := strings.CutPrefix(msg, "line ")
	if !ok {
		return 0, msg
	}
	num, text, ok := strings.Cut(rest, ":")
	if !ok {
		return 0, msg
	}
	n, err := strconv.Atoi(strings.TrimSpace(num))
	if err != nil {
		return 0, msg
	}
	return n, strings.TrimSpace(text)
}

// trimYAMLPrefix removes the "yaml: " the library prefixes its messages with.
func trimYAMLPrefix(msg string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(msg), "yaml:"))
}
