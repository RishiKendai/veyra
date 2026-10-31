// Package model holds UI-independent data types shared across veyra.
package model

import "time"

// MediaType is the kind of media a scan targets.
type MediaType string

const (
	Video MediaType = "video"
	Image MediaType = "image"
)

// File is a discovered, content-verified media file.
type File struct {
	Path    string
	Size    int64
	ModTime time.Time
	Dev     uint64
	Ino     uint64
	Nlink   uint64 // hard-link count; 0 when unknown
	Meta
}

// Meta is what content inspection learned about a media file.
type Meta struct {
	Format   string  // detected format/container, e.g. "jpeg", "mov,mp4,m4a,3gp"
	Width    int     // pixels (video: first video stream)
	Height   int     // pixels
	Duration float64 // seconds; video only, 0 if unknown
	Codec    string  // video codec; video only

	FrameRate  float64 // video average frames/s, 0 if unknown
	BitRate    int64   // video container bit/s, 0 if unknown
	AudioCodec string  // empty when the video has no audio stream
	ColorModel string  // image colour model and depth, e.g. "YCbCr (8-bit)"
}

// Skip records a file or directory that was not processed, and why.
type Skip struct {
	Path   string
	Reason string
}
