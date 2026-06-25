package models

import "time"

type InventoryGroup struct {
	ID              string     `json:"id"`
	Code            string     `json:"code"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Archived        bool       `json:"archived"`
	CreatedDatetime time.Time  `json:"created_datetime"`
	UpdatedDatetime *time.Time `json:"updated_datetime"`
}

type CreateInventoryGroupRequest struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type UpdateInventoryGroupRequest struct {
	Code                *string
	CodeSupplied        bool
	Name                *string
	NameSupplied        bool
	Description         *string
	DescriptionSupplied bool
	Archived            *bool
	ArchivedSupplied    bool
}

type ListInventoryGroupsResponse struct {
	InventoryGroups []InventoryGroup `json:"inventory_groups"`
}

type InventoryGroupReferenceCounts struct {
	Items        int `json:"items"`
	UploadGroups int `json:"upload_groups"`
}

type DeleteInventoryGroupBlockedResponse struct {
	Message string                        `json:"message"`
	Counts  InventoryGroupReferenceCounts `json:"counts"`
}
