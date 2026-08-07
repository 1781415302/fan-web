package services

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/at-wat/ebml-go"
	"github.com/remko/go-mkvparse"
)

func TestMatroskaSubtitleParserReadsEmbeddedTextTrack(t *testing.T) {
	parser := newMatroskaSubtitleParser()
	beginMaster(t, parser, mkvparse.TracksElement)
	beginMaster(t, parser, mkvparse.TrackEntryElement)
	parser.HandleInteger(mkvparse.TrackNumberElement, 3, mkvparse.ElementInfo{})
	parser.HandleInteger(mkvparse.TrackTypeElement, matroskaSubtitleTrackType, mkvparse.ElementInfo{})
	parser.HandleString(mkvparse.CodecIDElement, "S_TEXT/UTF8", mkvparse.ElementInfo{})
	parser.HandleString(mkvparse.NameElement, "简体中文", mkvparse.ElementInfo{})
	endMaster(t, parser, mkvparse.TrackEntryElement)
	endMaster(t, parser, mkvparse.TracksElement)

	beginMaster(t, parser, mkvparse.ClusterElement)
	parser.HandleInteger(mkvparse.TimecodeElement, 2_000, mkvparse.ElementInfo{})
	var rawBlock bytes.Buffer
	if err := ebml.MarshalBlock(&ebml.Block{
		TrackNumber: 3,
		Timecode:    500,
		Data:        [][]byte{[]byte("第一行\n第二行")},
	}, &rawBlock); err != nil {
		t.Fatal(err)
	}
	if err := parser.HandleBinary(
		mkvparse.SimpleBlockElement,
		rawBlock.Bytes(),
		mkvparse.ElementInfo{},
	); err != nil {
		t.Fatal(err)
	}
	endMaster(t, parser, mkvparse.ClusterElement)

	track := parser.tracks[3]
	if track == nil || track.Label != "简体中文" {
		t.Fatalf("unexpected track: %#v", track)
	}
	if len(parser.cues) != 1 {
		t.Fatalf("expected one cue, got %d", len(parser.cues))
	}
	vtt := string(renderWebVTT(parser.cues))
	if !strings.Contains(vtt, "00:00:02.500 --> 00:00:07.500") {
		t.Fatalf("unexpected cue time: %s", vtt)
	}
	if !strings.Contains(vtt, "第一行\n第二行") {
		t.Fatalf("unexpected cue text: %s", vtt)
	}
}

func TestRenderWebVTTUsesBlockDuration(t *testing.T) {
	vtt := string(renderWebVTT([]subtitleCue{{
		start: 1500 * time.Millisecond,
		end:   4250 * time.Millisecond,
		text:  "字幕",
	}}))
	if !strings.Contains(vtt, "00:00:01.500 --> 00:00:04.250") {
		t.Fatalf("unexpected WebVTT: %s", vtt)
	}
}

func TestDecodeASSDialogueRemovesFormatting(t *testing.T) {
	input := `Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,{\i1}第一行{\i0}\N第二行`
	if got := decodeASSDialogue(input); got != "第一行\n第二行" {
		t.Fatalf("unexpected ASS text: %q", got)
	}
}

func TestReadRealMatroskaSubtitle(t *testing.T) {
	path := os.Getenv("FAN_WEB_REAL_MKV")
	if path == "" {
		t.Skip("set FAN_WEB_REAL_MKV to run the real-file subtitle check")
	}

	tracks, err := ReadMatroskaSubtitleTracks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) == 0 {
		t.Fatal("expected at least one embedded subtitle track")
	}

	track, vtt, err := ReadMatroskaSubtitleVTT(path, tracks[0].TrackNumber)
	if err != nil {
		t.Fatal(err)
	}
	if track.TrackNumber != tracks[0].TrackNumber {
		t.Fatalf("returned track %d, expected %d", track.TrackNumber, tracks[0].TrackNumber)
	}
	if !bytes.HasPrefix(vtt, []byte("WEBVTT\n")) {
		t.Fatalf("expected WebVTT output, got %q", vtt[:minInt(len(vtt), 32)])
	}
	if !strings.Contains(string(vtt), " --> ") {
		t.Fatal("expected at least one WebVTT cue")
	}
}

func beginMaster(t *testing.T, parser *matroskaSubtitleParser, id mkvparse.ElementID) {
	t.Helper()
	if _, err := parser.HandleMasterBegin(id, mkvparse.ElementInfo{}); err != nil {
		t.Fatal(err)
	}
}

func endMaster(t *testing.T, parser *matroskaSubtitleParser, id mkvparse.ElementID) {
	t.Helper()
	if err := parser.HandleMasterEnd(id, mkvparse.ElementInfo{}); err != nil {
		t.Fatal(err)
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
