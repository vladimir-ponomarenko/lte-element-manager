package srsran

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizeForNetconf prepares srsRAN raw JSON for libyang strict parsing.
func NormalizeForNetconf(raw string) (string, error) {
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return "", err
	}

	normalizeStructure(root)
	normalizeTypes(root)

	out, err := json.Marshal(root)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func asSlice(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func normalizeStructure(root map[string]any) {
	cellList, ok := asSlice(root["cell_list"])
	if !ok {
		return
	}
	for _, cellEntry := range cellList {
		cellEntryObj, ok := asMap(cellEntry)
		if !ok {
			continue
		}
		cellObj := cellEntryObj
		if inner, ok := asMap(cellEntryObj["cell_container"]); ok {
			cellObj = inner
		}
		if _, hasKey := cellEntryObj["carrier_id"]; !hasKey {
			if cid, ok := cellObj["carrier_id"]; ok {
				cellEntryObj["carrier_id"] = cid
			}
		}
		if _, hasKey := cellEntryObj["pci"]; !hasKey {
			if pci, ok := cellObj["pci"]; ok {
				cellEntryObj["pci"] = pci
			}
		}

		ueList, ok := asSlice(cellObj["ue_list"])
		if !ok {
			continue
		}
		for _, ueEntry := range ueList {
			ueEntryObj, ok := asMap(ueEntry)
			if !ok {
				continue
			}
			ueObj := ueEntryObj
			if inner, ok := asMap(ueEntryObj["ue_container"]); ok {
				ueObj = inner
			}
			if _, hasKey := ueEntryObj["ue_rnti"]; !hasKey {
				if id, ok := ueObj["ue_rnti"]; ok {
					ueEntryObj["ue_rnti"] = id
				}
			}

			bearerList, ok := asSlice(ueObj["bearer_list"])
			if !ok {
				continue
			}
			for _, bearerEntry := range bearerList {
				bearerObj, ok := asMap(bearerEntry)
				if !ok {
					continue
				}
				if _, hasKey := bearerObj["bearer_id"]; hasKey {
					continue
				}
				if inner, ok := asMap(bearerObj["bearer_container"]); ok {
					if id, ok := inner["bearer_id"]; ok {
						bearerObj["bearer_id"] = id
					}
				}
			}
		}
	}
}

func normalizeTypes(root map[string]any) {
	walkAndConvert(root)
}

func walkAndConvert(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, vv := range t {
			if s, ok := convertValue(k, vv); ok {
				t[k] = s
				continue
			}
			walkAndConvert(vv)
		}
	case []any:
		for _, item := range t {
			walkAndConvert(item)
		}
	}
}

func convertValue(key string, v any) (string, bool) {
	if strings.HasPrefix(key, "nas_") {
		return toUintString(v)
	}
	switch key {
	case "timestamp",
		"dl_cqi",
		"dl_mcs",
		"dl_bitrate",
		"dl_bler",
		"ul_snr",
		"ul_mcs",
		"ul_bitrate",
		"ul_bler",
		"ul_phr",
		"ul_pusch_rssi",
		"ul_pucch_rssi",
		"ul_pucch_ni",
		"dl_cqi_offset",
		"ul_snr_offset",
		"dl_latency",
		"ul_latency":
		return toDecimalString(v)
	case "dl_total_bytes", "ul_total_bytes":
		return toUintString(v)
	case "ul_pusch_tpc", "ul_pucch_tpc":
		return toInt64String(v)
	default:
		return "", false
	}
}

func toUintString(v any) (string, bool) {
	switch t := v.(type) {
	case json.Number:
		return t.String(), true
	case float64:
		return fmt.Sprintf("%.0f", t), true
	case int64:
		return fmt.Sprintf("%d", t), true
	case uint64:
		return fmt.Sprintf("%d", t), true
	default:
		return "", false
	}
}

func toDecimalString(v any) (string, bool) {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("%.6f", f), true
	case float64:
		return fmt.Sprintf("%.6f", t), true
	case int64:
		return fmt.Sprintf("%d", t), true
	case uint64:
		return fmt.Sprintf("%d", t), true
	default:
		return "", false
	}
}

func toInt64String(v any) (string, bool) {
	switch t := v.(type) {
	case json.Number:
		return t.String(), true
	case float64:
		return fmt.Sprintf("%.0f", t), true
	case int64:
		return fmt.Sprintf("%d", t), true
	case uint64:
		return fmt.Sprintf("%d", t), true
	default:
		return "", false
	}
}
