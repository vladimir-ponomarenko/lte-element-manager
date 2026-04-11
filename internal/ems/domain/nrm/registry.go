package nrm

import (
	"fmt"
	"sort"
	"sync"

	"lte-element-manager/internal/ems/domain/canonical"
	emserrors "lte-element-manager/internal/errors"
)

type MOType string

const (
	MOTypeSubNetwork     MOType = "SubNetwork"
	MOTypeManagedElement        = "ManagedElement"
	MOTypeENBFunction           = "ENBFunction"
	MOTypeEUtranCell            = "EUtranCell"
)

type Object struct {
	Type MOType
	Name string
	DN   DN
}

type Registry struct {
	mu sync.RWMutex

	subNetwork string
	element    string
	enbFnID    string

	objectsByDN map[DN]Object
	cellsByKey  map[string]Object
}

type Config struct {
	SubNetwork     string
	ManagedElement string
	ENBFunctionID  string
}

func New(cfg Config) (*Registry, error) {
	if cfg.SubNetwork == "" || cfg.ManagedElement == "" || cfg.ENBFunctionID == "" {
		return nil, emserrors.New(emserrors.ErrCodeConfig, "nrm config is incomplete",
			emserrors.WithOp("nrm"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}

	r := &Registry{
		subNetwork:  cfg.SubNetwork,
		element:     cfg.ManagedElement,
		enbFnID:     cfg.ENBFunctionID,
		objectsByDN: make(map[DN]Object, 64),
		cellsByKey:  make(map[string]Object, 32),
	}
	r.initStatic()
	return r, nil
}

func (r *Registry) initStatic() {
	base := Build(
		RDN{Key: string(MOTypeSubNetwork), Value: r.subNetwork},
		RDN{Key: string(MOTypeManagedElement), Value: r.element},
		RDN{Key: string(MOTypeENBFunction), Value: r.enbFnID},
	)
	r.objectsByDN[base] = Object{Type: MOTypeENBFunction, Name: r.enbFnID, DN: base}
}

func (r *Registry) ENBDN() DN {
	return Build(
		RDN{Key: string(MOTypeSubNetwork), Value: r.subNetwork},
		RDN{Key: string(MOTypeManagedElement), Value: r.element},
		RDN{Key: string(MOTypeENBFunction), Value: r.enbFnID},
	)
}

func (r *Registry) Resolve(s canonical.Sample) (DN, error) {
	switch {
	case s.Scope == "node":
		return r.ENBDN(), nil
	case s.Scope == "":
		return "", emserrors.New(emserrors.ErrCodeDataCorrupt, "canonical sample scope is empty",
			emserrors.WithOp("nrm"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	}

	cellKey := cellKeyFromSample(s)
	if cellKey == "" {
		return r.ENBDN(), nil
	}
	return r.ensureCell(cellKey), nil
}

func cellKeyFromSample(s canonical.Sample) string {
	if s.Scope == "" {
		return ""
	}
	scope := s.Scope
	if i := index(scope, "/ue:"); i >= 0 {
		scope = scope[:i]
	}
	if len(scope) >= 5 && scope[:5] == "cell:" {
		return scope
	}
	return ""
}

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func (r *Registry) ensureCell(key string) DN {
	r.mu.Lock()
	defer r.mu.Unlock()

	if obj, ok := r.cellsByKey[key]; ok {
		return obj.DN
	}

	cellName := sanitizeCellID(key)
	cellDN := Append(r.ENBDN(), RDN{Key: string(MOTypeEUtranCell), Value: cellName})
	obj := Object{Type: MOTypeEUtranCell, Name: cellName, DN: cellDN}
	r.cellsByKey[key] = obj
	r.objectsByDN[cellDN] = obj
	return cellDN
}

func (r *Registry) Get(dn DN) (Object, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.objectsByDN[dn]
	return o, ok
}

func (r *Registry) EUtranCells() []Object {
	r.mu.RLock()
	out := make([]Object, 0, len(r.cellsByKey))
	for _, o := range r.cellsByKey {
		out = append(out, o)
	}
	r.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].DN < out[j].DN })
	return out
}

func sanitizeCellID(scopeKey string) string {
	// scopeKey examples:
	// - "cell:1"
	// - "cell:carrier=0,pci=1"
	// - "cell:carrier=0,pci=1/ue:rnti=70" (should already be trimmed)
	id := scopeKey
	if len(id) >= 5 && id[:5] == "cell:" {
		id = id[5:]
	}
	if id == "" {
		return "1"
	}

	// Replace characters not suitable for a YANG "id" leaf.
	out := make([]byte, 0, len(id))
	for i := 0; i < len(id); i++ {
		ch := id[i]
		switch {
		case ch >= 'a' && ch <= 'z':
			out = append(out, ch)
		case ch >= 'A' && ch <= 'Z':
			out = append(out, ch+('a'-'A'))
		case ch >= '0' && ch <= '9':
			out = append(out, ch)
		case ch == '-' || ch == '_' || ch == '.':
			out = append(out, ch)
		default:
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return fmt.Sprintf("%d", len(scopeKey))
	}
	return string(out)
}
