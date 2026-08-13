// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package command

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedTLSFile(t *testing.T) {
	root, err := ioutil.TempDir("", "etcdctl-tls-root")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	outside, err := ioutil.TempDir("", "etcdctl-tls-outside")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outside)
	if err = os.Mkdir(filepath.Join(root, "client"), 0700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "client", "cert.pem")
	if err = ioutil.WriteFile(want, []byte("certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "secret.pem")
	if err = ioutil.WriteFile(outsideFile, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := managedTLSFile(want, want)
	if err != nil || got != want {
		t.Fatalf("valid TLS path = %q, error = %v; want %q", got, err, want)
	}
	for _, malicious := range []string{
		outsideFile,
		want + "\n" + outsideFile,
		filepath.Join(root, "missing.pem"),
	} {
		if _, err := managedTLSFile(want, malicious); err == nil {
			t.Errorf("managedTLSFile accepted %q", malicious)
		}
	}
	symlink := filepath.Join(root, "client", "managed.pem")
	if err = os.Symlink(outsideFile, symlink); err == nil {
		if _, err := managedTLSFile(symlink, symlink); err == nil {
			t.Errorf("managedTLSFile accepted symlink %q", symlink)
		}
	}
}

func TestArgOrStdin(t *testing.T) {
	tests := []struct {
		args  []string
		stdin string
		i     int
		w     string
		we    error
	}{
		{
			args: []string{
				"a",
			},
			stdin: "b",
			i:     0,
			w:     "a",
			we:    nil,
		},
		{
			args: []string{
				"a",
			},
			stdin: "b",
			i:     1,
			w:     "b",
			we:    nil,
		},
		{
			args: []string{
				"a",
			},
			stdin: "",
			i:     1,
			w:     "",
			we:    ErrNoAvailSrc,
		},
	}

	for i, tt := range tests {
		var b bytes.Buffer
		b.Write([]byte(tt.stdin))
		g, ge := argOrStdin(tt.args, &b, tt.i)
		if g != tt.w {
			t.Errorf("#%d: expect %v, not %v", i, tt.w, g)
		}
		if ge != tt.we {
			t.Errorf("#%d: expect %v, not %v", i, tt.we, ge)
		}
	}
}
