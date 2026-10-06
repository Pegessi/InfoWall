package integration

import (
	"strings"
	"testing"
)

func TestComponentsValidateEmpty(t *testing.T) {
	t.Parallel()

	components := Components{}
	if !components.Empty() {
		t.Fatal("zero Components should be empty")
	}
	if err := components.Validate(); err != nil {
		t.Fatalf("zero Components should be valid: %v", err)
	}
}

func TestComponentsValidateValid(t *testing.T) {
	t.Parallel()

	components := Components{
		Connectors: []Connector{newFakeComponent("chat", RoleConnector)},
		Enrichers:  []Enricher{newFakeComponent("links", RoleEnricher)},
		Analyzer:   newFakeComponent("analysis", RoleAnalyzer),
		Exporters:  []Exporter{newFakeComponent("document", RoleExporter)},
	}
	if components.Empty() {
		t.Fatal("configured Components should not be empty")
	}
	if err := components.Validate(); err != nil {
		t.Fatalf("valid components: %v", err)
	}
}

func TestComponentsValidateRole(t *testing.T) {
	t.Parallel()

	components := Components{
		Enrichers: []Enricher{newFakeComponent("wrong-role", RoleExporter)},
	}
	err := components.Validate()
	if err == nil || !strings.Contains(err.Error(), `has role "exporter"; want "enricher"`) {
		t.Fatalf("expected role mismatch, got %v", err)
	}
}

func TestComponentsValidateDuplicateIDAcrossRoles(t *testing.T) {
	t.Parallel()

	components := Components{
		Enrichers: []Enricher{newFakeComponent("shared", RoleEnricher)},
		Analyzer:  newFakeComponent("shared", RoleAnalyzer),
	}
	err := components.Validate()
	if err == nil || !strings.Contains(err.Error(), `duplicate component id "shared"`) {
		t.Fatalf("expected duplicate id error, got %v", err)
	}
}

func TestComponentsValidateConnectorRequiresAnalyzer(t *testing.T) {
	t.Parallel()

	components := Components{Connectors: []Connector{newFakeComponent("chat", RoleConnector)}}
	err := components.Validate()
	if err == nil || !strings.Contains(err.Error(), "analyzer is required") {
		t.Fatalf("expected missing analyzer error, got %v", err)
	}
}

func TestComponentsValidateDescriptor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		descriptor Descriptor
		want       string
	}{
		{name: "empty id", descriptor: Descriptor{Role: RoleAnalyzer, ProtocolVersion: ProtocolVersion}, want: "component id is empty"},
		{name: "invalid role", descriptor: Descriptor{ID: "analysis", Role: "invalid", ProtocolVersion: ProtocolVersion}, want: "invalid role"},
		{name: "protocol version", descriptor: Descriptor{ID: "analysis", Role: RoleAnalyzer, ProtocolVersion: ProtocolVersion + 1}, want: "uses protocol version 2; want 1"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := (Components{Analyzer: &fakeComponent{descriptor: test.descriptor}}).Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestComponentsValidateNilComponent(t *testing.T) {
	t.Parallel()

	var typedNil *fakeComponent
	if (Components{Analyzer: typedNil}).Empty() {
		t.Fatal("typed nil analyzer should be treated as configured until validation")
	}
	tests := []struct {
		name       string
		components Components
		want       string
	}{
		{name: "nil connector", components: Components{Connectors: []Connector{nil}}, want: "connector[0]: component is nil"},
		{name: "typed nil enricher", components: Components{Enrichers: []Enricher{typedNil}}, want: "enricher[0]: component is nil"},
		{name: "typed nil analyzer", components: Components{Analyzer: typedNil}, want: "analyzer: component is nil"},
		{name: "nil exporter", components: Components{Exporters: []Exporter{nil}}, want: "exporter[0]: component is nil"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.components.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func newFakeComponent(id ComponentID, role Role) *fakeComponent {
	return &fakeComponent{descriptor: Descriptor{
		ID:              id,
		Role:            role,
		Name:            string(id),
		ProtocolVersion: ProtocolVersion,
	}}
}
