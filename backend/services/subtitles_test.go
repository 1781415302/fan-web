package services

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
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

func TestDecodeASSDialogueMatroskaNineField(t *testing.T) {
	input := `0:00:01.00,0:00:03.00,Default,,0,0,0,,{\an8}第一行\N第二行`
	got := decodeASSDialogue(input)
	if got != "第一行\n第二行" {
		t.Fatalf("unexpected Matroska ASS text: %q", got)
	}
	if strings.Contains(got, `{\an8}`) {
		t.Fatalf("must not return raw {\\an8}: %q", got)
	}
}

func TestDecodeASSDialogueTooFewFieldsStillStripsTags(t *testing.T) {
	input := `{\an8}只有一行\h文本`
	got := decodeASSDialogue(input)
	if got != "只有一行 文本" {
		t.Fatalf("too-few-fields path should still strip tags, got %q", got)
	}
	if strings.Contains(got, "{") || strings.Contains(got, `{\an8}`) {
		t.Fatalf("must not return raw ASS tags: %q", got)
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

// TestSubtitleCacheBeginSharesResultIncludingError 是 singleflight 失败路径的核心回归：
// 旧实现首个请求解析失败时不写缓存、只关闭 done，所有 waiter 醒来发现缓存为空后各自
// 重新解析同一个 MKV，对损坏/异常文件反而放大了资源消耗。修复后 waiter 共享首次结果
// （含失败错误），不再重复解析。
func TestSubtitleCacheBeginSharesResultIncludingError(t *testing.T) {
	cache := &subtitleDocumentCache{
		entries:  make(map[subtitleCacheKey]*subtitleDocument),
		inflight: make(map[subtitleCacheKey]*subtitleInflight),
	}
	key := subtitleCacheKey{path: "/x.mkv", size: 1, modTime: 1}

	_, leader, finish := cache.begin(key)
	if !leader {
		t.Fatal("首次 begin 应为 leader")
	}

	const waiters = 5
	var wg sync.WaitGroup
	results := make([]error, waiters)
	registered := make(chan struct{}, waiters)
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e, isLeader, _ := cache.begin(key)
			if isLeader {
				t.Errorf("waiter %d 不应为 leader", i)
				return
			}
			// 先注册再等待，确保所有 waiter 都已挂到同一 in-flight 再 finish。
			registered <- struct{}{}
			<-e.done
			results[i] = e.err
		}(i)
	}
	for i := 0; i < waiters; i++ {
		<-registered
	}

	parseErr := fmt.Errorf("parse boom")
	finish(subtitleDocument{}, parseErr)

	wg.Wait()

	for i, err := range results {
		if !errors.Is(err, parseErr) {
			t.Fatalf("waiter %d 期望共享失败错误 %v，got %v", i, parseErr, err)
		}
	}

	// 失败不被缓存：finish 后 inflight 清空，新请求成为新 leader（可重试）。
	if _, ok := cache.inflight[key]; ok {
		t.Fatal("finish 后 inflight 应已清空")
	}
	_, leader2, _ := cache.begin(key)
	if !leader2 {
		t.Fatal("finish 后新请求应为新 leader")
	}
}

// TestSubtitleCacheBeginSharesSuccess 验证成功路径下 waiter 共享 leader 的解析结果。
func TestSubtitleCacheBeginSharesSuccess(t *testing.T) {
	cache := &subtitleDocumentCache{
		entries:  make(map[subtitleCacheKey]*subtitleDocument),
		inflight: make(map[subtitleCacheKey]*subtitleInflight),
	}
	key := subtitleCacheKey{path: "/y.mkv", size: 2, modTime: 2}

	_, leader, finish := cache.begin(key)
	if !leader {
		t.Fatal("应为 leader")
	}

	const waiters = 3
	var wg sync.WaitGroup
	got := make([]subtitleDocument, waiters)
	registered := make(chan struct{}, waiters)
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e, isLeader, _ := cache.begin(key)
			if isLeader {
				t.Errorf("waiter %d 不应为 leader", i)
				return
			}
			registered <- struct{}{}
			<-e.done
			got[i] = e.doc
		}(i)
	}
	for i := 0; i < waiters; i++ {
		<-registered
	}

	doc := subtitleDocument{
		tracks: map[uint64]*subtitleTrack{7: {SubtitleTrack: SubtitleTrack{TrackNumber: 7}}},
		cues:   map[uint64][]subtitleCue{},
	}
	finish(doc, nil)
	wg.Wait()

	for i, d := range got {
		if d.tracks[7] == nil || d.tracks[7].TrackNumber != 7 {
			t.Fatalf("waiter %d 期望共享解析结果", i)
		}
	}
}
