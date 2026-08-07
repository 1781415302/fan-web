package services

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/at-wat/ebml-go"
	"github.com/remko/go-mkvparse"
)

const matroskaSubtitleTrackType = 0x11

// SubtitleTrack describes one text subtitle track embedded in a Matroska file.
type SubtitleTrack struct {
	TrackNumber uint64 `json:"track_number"`
	Name        string `json:"name"`
	Language    string `json:"language"`
	Label       string `json:"label"`
}

type subtitleCue struct {
	start    time.Duration
	end      time.Duration
	text     string
	trackNum uint64
}

type subtitleTrack struct {
	SubtitleTrack
	codec     string
	trackType uint64
}

type pendingBlockGroup struct {
	cue      subtitleCue
	duration uint64
}

type matroskaSubtitleParser struct {
	stack         []mkvparse.ElementID
	tracks        map[uint64]*subtitleTrack
	currentTrack  *subtitleTrack
	clusterTime   int64
	timecodeScale uint64
	pendingGroup  *pendingBlockGroup
	cues          []subtitleCue
	parseError    error
}

func newMatroskaSubtitleParser() *matroskaSubtitleParser {
	return &matroskaSubtitleParser{
		tracks:        make(map[uint64]*subtitleTrack),
		timecodeScale: 1_000_000,
	}
}

func (p *matroskaSubtitleParser) HandleMasterBegin(
	id mkvparse.ElementID,
	info mkvparse.ElementInfo,
) (bool, error) {
	p.stack = append(p.stack, id)
	// Indexes, attachments and tags can be large and do not contain subtitle
	// cues needed by the Web player.
	switch id {
	case mkvparse.CuesElement,
		mkvparse.AttachmentsElement,
		mkvparse.ChaptersElement,
		mkvparse.TagsElement:
		return false, nil
	}

	if id == mkvparse.TrackEntryElement {
		p.currentTrack = &subtitleTrack{}
	}
	return true, nil
}

func (p *matroskaSubtitleParser) HandleMasterEnd(
	id mkvparse.ElementID,
	info mkvparse.ElementInfo,
) error {
	switch id {
	case mkvparse.TrackEntryElement:
		if p.currentTrack != nil &&
			p.currentTrack.trackType == matroskaSubtitleTrackType &&
			isSupportedSubtitleCodec(p.currentTrack.codec) {
			p.currentTrack.Label = subtitleTrackLabel(*p.currentTrack)
			p.tracks[p.currentTrack.TrackNumber] = p.currentTrack
		}
		p.currentTrack = nil
	case mkvparse.BlockGroupElement:
		if p.pendingGroup != nil {
			p.pendingGroup.cue.end = p.pendingGroup.cue.start +
				time.Duration(p.pendingGroup.duration)*time.Duration(p.timecodeScale)
			p.cues = append(p.cues, p.pendingGroup.cue)
			p.pendingGroup = nil
		}
	}

	if len(p.stack) > 0 {
		p.stack = p.stack[:len(p.stack)-1]
	}
	return nil
}

func (p *matroskaSubtitleParser) HandleString(
	id mkvparse.ElementID,
	value string,
	info mkvparse.ElementInfo,
) error {
	if p.currentTrack == nil || !p.inMaster(mkvparse.TrackEntryElement) {
		return nil
	}
	switch id {
	case mkvparse.CodecIDElement:
		p.currentTrack.codec = strings.TrimSpace(value)
	case mkvparse.NameElement:
		p.currentTrack.Name = strings.TrimSpace(value)
	case mkvparse.LanguageElement, mkvparse.LanguageIETFElement:
		if p.currentTrack.Language == "" {
			p.currentTrack.Language = strings.TrimSpace(value)
		}
	}
	return nil
}

func (p *matroskaSubtitleParser) HandleInteger(
	id mkvparse.ElementID,
	value int64,
	info mkvparse.ElementInfo,
) error {
	switch id {
	case mkvparse.TimecodeScaleElement:
		if p.inMaster(mkvparse.InfoElement) && value > 0 {
			p.timecodeScale = uint64(value)
		}
	case mkvparse.TrackNumberElement:
		if p.currentTrack != nil && p.inMaster(mkvparse.TrackEntryElement) {
			p.currentTrack.TrackNumber = uint64(value)
		}
	case mkvparse.TrackTypeElement:
		if p.currentTrack != nil && p.inMaster(mkvparse.TrackEntryElement) {
			p.currentTrack.trackType = uint64(value)
		}
	case mkvparse.TimecodeElement:
		if p.inMaster(mkvparse.ClusterElement) && p.topIs(mkvparse.ClusterElement) {
			p.clusterTime = value
		}
	case mkvparse.BlockDurationElement:
		if p.pendingGroup != nil && p.inMaster(mkvparse.BlockGroupElement) {
			p.pendingGroup.duration = uint64(value)
		}
	}
	return nil
}

func (p *matroskaSubtitleParser) HandleFloat(
	id mkvparse.ElementID,
	value float64,
	info mkvparse.ElementInfo,
) error {
	return nil
}

func (p *matroskaSubtitleParser) HandleDate(
	id mkvparse.ElementID,
	value time.Time,
	info mkvparse.ElementInfo,
) error {
	return nil
}

func (p *matroskaSubtitleParser) HandleBinary(
	id mkvparse.ElementID,
	value []byte,
	info mkvparse.ElementInfo,
) error {
	if id != mkvparse.SimpleBlockElement && id != mkvparse.BlockElement {
		return nil
	}
	block, err := ebml.UnmarshalBlock(bytes.NewReader(value), int64(len(value)))
	if err != nil {
		p.parseError = err
		return err
	}
	track := p.tracks[block.TrackNumber]
	if track == nil || !isSupportedSubtitleCodec(track.codec) {
		return nil
	}
	if len(block.Data) == 0 {
		return nil
	}

	startTicks := p.clusterTime + int64(block.Timecode)
	if startTicks < 0 {
		return nil
	}
	cue := subtitleCue{
		start:    time.Duration(startTicks) * time.Duration(p.timecodeScale),
		text:     decodeSubtitleText(track.codec, bytes.Join(block.Data, nil)),
		trackNum: block.TrackNumber,
	}
	if strings.TrimSpace(cue.text) == "" {
		return nil
	}
	if id == mkvparse.BlockElement && p.topIs(mkvparse.BlockGroupElement) {
		p.pendingGroup = &pendingBlockGroup{cue: cue}
	} else {
		p.cues = append(p.cues, cue)
	}
	return nil
}

func (p *matroskaSubtitleParser) inMaster(id mkvparse.ElementID) bool {
	for _, current := range p.stack {
		if current == id {
			return true
		}
	}
	return false
}

func (p *matroskaSubtitleParser) topIs(id mkvparse.ElementID) bool {
	return len(p.stack) > 0 && p.stack[len(p.stack)-1] == id
}

// ReadMatroskaSubtitleTracks returns text subtitle tracks embedded in a MKV.
// Non-Matroska files simply return no tracks because browser-native tracks are
// not available through the current stream endpoint.
func ReadMatroskaSubtitleTracks(path string) ([]SubtitleTrack, error) {
	document, err := parseMatroskaSubtitles(path)
	if err != nil {
		return nil, err
	}
	tracks := make([]SubtitleTrack, 0, len(document.tracks))
	for _, track := range document.tracks {
		tracks = append(tracks, track.SubtitleTrack)
	}
	sort.Slice(tracks, func(i, j int) bool {
		return tracks[i].TrackNumber < tracks[j].TrackNumber
	})
	return tracks, nil
}

// ReadMatroskaSubtitleVTT converts one embedded text track to WebVTT.
func ReadMatroskaSubtitleVTT(path string, trackNumber uint64) (SubtitleTrack, []byte, error) {
	document, err := parseMatroskaSubtitles(path)
	if err != nil {
		return SubtitleTrack{}, nil, err
	}
	track, ok := document.tracks[trackNumber]
	if !ok {
		return SubtitleTrack{}, nil, os.ErrNotExist
	}
	return track.SubtitleTrack, renderWebVTT(document.cues[trackNumber]), nil
}

type subtitleDocument struct {
	tracks map[uint64]*subtitleTrack
	cues   map[uint64][]subtitleCue
}

func parseMatroskaSubtitles(path string) (subtitleDocument, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".mkv" && extension != ".webm" {
		return subtitleDocument{
			tracks: make(map[uint64]*subtitleTrack),
			cues:   make(map[uint64][]subtitleCue),
		}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return subtitleDocument{}, err
	}
	defer file.Close()

	parser := newMatroskaSubtitleParser()
	if err := mkvparse.Parse(file, parser); err != nil {
		return subtitleDocument{}, err
	}
	if parser.parseError != nil {
		return subtitleDocument{}, parser.parseError
	}

	document := subtitleDocument{
		tracks: parser.tracks,
		cues:   make(map[uint64][]subtitleCue),
	}
	for _, cue := range parser.cues {
		document.cues[cue.trackNum] = append(document.cues[cue.trackNum], cue)
	}
	for trackNum := range document.cues {
		sort.SliceStable(document.cues[trackNum], func(i, j int) bool {
			return document.cues[trackNum][i].start < document.cues[trackNum][j].start
		})
	}
	return document, nil
}

func isSupportedSubtitleCodec(codec string) bool {
	switch strings.ToUpper(strings.TrimSpace(codec)) {
	case "S_TEXT/UTF8", "S_TEXT/ASS", "S_TEXT/SSA", "S_TEXT/WEBVTT":
		return true
	default:
		return false
	}
}

func subtitleTrackLabel(track subtitleTrack) string {
	if track.Name != "" {
		return track.Name
	}
	if track.Language != "" {
		return track.Language
	}
	return fmt.Sprintf("字幕 %d", track.TrackNumber)
}

func decodeSubtitleText(codec string, data []byte) string {
	text := strings.TrimSpace(string(data))
	if strings.EqualFold(codec, "S_TEXT/ASS") || strings.EqualFold(codec, "S_TEXT/SSA") {
		return decodeASSDialogue(text)
	}
	return strings.ReplaceAll(text, "\r\n", "\n")
}

func decodeASSDialogue(value string) string {
	line := strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(line), "dialogue:") {
		return line
	}
	fields := strings.SplitN(strings.TrimSpace(line[len("Dialogue:"):]), ",", 10)
	if len(fields) < 10 {
		return ""
	}
	text := fields[9]
	text = strings.ReplaceAll(text, `\N`, "\n")
	text = strings.ReplaceAll(text, `\n`, "\n")
	text = strings.ReplaceAll(text, `\h`, " ")
	return stripASSTags(text)
}

func stripASSTags(value string) string {
	result := strings.Builder{}
	inside := false
	for _, char := range value {
		switch char {
		case '{':
			inside = true
		case '}':
			inside = false
		default:
			if !inside {
				result.WriteRune(char)
			}
		}
	}
	return strings.TrimSpace(result.String())
}

func renderWebVTT(cues []subtitleCue) []byte {
	var output strings.Builder
	output.WriteString("WEBVTT\n\n")
	written := 0
	for index, cue := range cues {
		if strings.TrimSpace(cue.text) == "" {
			continue
		}
		end := cue.end
		if end <= cue.start {
			if index+1 < len(cues) && cues[index+1].start > cue.start {
				end = cues[index+1].start
			} else {
				end = cue.start + 5*time.Second
			}
		}
		if end <= cue.start {
			continue
		}
		written++
		fmt.Fprintf(&output, "%d\n%s --> %s\n%s\n\n", written,
			formatVTTTime(cue.start), formatVTTTime(end), cue.text)
	}
	return []byte(output.String())
}

func formatVTTTime(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	totalMilliseconds := value / time.Millisecond
	hours := totalMilliseconds / (60 * 60 * 1000)
	minutes := (totalMilliseconds / (60 * 1000)) % 60
	seconds := (totalMilliseconds / 1000) % 60
	milliseconds := totalMilliseconds % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, milliseconds)
}

var _ mkvparse.Handler = (*matroskaSubtitleParser)(nil)
