package model

import "encoding/xml"

type BucketAlreadyExistError struct {
	Code    string
	Message string
}

type S3Error struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource"`
	RequestId string   `xml:"RequestId"`
}

type Object struct {
	BucketName   string
	Key          string
	Size         int64
	Etag         string
	StorageClass string
	CreatedBy    string
}
