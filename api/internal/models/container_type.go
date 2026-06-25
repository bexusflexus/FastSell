package models

import "time"

type ContainerType struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Description      *string    `json:"description"`
	Archived         bool       `json:"archived"`
	ArchivedDatetime *time.Time `json:"archived_datetime"`
	CreatedDatetime  time.Time  `json:"created_datetime"`
	UpdatedDatetime  *time.Time `json:"updated_datetime"`
}

type CreateContainerTypeRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type UpdateContainerTypeRequest struct {
	Name                *string
	NameSupplied        bool
	Description         *string
	DescriptionSupplied bool
	Archived            *bool
	ArchivedSupplied    bool
}

type ListContainerTypesResponse struct {
	ContainerTypes []ContainerType `json:"container_types"`
}

type ContainerTypeDeletePreview struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	CanDelete      bool    `json:"can_delete"`
	UsageCount     int     `json:"usage_count"`
	BlockingReason *string `json:"blocking_reason"`
}

type DeleteContainerTypeResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Deleted    bool   `json:"deleted"`
	UsageCount int    `json:"usage_count"`
}
