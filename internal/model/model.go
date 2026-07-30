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
	LastModified string
}

type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

type Bucket struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

type Buckets struct {
	Buckets []Bucket `xml:"Bucket"`
}

type ListAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Xmlns   string   `xml:"xmlns,attr"`
	Owner   Owner    `xml:"Owner"`
	Buckets Buckets  `xml:"Buckets"`
}

type Content struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

type ListObjectsResult struct {
	XMLName     xml.Name  `xml:"ListObjectsResult"`
	Xmlns       string    `xml:"xmlns,attr"`
	Name        string    `xml:"Name"`
	Prefix      string    `xml:"Prefix"`
	KeyCount    int       `xml:"KeyCount"`
	MaxKeys     int       `xml:"MaxKeys"`
	IsTruncated bool      `xml:"IsTruncated"`
	Contents    []Content `xml:"Contents"`
}
