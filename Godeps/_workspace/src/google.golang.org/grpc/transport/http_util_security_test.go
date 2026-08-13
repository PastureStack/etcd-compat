package transport

import (
	"testing"

	"github.com/coreos/etcd/Godeps/_workspace/src/golang.org/x/net/http2/hpack"
	"github.com/coreos/etcd/Godeps/_workspace/src/google.golang.org/grpc/codes"
)

func TestGRPCStatusBounds(t *testing.T) {
	valid := &decodeState{}
	valid.processHeaderField(hpack.HeaderField{Name: "grpc-status", Value: "14"})
	if valid.err != nil || valid.statusCode != codes.Unavailable {
		t.Fatalf("valid grpc-status: code=%v error=%v", valid.statusCode, valid.err)
	}

	for _, value := range []string{"-1", "4294967296", "not-a-number"} {
		state := &decodeState{}
		state.processHeaderField(hpack.HeaderField{Name: "grpc-status", Value: value})
		if state.err == nil {
			t.Errorf("grpc-status %q was accepted", value)
		}
	}
}
