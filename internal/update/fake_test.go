package update

import (
	"context"
	"errors"
	"testing"
)

func TestFakeRecordsRequestsAndAnswers(t *testing.T) {
	isolate(t)
	f := &Fake{Result: Result{Output: []byte(answer)}}
	r := Request{Host: HostCodex, Input: []byte("batch"), CodexDisable: []string{"view_image"}}
	res, err := f.Run(context.Background(), r)
	if err != nil || string(res.Output) != answer {
		t.Fatalf("result %+v %v", res, err)
	}
	// The record is a copy: the caller may reuse its buffers.
	r.Input[0] = 'X'
	r.CodexDisable[0] = "changed"
	got := f.Requests()
	if len(got) != 1 || string(got[0].Input) != "batch" || got[0].CodexDisable[0] != "view_image" {
		t.Fatalf("requests %+v", got)
	}

	f.Respond = func(r Request) (Result, error) {
		return Result{Reason: ReasonRateLimit}, nil
	}
	if res, err = f.Run(context.Background(), r); err != nil || res.Reason != ReasonRateLimit {
		t.Fatalf("result %+v %v", res, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = f.Run(ctx, r); !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v", err)
	}
	if n := len(f.Requests()); n != 3 {
		t.Fatalf("%d requests recorded", n)
	}
}
