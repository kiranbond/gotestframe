// Copyright 2015 ZeroStack, Inc.
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
//
// SymbolTable is a symbol to value mapping service for substitution in json.

package gotestframe

import (
  "encoding/json"
  "fmt"

  "github.com/Jeffail/gabs"
  "github.com/golang/glog"

  "zerostack/common/customtypes"
  "zerostack/integration/common/iprotos"
)

// SymbolTable maps string symbols from the test code to their byte slice
// values.
type SymbolTable struct {
  Table map[string][]byte
}

// NewSymbolTable returns a newly constructed symbol table.
func NewSymbolTable() *SymbolTable {
  return &SymbolTable{
    Table: map[string][]byte{},
  }
}

// PutSymbol sets the value of symbol "symbol" to value "value".
func (s *SymbolTable) PutSymbol(symbol string, value []byte) {
  s.Table[symbol] = value
}

// GetSymbol gets the value of symbol "symbol". The second return value is set
// to false if the symbol is not present.
func (s *SymbolTable) GetSymbol(symbol string) ([]byte, bool) {
  v, ok := s.Table[symbol]
  return v, ok
}

// Size returns the number of symbols in the table.
func (s *SymbolTable) Size() int {
  return len(s.Table)
}

// ExtractSymbols augments the symbol table with the values corresponding to the
// paths in the appropriate param. The list of symbols is specified in the
// testSymbols slice.
func (s *SymbolTable) ExtractSymbols(symbols []*iprotos.TestSymbol,
  params []customtypes.RawMessage) error {

  // We will not parse a param out unless a TestSymbol refers to it. As such,
  // we maintain a map of the index in the params slice to its corresponding
  // parsed Container.
  paramsMap := make(map[int32]*gabs.Container, 0)
  numParams := int32(len(params))
  for _, symbol := range symbols {
    glog.Infof("searching for symbol: %s", symbol)
    if _, found := s.GetSymbol(symbol.GetId()); found {
      glog.Warningf("symbol %s being overwritten", symbol.GetId())
    }
    symIndex := symbol.GetIndex()
    if symIndex >= numParams || symIndex < 0 {
      return fmt.Errorf("invalid symbol index: %d", symIndex)
    }
    // We treat the absence of a path field to mean that the param should be
    // used literally, i.e. verbatim.
    if symbol.Path == nil {
      s.PutSymbol(symbol.GetId(), []byte(params[symIndex]))
      glog.Infof("symbol[%s] value=%v", symbol.GetId(),
        string(params[symIndex]))
      continue
    }
    // If we do have a path, we require it to be non-empty.
    if symbol.GetPath() == "" {
      return fmt.Errorf("empty path specified for symbol %s", symbol.String())
    }
    value, ok := paramsMap[symIndex]
    if !ok {
      parsed, err := gabs.ParseJSON([]byte(params[symIndex]))
      if err != nil {
        return fmt.Errorf("%s is not valid JSON :: %v",
          string(params[symIndex]), err)
      }
      paramsMap[symIndex] = parsed
      value = paramsMap[symIndex]
    }
    path := value.PathElement(symbol.GetPath())
    if path == nil || path.Data() == nil {
      glog.Errorf("could not find data for symbol %s at path %s",
        symbol.GetId(), symbol.GetPath())
      return fmt.Errorf("could not match %s with %s", symbol.GetPath(),
        value.String())
    }
    glog.Infof("symbol[%s] at path[%s] value=%v", symbol.GetId(),
      symbol.GetPath(), path.Data())

    marshaled, err := json.Marshal(path.Data())
    if err != nil {
      return err
    }
    s.PutSymbol(symbol.GetId(), marshaled)
  }
  return nil
}

// ReplaceSymbols replaces known symbols in params with their
// corresponding values and returns a new slice of customtypes.RawMessage.
// Each symbol is expected to be begun and ended by the
// character '%'.
func (s *SymbolTable) ReplaceSymbols(
  params []customtypes.RawMessage) ([]customtypes.RawMessage, error) {

  var replaced []customtypes.RawMessage
  for _, param := range params {
    replacedParam, err := s.replaceSymbols(param)
    if err != nil {
      return nil, err
    }
    replaced = append(replaced, replacedParam)
  }
  return replaced, nil
}

// replaceSymbols replaces known symbols in a single param. This is the
// workhorse of the ReplaceSymbols function above.
func (s *SymbolTable) replaceSymbols(
  param customtypes.RawMessage) (customtypes.RawMessage, error) {

  // param needs to have at least one %.*% pattern to comprise a symbol.
  if len(param) < 2 {
    return param, nil
  }

  // The algorithm:
  // inSymbol := false
  // for (each character c in param) {
  //   if c == '%' {
  //     flip inSymbol
  //     if !inSymbol {
  //       lookup symbol
  //       if symbol found {
  //         if symbol value is a string {
  //           drop the surrounding quotes if replacing in a substring, else
  //           retain the quotes and replace the symbol with the value.
  //         } else {
  //           drop the quotes surrounding the destination if NOT in a
  //           substring and replace the symbol with the value
  //         }
  //       }
  //     }
  //   }
  // }

  replaced := customtypes.RawMessage{}
  if param[0] != '"' {
    replaced = append(replaced, param[0])
  }

  // Let us assume that that B represents an arbitrary byte. The algorithm that
  // we employ here implements the following simple regular grammar:
  // (B*(%B*%))*.
  inSymbol := false
  var symStart int
  symbolReplaced := false
  var previous byte
  ii := 1
  for ; ii < len(param)-1; ii++ {
    c := param[ii]
    switch c {
    case '%':
      inSymbol = !inSymbol
      if inSymbol {
        previous = param[ii-1]
        symStart = ii
      } else {
        sym := param[symStart : ii+1]
        if value, ok := s.GetSymbol(string(sym)); ok {
          valueStr := len(value) > 1 &&
            value[0] == '"' && value[len(value)-1] == '"'
          if valueStr {
            // If either the character before the symbol starts, or the
            // character after the symbol ends is not a quote, the replacement
            // is occurring as a substring.
            if previous == '"' {
              replaced = append(replaced, '"')
            }
            replaced = append(replaced, value[1:len(value)-1]...)
            if param[ii+1] == '"' {
              replaced = append(replaced, '"')
            }
          } else {
            // The conditions before and after the actual symbol replacement
            // together encapsulate "replacement as a substring". This is the
            // only case in which the quotes are retained.
            if previous == '"' && param[ii+1] != '"' {
              replaced = append(replaced, '"')
            }
            replaced = append(replaced, value...)
            if previous != '"' && param[ii+1] == '"' {
              replaced = append(replaced, '"')
            }
          }
          symbolReplaced = true
        } else {
          // TODO(kiran): we are treating this as error to make sure people have
          // not mistyped symbols. In future if we see %...% as organic strings
          // then we will have to just do append like the comment.
          //replaced = append(replaced, sym...)
          return nil, fmt.Errorf("did not find symbol for %s", string(sym))
        }
      }
    case '"':
      if inSymbol {
        return nil, fmt.Errorf("incomplete symbol: %s", param[symStart:ii])
      }
      // handle empty string, which has quotes back-to-back. The first part of
      // the quote is handled by this if case, the second half is handled by the
      // default case, where another single quote gets appended before comma
      if param[ii+1] == '"' {
        replaced = append(replaced, '"')
      }
    default:
      // The '"' token is always handled on the next token. As such, if a symbol
      // was not replaced just before the current token, the quote token has to
      // be retained.
      if param[ii-1] == '"' && !symbolReplaced {
        replaced = append(replaced, '"')
      }
      if !inSymbol {
        replaced = append(replaced, c)
      }
      symbolReplaced = false
    }
  }
  if inSymbol {
    return nil, fmt.Errorf("replaced symbols should be encapsulated in strings")
  }
  if !symbolReplaced {
    // Handle the previous quote, since a symbol replacement did not occur just
    // before this.
    if param[ii-1] == '"' {
      replaced = append(replaced, '"')
    }
    // Since symbol replacement did not occur, the last token should be
    // unchanged. Retain.
    replaced = append(replaced, param[ii])
  } else if param[ii] != '"' {
    // If the last token is not a quote, the input will not be modified just
    // around this token. Retain it as is.
    replaced = append(replaced, param[ii])
  }
  return replaced, nil
}
