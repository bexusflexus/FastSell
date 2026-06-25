package handlers

import (
	"testing"

	"fastsell-api/internal/models"
)

func TestValidateCreateContainerRequestRequiresName(t *testing.T) {
	req := models.CreateContainerRequest{Name: "   "}
	normalizeCreateContainerRequest(&req)

	if err := validateCreateContainerRequest(req); err == nil {
		t.Fatal("expected validation error for blank name")
	}
}

func TestNormalizeCreateContainerRequestTrimsOptionalFields(t *testing.T) {
	containerType := " shelf "
	structuredContainerTypeID := " 123e4567-e89b-12d3-a456-426614174001 "
	locationID := " 123e4567-e89b-12d3-a456-426614174000 "
	location := " North wall "
	notes := " fragile items "
	req := models.CreateContainerRequest{
		Name:                " Garage Shelf A ",
		Type:                &containerType,
		ContainerTypeID:     &structuredContainerTypeID,
		LocationID:          &locationID,
		LocationDescription: &location,
		Notes:               &notes,
	}

	normalizeCreateContainerRequest(&req)

	if req.Name != "Garage Shelf A" {
		t.Fatalf("expected trimmed name, got %q", req.Name)
	}
	if req.Type == nil || *req.Type != "shelf" {
		t.Fatalf("expected trimmed type, got %#v", req.Type)
	}
	if req.ContainerTypeID == nil || *req.ContainerTypeID != "123e4567-e89b-12d3-a456-426614174001" {
		t.Fatalf("expected trimmed container_type_id, got %#v", req.ContainerTypeID)
	}
	if req.LocationID == nil || *req.LocationID != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("expected trimmed location_id, got %#v", req.LocationID)
	}
	if req.LocationDescription == nil || *req.LocationDescription != "North wall" {
		t.Fatalf("expected trimmed location, got %#v", req.LocationDescription)
	}
	if req.Notes == nil || *req.Notes != "fragile items" {
		t.Fatalf("expected trimmed notes, got %#v", req.Notes)
	}
}
