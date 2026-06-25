package handlers

import (
	"mime/multipart"
	"testing"
)

func TestMultipartFileFieldName(t *testing.T) {
	if got := multipartFileFieldName("file_1"); got != "file_file_1" {
		t.Fatalf("expected file_file_1, got %q", got)
	}
}

func TestCleanOriginalFilenameRemovesClientPath(t *testing.T) {
	if got := cleanOriginalFilename(`C:\fakepath\IMG_1.JPG`); got != "IMG_1.JPG" {
		t.Fatalf("expected base filename, got %q", got)
	}
}

func TestValidateGroupedUploadRequestRequiresMetadataFilePart(t *testing.T) {
	req := groupedUploadRequest{
		Groups: []uploadGroupInput{
			{
				ClientGroupID: "group_1",
				Files: []uploadFileInput{
					{
						ClientFileID:     "file_1",
						OriginalFilename: "IMG_1.jpg",
						MimeType:         "image/jpeg",
					},
				},
			},
		},
	}

	form := &multipart.Form{File: map[string][]*multipart.FileHeader{}}
	err := validateGroupedUploadRequest(req, 25*1024*1024, form)
	if err == nil {
		t.Fatal("expected missing file validation error")
	}
}

func TestValidateGroupedUploadRequestRejectsNoFiles(t *testing.T) {
	req := groupedUploadRequest{
		Groups: []uploadGroupInput{{ClientGroupID: "group_1"}},
	}

	form := &multipart.Form{File: map[string][]*multipart.FileHeader{}}
	err := validateGroupedUploadRequest(req, 25*1024*1024, form)
	if err == nil {
		t.Fatal("expected no files validation error")
	}
}
