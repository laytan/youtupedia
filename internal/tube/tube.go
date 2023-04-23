package tube

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	EndpointChannels      = "https://youtube.googleapis.com/youtube/v3/channels"
	EndpointPlaylistItems = "https://www.googleapis.com/youtube/v3/playlistItems"
	EndpointCaptions      = "https://www.googleapis.com/youtube/v3/captions"
	EndpointVideo         = "https://www.googleapis.com/youtube/v3/videos"
)

type Thumbnail struct {
	Url    string
	Width  int
	Height int
}
type Client struct {
	Key string
}

type ChannelInfo struct {
	Id             string
	ContentDetails struct {
		RelatedPlaylists struct {
			Uploads string
		}
	}
	Snippet struct {
		Title string
		// Description string
		Thumbnails map[string]Thumbnail
	}
}

// More is returned, this just outlines what we actually use.
type ResChannelInfo struct {
	Items []ChannelInfo
}

var ErrQuotaExceeded = errors.New("quota exceeded")

// Uses 1 quota.
func (c *Client) ChannelInfo(id string) (*ChannelInfo, error) {
	res, err := http.Get(
		fmt.Sprintf(
			"%s?part=contentDetails,snippet&id=%s&key=%s",
			EndpointChannels,
			id,
			c.Key,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("retrieving channel info for %q: %v", id, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("[ERROR]: reading response body: %v", body)
	}

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusForbidden {
			return nil, ErrQuotaExceeded
		}

		return nil, fmt.Errorf(
			"channel info request responded with status code %d: %q",
			res.StatusCode,
			string(body),
		)
	}

	result := ResChannelInfo{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("[ERROR]: unmarshalling response to struct: %v", err)
	}

	if len(result.Items) != 1 {
		return nil, fmt.Errorf(
			"[ERROR]: resulting channel info %d items, expected 1",
			len(result.Items),
		)
	}

	return &result.Items[0], nil
}

type ResPlaylistItems struct {
	NextPageToken string `json:",omitempty"`
	Items         []PlaylistItem
	PageInfo      struct {
		TotalResults   int
		ResultsPerPage int
	}
}

type PlaylistItem struct {
	ContentDetails struct {
		VideoId          string
		VideoPublishedAt string
	}
	Snippet struct {
		Title       string
		Description string
		Thumbnails  map[string]Thumbnail
	}
	Status struct {
		PrivacyStatus string
	}
}

func (c *Client) PlaylistItems(playlistId string, token string) (*ResPlaylistItems, error) {
	path := fmt.Sprintf(
		"%s?part=contentDetails,snippet,status&playlistId=%s&key=%s&maxResults=50",
		EndpointPlaylistItems,
		playlistId,
		c.Key,
	)
	if token != "" {
		path += "&pageToken=" + token
	}

	res, err := http.Get(path)
	if err != nil {
		return nil, fmt.Errorf("retrieving playlist %q videos: %w", playlistId, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response of playlist items %q: %w", playlistId, err)
	}

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusForbidden {
			return nil, ErrQuotaExceeded
		}

		return nil, fmt.Errorf(
			"status code %d when retrieving playlist %q's videos: %q",
			res.StatusCode,
			playlistId,
			string(body),
		)
	}

	result := ResPlaylistItems{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshalling response of playlist items %q: %w", playlistId, err)
	}

	return &result, nil
}

func (c *Client) EachPlaylistItemPage(
	playlistId string,
	f func(*ResPlaylistItems, string, error) bool,
) error {
	var token string
	for {
		items, err := c.PlaylistItems(playlistId, token)
		cont := f(items, token, err)
		if !cont {
			return nil
		}

		if items.NextPageToken == "" {
			return nil
		}

		token = items.NextPageToken
	}
}

type ResCaptionsList struct {
	PlayerCaptionsTrackListRenderer struct {
		CaptionTracks []ResTrack
		// There is more, ex:
		// AudioTracks
		// TranslationLanguages
	}
}

type ResTrack struct {
	BaseUrl string
	Name    struct {
		SimpleText string
	}
	LanguageCode   string
	Kind           string
	IsTranslatable bool
}

type Transcript struct {
	Entries []struct {
		Text  string  `xml:",chardata"`
		Start float64 `xml:"start,attr"`
		Dur   float32 `xml:"dur,attr"`
	} `xml:"text"`
}

var (
	ErrNotOk          = errors.New("unexpected non 200 status code")
	ErrToManyRequests = errors.New("too many requests")
	ErrNoCaptions     = errors.New("no caption tracks")
	ErrUnavailable    = errors.New("video unavailable")
)

type TranscriptType int

const (
	TypeNone TranscriptType = iota
	TypeAuto
	TypeManual
)

func (c *Client) Captions(videoId string) (*Transcript, TranscriptType, error) {
	res, err := http.Get(fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoId))
	if err != nil {
		return nil, 0, fmt.Errorf("requesting watch page: %w", err)
	}
	defer res.Body.Close()

	content, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("reading response body: %w", err)
	}
	sContent := string(content)

	if strings.Contains(sContent, `action="https://consent.youtube.com/s"`) {
		return nil, 0, fmt.Errorf("got consent form, this was never shown in testing")
	}

	if res.StatusCode != 200 {
		return nil, 0, fmt.Errorf(
			"got code %d with body %q: %w",
			res.StatusCode,
			sContent,
			ErrNotOk,
		)
	}

	split := strings.Split(sContent, `"captions":`)
	if len(split) <= 1 {
		if strings.Contains(sContent, `class="g-recaptcha"`) {
			return nil, 0, fmt.Errorf("video %q got captcha: %w", videoId, ErrToManyRequests)
		}

		// TODO: doesn't seem to get here.
		if strings.Contains(sContent, `"playabilityStatus"`) &&
			strings.Contains(sContent, `"ERROR"`) {
			return nil, 0, fmt.Errorf(
				"video %q not playable, maybe unlisted?: %w",
				videoId,
				ErrUnavailable,
			)
		}

		return nil, 0, fmt.Errorf("no captions json: %w", ErrNoCaptions)
	}

	rawCaptions := strings.ReplaceAll(strings.Split(split[1], `,"videoDetails`)[0], "\n", "")
	captionsList := ResCaptionsList{}
	if err := json.Unmarshal([]byte(rawCaptions), &captionsList); err != nil {
		return nil, 0, fmt.Errorf("could not unmarshal caption results %q: %w", rawCaptions, err)
	}

	track, trackType := bestTrack(captionsList.PlayerCaptionsTrackListRenderer.CaptionTracks)
	if trackType == TypeNone {
		return nil, 0, ErrNoCaptions
	}

	res, err = http.Get(track.BaseUrl)
	if err != nil {
		return nil, 0, fmt.Errorf("captions request: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("reading captions body: %w", err)
	}

	if res.StatusCode != 200 {
		return nil, 0, fmt.Errorf("captions file status code %d: %w", res.StatusCode, ErrNotOk)
	}

	transcript := Transcript{}
	if err := xml.Unmarshal(body, &transcript); err != nil {
		return nil, 0, fmt.Errorf("could not parse transcript xml %q: %w", body, err)
	}

	return &transcript, trackType, nil
}

type ResVideos struct {
	Items []ResVideo
	// There is more but not needed.
}

type ResVideo struct {
	Snippet struct {
		PublishedAt          string
		ChannelId            string
		Title                string
		Description          string
		Thumbnails           map[string]Thumbnail
		LiveBroadcastContent string
		// There is more but not needed.
	}
}

func (r *ResVideo) IsBroadcast() bool {
	return r.Snippet.LiveBroadcastContent != "none"
}

var ErrNotFound = errors.New("not found")

func (c *Client) Video(id string) (*ResVideo, error) {
	res, err := http.Get(fmt.Sprintf("%s?part=snippet&id=%s&key=%s", EndpointVideo, id, c.Key))
	if err != nil {
		return nil, fmt.Errorf("video %q request: %w", id, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading videos %q body: %w", id, err)
	}

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("videos status code %d: %w", res.StatusCode, ErrNotOk)
	}

	result := ResVideos{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshalling videos response %q: %w", string(body), err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("videos result has no items: %w", ErrNotFound)
	}

	return &result.Items[0], nil
}

var thumbResses = []string{"maxres", "high", "medium", "standard", "default"}

func HighestResThumbnail(thumbs map[string]Thumbnail) Thumbnail {
	for _, res := range thumbResses {
		if thumb, ok := thumbs[res]; ok {
			return thumb
		}
	}

	return Thumbnail{
		Url:    "https://placehold.co/600x400?text=No+Thumbnail",
		Width:  600,
		Height: 400,
	}
}

func ParsePublishedTime(value string) (time.Time, error) {
	published, err := time.Parse("2006-01-02T15:04:05Z", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse published time %q: %w", value, err)
	}

	return published, nil
}

// Returns the "best" track, which is an english track non automatic.
// Then goes for non-english non-automatic,
// Then for english automatic,
// Then for non-english automatic.
func bestTrack(tracks []ResTrack) (*ResTrack, TranscriptType) {
	for _, t := range tracks {
		if t.LanguageCode == "en" && t.Kind != "asr" {
			return &t, TypeManual
		}
	}

	for _, t := range tracks {
		if t.LanguageCode == "en" {
			return &t, TypeAuto
		}
	}

	for _, t := range tracks {
		if t.Kind != "asr" {
			return &t, TypeManual
		}
	}

	if len(tracks) > 0 {
		return &tracks[0], TypeAuto
	}

	return nil, TypeNone
}
