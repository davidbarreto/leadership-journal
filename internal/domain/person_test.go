package domain

import (
	"errors"
	"testing"
)

func TestPerson_Validate(t *testing.T) {
	team := int64(1)

	tests := []struct {
		name    string
		p       Person
		wantErr []error
	}{
		{name: "valid report with team", p: Person{Name: "Ana", Relationship: RelationshipReport, TeamID: &team}},
		{name: "valid report without team", p: Person{Name: "Ana", Relationship: RelationshipReport}},
		{name: "valid manager", p: Person{Name: "Bob", Relationship: RelationshipManager}},
		{name: "valid self", p: Person{Name: "Me", Relationship: RelationshipSelf}},
		{name: "blank name", p: Person{Name: "   ", Relationship: RelationshipReport}, wantErr: []error{ErrNameRequired}},
		{name: "unknown relationship", p: Person{Name: "Ana", Relationship: "friend"}, wantErr: []error{ErrInvalidRelationship}},
		{name: "empty relationship", p: Person{Name: "Ana"}, wantErr: []error{ErrInvalidRelationship}},
		{name: "manager cannot have a team", p: Person{Name: "Bob", Relationship: RelationshipManager, TeamID: &team}, wantErr: []error{ErrTeamNotAllowed}},
		{name: "self cannot have a team", p: Person{Name: "Me", Relationship: RelationshipSelf, TeamID: &team}, wantErr: []error{ErrTeamNotAllowed}},
		{name: "all problems are reported together", p: Person{Name: "", Relationship: "friend"}, wantErr: []error{ErrNameRequired, ErrInvalidRelationship}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if len(tc.wantErr) == 0 {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			for _, want := range tc.wantErr {
				if !errors.Is(err, want) {
					t.Errorf("Validate() = %v, want it to include %v", err, want)
				}
			}
		})
	}
}
