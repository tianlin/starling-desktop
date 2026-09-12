package app

import (
	"context"
	"encoding/json"
	"starling/internal/model"
	"starling/internal/testkit"
	"testing"
)

func TestCommentOrderDispatch(t *testing.T) {
	var got []model.CommentOrder
	f := &testkit.Fake{CommentsOrderedFunc: func(_ context.Context, _, _, _ string, order model.CommentOrder) (model.CommentPage, error) {
		got = append(got, order)
		return model.CommentPage{Items: []model.Comment{}, Complete: true}, nil
	}}
	s := fixture(t, f)
	epoch := connect(t, s)
	for _, order := range []string{"", "hot", "latest", "bogus"} {
		payload, _ := json.Marshal(map[string]any{"epoch": epoch, "episodeId": idA, "order": order})
		raw := s.Dispatch(context.Background(), "comments.list", string(payload))
		var res struct {
			OK    bool
			Error *model.AppError
		}
		if err := json.Unmarshal([]byte(raw), &res); err != nil {
			t.Fatal(err)
		}
		if order == "bogus" {
			if res.OK || res.Error.Code != "INVALID_ORDER" {
				t.Fatal(raw)
			}
		} else if !res.OK {
			t.Fatal(raw)
		}
	}
	if len(got) != 3 || got[0] != "hot" || got[1] != "hot" || got[2] != "latest" {
		t.Fatal(got)
	}
	raw := s.Dispatch(context.Background(), "comments.thread", `{"epoch":`+jsonNumber(epoch)+`,"episodeId":"`+idA+`","commentId":"`+idB+`","order":"latest"}`)
	var res struct{ OK bool }
	json.Unmarshal([]byte(raw), &res)
	if res.OK {
		t.Fatal("thread accepted primary order")
	}
}
