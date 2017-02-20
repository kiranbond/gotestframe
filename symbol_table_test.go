// Copyright (c) 2017 ZeroStack, Inc.
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

package gotestframe

import (
  "bytes"
  "encoding/json"
  "fmt"
  "testing"

  "github.com/gogo/protobuf/proto"
  "github.com/stretchr/testify/assert"

  "github.com/zerostackinc/customtypes"
  "github.com/zerostackinc/gotestframe/iprotos"
)

type myStruct struct {
  member int
}

type SampleStruct struct {
  Params []customtypes.RawMessage `json:"params"`
}

func TestSymbols(t *testing.T) {
  symbols := []*iprotos.TestSymbol{
    &iprotos.TestSymbol{
      Index: proto.Int32(0),
      Id:    proto.String(`%foo%`),
      Path:  proto.String(`a.b.c`),
    },
    &iprotos.TestSymbol{
      Index: proto.Int32(1),
      Id:    proto.String(`%bar%`),
      Path:  proto.String(`x.y`),
    },
    &iprotos.TestSymbol{
      Index: proto.Int32(1),
      Id:    proto.String(`%baz%`),
      Path:  proto.String(`a.b`),
    },
    &iprotos.TestSymbol{
      Index: proto.Int32(2),
      Id:    proto.String(`%bazinga%`),
      Path:  proto.String(`0.x`),
    },
    &iprotos.TestSymbol{
      Index: proto.Int32(2),
      Id:    proto.String(`%bazinga1%`),
      Path:  proto.String(`1.y`),
    },
  }
  sourceParams := []customtypes.RawMessage{
    customtypes.RawMessage(`
    {
      "a" : {
        "b" : {
          "c" : ["one", "two"]
        }
      }
    }`),
    customtypes.RawMessage(`
    {
      "x" : {
        "y": 3
      },
      "a" : {
        "b": "str"
      }
    }`),
    customtypes.RawMessage(`
    [
      {
        "x": 1,
        "y": "y"
      },
      {
        "x": 2,
        "y": "yy"
      }
    ]`),
  }

  symbolTable := NewSymbolTable()
  err := symbolTable.ExtractSymbols(symbols, sourceParams)
  assert.NoError(t, err)

  targetParams := []customtypes.RawMessage{
    customtypes.RawMessage(`""`),
    customtypes.RawMessage(`"raw"`),
    customtypes.RawMessage(`"%bar%"`),
    customtypes.RawMessage(`"%baz%"`),
    customtypes.RawMessage(`
    {
      "field1": "%foo%",
      "field2": "%bar%"
    }`),
    customtypes.RawMessage(`
    {
      "field3": {
        "field4": "%foo%"
      },
      "field5": {
        "field6": "%bar%"
      }
    }`),
    customtypes.RawMessage(`
    {
      "field7": "baz",
      "field8": "%baz%",
      "field9": "abc%baz%",
      "field10": "%baz%abc",
      "field11": "abc%baz%def",
      "field12": "abc%bar%",
      "field13": "%bar%abc",
      "field14": "abc%bar%def",
      "field15": "%bazinga%",
      "field16": "%bazinga1%"
    }`),
  }
  replacedParams, err := symbolTable.ReplaceSymbols(targetParams)
  assert.NoError(t, err)

  expected := []customtypes.RawMessage{
    customtypes.RawMessage(`""`),
    customtypes.RawMessage(`"raw"`),
    customtypes.RawMessage(`3`),
    customtypes.RawMessage(`"str"`),
    customtypes.RawMessage(`
    {
      "field1": ["one","two"],
      "field2": 3
    }`),
    customtypes.RawMessage(`
    {
      "field3": {
        "field4": ["one","two"]
      },
      "field5": {
        "field6": 3
      }
    }`),
    customtypes.RawMessage(`
    {
      "field7": "baz",
      "field8": "str",
      "field9": "abcstr",
      "field10": "strabc",
      "field11": "abcstrdef",
      "field12": "abc3",
      "field13": "3abc",
      "field14": "abc3def",
      "field15": 1,
      "field16": "yy"
    }`),
  }

  assert.Equal(t, len(expected), len(replacedParams))
  for ii, elem := range expected {
    assert.Equal(t, 0, bytes.Compare([]byte(elem), []byte(replacedParams[ii])),
      fmt.Sprintf("%s !=\n%s", string(elem), string(replacedParams[ii])))
  }

  badMessage := customtypes.RawMessage(`
    {
      "field1": "%foo"
    }`)
  badParams := []customtypes.RawMessage{badMessage}
  badReplaced, err := symbolTable.ReplaceSymbols(badParams)
  assert.Nil(t, badReplaced)
  assert.NotNil(t, err)
  expectedErr := fmt.Errorf("incomplete symbol: %s", `%foo`)
  assert.Equal(t, err.Error(), expectedErr.Error())

  s := SampleStruct{
    Params: replacedParams,
  }
  e := SampleStruct{
    Params: expected,
  }

  sampleJSON, err := json.Marshal(s)
  assert.NoError(t, err)

  expectedJSON, err := json.Marshal(e)
  assert.NoError(t, err)
  assert.Equal(t, 0, bytes.Compare(expectedJSON, sampleJSON),
    fmt.Sprintf("%s != %s", string(expectedJSON), string(sampleJSON)))
}
