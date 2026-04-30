//go:build !netconf || !cgo

package netconfcm

import "fmt"

type extractedLeaf struct {
	Path  string
	Value string
	IsKey bool
}

func extractLeaves(string, string) ([]extractedLeaf, error) {
	return nil, fmt.Errorf("NETCONF CM mediation requires netconf+cgo build")
}

func validateYANGJSON(string, string) error {
	return fmt.Errorf("NETCONF CM mediation requires netconf+cgo build")
}
