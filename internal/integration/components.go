package integration

import (
	"fmt"
	"reflect"
)

// Components is the complete set of integration components available to an
// InfoWall process. A zero Components value is valid.
type Components struct {
	Connectors []Connector
	Enrichers  []Enricher
	Analyzer   Analyzer
	Exporters  []Exporter
}

// Empty reports whether no component slots have been configured. A typed nil
// interface is considered configured and will be rejected by Validate.
func (c Components) Empty() bool {
	return len(c.Connectors) == 0 && len(c.Enrichers) == 0 && c.Analyzer == nil && len(c.Exporters) == 0
}

// Validate checks component descriptors, role assignments, and global ID
// uniqueness. A connector pipeline requires an analyzer.
func (c Components) Validate() error {
	seen := make(map[ComponentID]string)

	for i, connector := range c.Connectors {
		label := fmt.Sprintf("connector[%d]", i)
		if err := validateComponent(label, connector, RoleConnector, seen); err != nil {
			return err
		}
	}
	for i, enricher := range c.Enrichers {
		label := fmt.Sprintf("enricher[%d]", i)
		if err := validateComponent(label, enricher, RoleEnricher, seen); err != nil {
			return err
		}
	}
	if !nilComponent(c.Analyzer) {
		if err := validateComponent("analyzer", c.Analyzer, RoleAnalyzer, seen); err != nil {
			return err
		}
	} else if c.Analyzer != nil {
		return fmt.Errorf("analyzer: component is nil")
	}
	for i, exporter := range c.Exporters {
		label := fmt.Sprintf("exporter[%d]", i)
		if err := validateComponent(label, exporter, RoleExporter, seen); err != nil {
			return err
		}
	}

	if len(c.Connectors) > 0 && nilComponent(c.Analyzer) {
		return fmt.Errorf("analyzer is required when connectors are configured")
	}
	return nil
}

type component interface {
	Descriptor() Descriptor
}

func validateComponent(label string, candidate component, want Role, seen map[ComponentID]string) error {
	if nilComponent(candidate) {
		return fmt.Errorf("%s: component is nil", label)
	}
	descriptor := candidate.Descriptor()
	if err := descriptor.Validate(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if descriptor.Role != want {
		return fmt.Errorf("%s: component %q has role %q; want %q", label, descriptor.ID, descriptor.Role, want)
	}
	if previous, ok := seen[descriptor.ID]; ok {
		return fmt.Errorf("%s: duplicate component id %q (already used by %s)", label, descriptor.ID, previous)
	}
	seen[descriptor.ID] = label
	return nil
}

func nilComponent(candidate any) bool {
	if candidate == nil {
		return true
	}
	value := reflect.ValueOf(candidate)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
