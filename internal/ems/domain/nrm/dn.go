package nrm

import (
	"sort"
	"strings"
)

// DN is a Distinguished Name encoded as "k=v,k=v,...".
type DN string

type RDN struct {
	Key   string
	Value string
}

func (d DN) String() string { return string(d) }

func Build(parts ...RDN) DN {
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(p.Key)
		b.WriteByte('=')
		b.WriteString(p.Value)
	}
	return DN(b.String())
}

func Append(base DN, parts ...RDN) DN {
	if base == "" {
		return Build(parts...)
	}
	if len(parts) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(string(base))
	for _, p := range parts {
		b.WriteByte(',')
		b.WriteString(p.Key)
		b.WriteByte('=')
		b.WriteString(p.Value)
	}
	return DN(b.String())
}

func KeyValues(dn DN) map[string]string {
	out := map[string]string{}
	if dn == "" {
		return out
	}
	for _, seg := range strings.Split(string(dn), ",") {
		k, v, ok := strings.Cut(seg, "=")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}

func Canonicalize(dn DN) DN {
	m := KeyValues(dn)
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]RDN, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, RDN{Key: k, Value: m[k]})
	}
	return Build(parts...)
}
