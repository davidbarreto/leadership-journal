package domain

import (
	"errors"
	"strings"
)

// Relationship says how a Person relates to the journal's owner.
type Relationship string

// The valid relationships.
const (
	RelationshipReport  Relationship = "report"
	RelationshipManager Relationship = "manager"
	RelationshipSelf    Relationship = "self"
)

// Problems reported (joined) by Person.Validate.
var (
	ErrNameRequired        = invalid("name is required")
	ErrInvalidRelationship = invalid("invalid relationship")
	ErrTeamNotAllowed      = invalid("only reports can belong to a team")
)

// Person is anyone the journal has entries about: a team member, the owner's
// manager, or the owner.
type Person struct {
	ID           int64
	Name         string
	Relationship Relationship
	RoleTitle    string
	// nil means no team (NULL column).
	TeamID *int64
}

// Validate returns every problem at once (joined), so a form can show them
// all in one round trip.
func (p Person) Validate() error {
	var errs []error

	if strings.TrimSpace(p.Name) == "" {
		errs = append(errs, ErrNameRequired)
	}

	switch p.Relationship {
	case RelationshipReport:
	case RelationshipManager, RelationshipSelf:
		if p.TeamID != nil {
			errs = append(errs, ErrTeamNotAllowed)
		}
	default:
		errs = append(errs, ErrInvalidRelationship)
	}

	return errors.Join(errs...)
}
