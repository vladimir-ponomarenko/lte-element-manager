package nrm

type ManagedObject interface {
	GetDN() DN
	GetType() MOType
	GetAttributes() map[string]any
}

func (o Object) GetDN() DN       { return o.DN }
func (o Object) GetType() MOType { return o.Type }
func (o Object) GetAttributes() map[string]any {
	return map[string]any{"name": o.Name}
}
