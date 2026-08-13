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

package main

import "testing"

func TestParsePort(t *testing.T) {
	tests := []struct {
		value string
		want  uint16
		valid bool
	}{
		{value: "2379", want: 2379, valid: true},
		{value: " 4001 ", want: 4001, valid: true},
		{value: "65535", want: 65535, valid: true},
		{value: "0"},
		{value: "-1"},
		{value: "65536"},
		{value: "131071"},
		{value: "not-a-port"},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := parsePort(test.value)
			if test.valid && err != nil {
				t.Fatalf("parsePort returned an error: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatalf("parsePort(%q) = %d, want an error", test.value, got)
			}
			if test.valid && got != test.want {
				t.Fatalf("parsePort(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}
