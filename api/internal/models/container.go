package models

import "time"

type Container struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Type                *string    `json:"type"`
	ContainerTypeID     *string    `json:"container_type_id"`
	ContainerTypeName   *string    `json:"container_type_name"`
	LocationID          *string    `json:"location_id"`
	LocationName        *string    `json:"location_name"`
	LocationDescription *string    `json:"location_description"`
	Notes               *string    `json:"notes"`
	CreatedDatetime     time.Time  `json:"created_datetime"`
	UpdatedDatetime     *time.Time `json:"updated_datetime"`
	Archived            bool       `json:"archived"`
	ArchivedDatetime    *time.Time `json:"archived_datetime"`
}

type CreateContainerRequest struct {
	Name                string  `json:"name"`
	Type                *string `json:"type"`
	ContainerTypeID     *string `json:"container_type_id"`
	LocationID          *string `json:"location_id"`
	LocationDescription *string `json:"location_description"`
	Notes               *string `json:"notes"`
}

type UpdateContainerRequest struct {
	Name                        *string
	NameSupplied                bool
	Type                        *string
	TypeSupplied                bool
	ContainerTypeID             *string
	ContainerTypeIDSupplied     bool
	LocationID                  *string
	LocationIDSupplied          bool
	LocationDescription         *string
	LocationDescriptionSupplied bool
	Notes                       *string
	NotesSupplied               bool
	Archived                    *bool
	ArchivedSupplied            bool
}

type ContainerSummary struct {
	Container Container                 `json:"container"`
	Summary   ContainerSummaryAggregate `json:"summary"`
}

type ContainerSummaryAggregate struct {
	UploadSessionCount   int        `json:"upload_session_count"`
	UploadGroupCount     int        `json:"upload_group_count"`
	ImageCount           int        `json:"image_count"`
	PendingImageCount    int        `json:"pending_image_count"`
	UploadedImageCount   int        `json:"uploaded_image_count"`
	ProcessingImageCount int        `json:"processing_image_count"`
	ProcessedImageCount  int        `json:"processed_image_count"`
	FailedImageCount     int        `json:"failed_image_count"`
	LatestUploadDatetime *time.Time `json:"latest_upload_datetime"`
}

type ContainerDeletePreview struct {
	Container Container                    `json:"container"`
	Counts    ContainerDeletePreviewCounts `json:"counts"`
}

type ContainerDeletePreviewCounts struct {
	Items          int `json:"items"`
	UploadSessions int `json:"upload_sessions"`
	UploadGroups   int `json:"upload_groups"`
	ImageAssets    int `json:"image_assets"`
	FilePaths      int `json:"file_paths"`
}

type ContainerDeleteResponse struct {
	ContainerID string                 `json:"container_id"`
	Deleted     ContainerDeletedCounts `json:"deleted"`
	Warnings    []string               `json:"warnings"`
}

type ContainerDeletedCounts struct {
	Containers       int `json:"containers"`
	Items            int `json:"items"`
	UploadSessions   int `json:"upload_sessions"`
	UploadGroups     int `json:"upload_groups"`
	ImageAssets      int `json:"image_assets"`
	FilesDeleted     int `json:"files_deleted"`
	FilesMissing     int `json:"files_missing"`
	FileDeleteErrors int `json:"file_delete_errors"`
}
