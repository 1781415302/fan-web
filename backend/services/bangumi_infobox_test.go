package services

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestGetSubjectInfobox(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantAliases []string
	}{
		{
			name:        "string value",
			body:        `{"id":604826,"name":"超かぐや姫！","name_cn":"超时空辉夜姬！","summary":"s","total_episodes":1,"images":{"large":"https://example.com/c.jpg"},"infobox":[{"key":"别名","value":"Cosmic Princess Kaguya!"}]}`,
			wantAliases: []string{"Cosmic Princess Kaguya!"},
		},
		{
			name:        "array of v",
			body:        `{"id":604826,"name":"超かぐや姫！","name_cn":"超时空辉夜姬！","infobox":[{"key":"别名","value":[{"v":"Chou Kaguya-hime!"},{"v":"Cosmic Princess Kaguya!"}]}]}`,
			wantAliases: []string{"Chou Kaguya-hime!", "Cosmic Princess Kaguya!"},
		},
		{
			name:        "missing infobox",
			body:        `{"id":1001,"name":"Bocchi the Rock!","name_cn":"孤独摇滚！","summary":"summary","total_episodes":12,"images":{"large":"https://example.com/cover.jpg"}}`,
			wantAliases: nil,
		},
		{
			name:        "bad infobox value",
			body:        `{"id":1,"name":"x","infobox":[{"key":"别名","value":123}]}`,
			wantAliases: nil,
		},
		{
			name:        "bad infobox shape",
			body:        `{"id":1,"name":"x","infobox":"nope"}`,
			wantAliases: nil,
		},
		{
			name:        "empty alias strings discarded",
			body:        `{"id":1,"name":"x","infobox":[{"key":"别名","value":[{"v":""},{"v":"Kept"}]}]}`,
			wantAliases: []string{"Kept"},
		},
		{
			name:        "other keys ignored",
			body:        `{"id":1,"name":"x","infobox":[{"key":"中文名","value":"不是别名"},{"key":"别名","value":"Real Alias"}]}`,
			wantAliases: []string{"Real Alias"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := bangumiServiceWithSubjectJSON(test.body)
			got, err := svc.GetSubject(604826)
			if err != nil {
				t.Fatalf("GetSubject error: %v", err)
			}
			if !reflect.DeepEqual(got.Aliases, test.wantAliases) {
				t.Fatalf("Aliases = %#v, want %#v", got.Aliases, test.wantAliases)
			}
		})
	}
}

func TestGetSubjectAliasesJSONTag(t *testing.T) {
	field, ok := reflect.TypeOf(BangumiSubjectInfo{}).FieldByName("Aliases")
	if !ok {
		t.Fatal("BangumiSubjectInfo.Aliases missing")
	}
	if tag := field.Tag.Get("json"); tag != "-" {
		t.Fatalf("Aliases json tag = %q, want -", tag)
	}

	raw, err := json.Marshal(BangumiSubjectInfo{
		ID:      1,
		Name:    "x",
		Aliases: []string{"secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Aliases") || strings.Contains(string(raw), "secret") {
		t.Fatalf("Aliases leaked in JSON: %s", raw)
	}
}

func bangumiServiceWithSubjectJSON(body string) *BangumiService {
	return &BangumiService{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}}
}
