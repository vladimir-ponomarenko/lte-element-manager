package netconf

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"

	"lte-element-manager/internal/ems/fcaps/metrics"
)

const (
	moduleNamespace = "urn:ems:enb:metrics"
)

func buildMetricsReply(store *metrics.Store) (string, error) {
	latest := store.Latest()
	if latest.RawJSON == "" {
		return "<data/>", nil
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(latest.RawJSON), &obj); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")

	start := xml.StartElement{
		Name: xml.Name{Local: "data"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: "urn:ietf:params:xml:ns:netconf:base:1.0"}},
	}
	if err := enc.EncodeToken(start); err != nil {
		return "", err
	}

	root := xml.StartElement{
		Name: xml.Name{Local: "enb_metrics"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: moduleNamespace}},
	}
	if err := enc.EncodeToken(root); err != nil {
		return "", err
	}
	if err := encodeMap(enc, obj); err != nil {
		return "", err
	}
	if err := enc.EncodeToken(root.End()); err != nil {
		return "", err
	}
	if err := enc.EncodeToken(start.End()); err != nil {
		return "", err
	}
	if err := enc.Flush(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func encodeMap(enc *xml.Encoder, m map[string]any) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m[k]
		switch val := v.(type) {
		case map[string]any:
			if err := encodeElement(enc, k, val); err != nil {
				return err
			}
		case []any:
			for _, item := range val {
				if err := encodeElement(enc, k, item); err != nil {
					return err
				}
			}
		default:
			if err := encodeElement(enc, k, val); err != nil {
				return err
			}
		}
	}
	return nil
}

func encodeElement(enc *xml.Encoder, name string, value any) error {
	start := xml.StartElement{Name: xml.Name{Local: name}}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}

	switch v := value.(type) {
	case map[string]any:
		if err := encodeMap(enc, v); err != nil {
			return err
		}
	case []any:
		for _, item := range v {
			if err := encodeElement(enc, "item", item); err != nil {
				return err
			}
		}
	default:
		if err := enc.EncodeToken(xml.CharData([]byte(toString(v)))); err != nil {
			return err
		}
	}

	return enc.EncodeToken(start.End())
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return formatFloat(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", t)
	}
}

func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%f", v)
}
