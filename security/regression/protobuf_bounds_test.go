package regression

import (
	"testing"

	gogoproto "github.com/coreos/etcd/Godeps/_workspace/src/github.com/gogo/protobuf/proto"
	golangproto "github.com/coreos/etcd/Godeps/_workspace/src/github.com/golang/protobuf/proto"
)

type protobufProperties interface {
	Parse(string)
}

func TestProtobufFieldNumberBounds(t *testing.T) {
	tests := []struct {
		name string
		new  func() (protobufProperties, func() int)
	}{
		{
			name: "gogo",
			new: func() (protobufProperties, func() int) {
				p := &gogoproto.Properties{}
				return p, func() int { return p.Tag }
			},
		},
		{
			name: "golang",
			new: func() (protobufProperties, func() int) {
				p := &golangproto.Properties{}
				return p, func() int { return p.Tag }
			},
		},
	}

	for _, tt := range tests {
		p, tag := tt.new()
		p.Parse("varint,536870911,opt,name=value")
		if got := tag(); got != 536870911 {
			t.Errorf("%s maximum field number = %d, want 536870911", tt.name, got)
		}

		p, tag = tt.new()
		p.Parse("varint,536870912,opt,name=value")
		if got := tag(); got != 0 {
			t.Errorf("%s out-of-range field number = %d, want rejected value 0", tt.name, got)
		}
	}
}
