package store

type FailureType string

const (
	FailureTypeNoCaptions FailureType = "no_captions" // No captions available from yt itself, data is the video ID.
	FailureTypePageQuota  FailureType = "page_quota"  // Quota exceeded while fetching video pages, data is the page token that failed.
)

type TranscriptType string

const (
	TubeAuto    TranscriptType = "tube_auto"    // Auto generated YouTube.
	TubeManual  TranscriptType = "tube_manual"  // Manually added YouTube (creator or community).
	WhisperBase TranscriptType = "whisper_base" // OpenAI Whisper base model.
)
