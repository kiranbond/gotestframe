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

package gotestframe

import (
  "bytes"
  "encoding/json"
  "fmt"
  "io"
  "io/ioutil"
  "net/http"
  "net/http/httputil"
  "reflect"
  "regexp"
  "strconv"
  "strings"
  "time"

  "github.com/Jeffail/gabs"
  "github.com/golang/glog"

  "github.com/zerostackinc/customtypes"
  "github.com/zerostackinc/gotestframe/iprotos"
  "github.com/zerostackinc/invoke"
)

// invokeMethod calls the method specified in op on the interface any after
// pre-processing the input params and returns the results of the call as a
// slice of customtypes.RawMessage.
// The pre-processing step has two parts:
//   1) Extract symbols from op.Params as specified by op.InputSymbols.
//   2) Replace known symbols from the symbol table in op.Params with their
//      respective values.
// Once the method call is complete, the results are post-processed to extract
// the symbols as specified by op.OutputSymbols.
func (s *TestSystem) invokeMethod(any interface{}, op *iprotos.TestOp) error {

  if any == nil {
    return fmt.Errorf("cannot invokeMethod on nil interface")
  }

  err := s.symbolTable.ExtractSymbols(op.InputSymbols, op.Params)
  if err != nil {
    glog.Errorf("error extracting symbols: %v", err)
    return err
  }

  replacedParams, err := s.symbolTable.ReplaceSymbols(op.Params)
  if err != nil {
    glog.Errorf("error replacing input symbols: %v", err)
    return err
  }

  callStr := fmt.Sprintf("%s(", op.GetMethod())
  for _, item := range replacedParams {
    callStr += fmt.Sprintf("%s,", item.String())
  }
  callStr = strings.TrimSuffix(callStr, ",")
  callStr += ")"

  glog.Infof(callStr)

  invokeResults := invoke.CallFuncWithRaw(any, op.GetMethod(), replacedParams)
  if len(invokeResults) < 1 {
    glog.Warningf("expect to have at least one return value which is error")
    return nil
  }

  last := invokeResults[len(invokeResults)-1].Interface()

  err, ok := last.(error)
  numResults := len(invokeResults)

  if ok {
    if err != nil {
      return err
    }
    numResults--
  }

  results := []customtypes.RawMessage{}
  for ii := 0; ii < numResults; ii++ {
    elem := invokeResults[ii].Interface()
    marshaled, errM := json.Marshal(elem)
    if errM != nil {
      return errM
    }
    results = append(results, customtypes.RawMessage(marshaled))
  }

  if op.Results != nil {
    for _, expected := range op.Results {
      if expected != nil {
        if expected.Value == nil {
          return fmt.Errorf("no value specified for expected result")
        }
        index := int(expected.GetIndex())
        if index >= len(results) {
          return fmt.Errorf("expected result index %d is out of bounds",
            index)
        }
        expectedValues := []customtypes.RawMessage{*expected.Value}
        replacedValues, errR := s.symbolTable.ReplaceSymbols(expectedValues)
        if errR != nil || len(replacedValues) != 1 {
          return fmt.Errorf("error replacing symbols in %s :: %v",
            *expected.Value, errR)
        }
        replacedValue := replacedValues[0]
        if expected.Path == nil {
          if !reflect.DeepEqual(results[index], replacedValue) {
            return fmt.Errorf(
              "expected result %s does not match actual result %s",
              replacedValue, results[index])
          }
        } else {
          if expected.GetPath() == "" {
            return fmt.Errorf(
              "empty path specified for expected result with index: %d",
              index)
          }
          // The results are parsed to avoid returning error on marshaling
          // inconsistencies.
          exp, errParse := gabs.ParseJSON([]byte(replacedValue))
          if errParse != nil {
            return fmt.Errorf("expected %s is not valid JSON :: %v",
              replacedValue, errParse)
          }
          act, errParse := gabs.ParseJSON([]byte(results[index]))
          // This bit should never get executed. It is present only for
          // completeness.
          if errParse != nil {
            return fmt.Errorf("actual %s is not valid JSON :: %v",
              results[index], errParse)
          }
          actVal := act.Path(expected.GetPath())
          if !reflect.DeepEqual(exp, actVal) {
            return fmt.Errorf(
              "expected result at path %s (%s) does not match actual result %s",
              expected.GetPath(), replacedValue, results[index])
          }
        }
      }
    }
  }

  err = s.symbolTable.ExtractSymbols(op.OutputSymbols, results)
  if err != nil {
    glog.Errorf("error extracting output symbols: %v", err)
    return err
  }

  return nil
}

func (s *TestSystem) doSystemOp(t *iprotos.TestOp) error {
  return s.invokeMethod(s, t)
}

var matchQuotesRegexp = regexp.MustCompile(`"(.*)"`)

func (s *TestSystem) doHTTPOp(t *iprotos.TestOp) error {
  if len(t.Params) > 1 {
    return fmt.Errorf(
      "http ops should specify a single param matching the request body")
  }
  symErr := s.symbolTable.ExtractSymbols(t.InputSymbols, t.Params)
  if symErr != nil {
    glog.Errorf("error extracting symbols: %v", symErr)
    return symErr
  }

  replacedParams, err := s.symbolTable.ReplaceSymbols(t.Params)

  if err != nil {
    glog.Errorf("error replacing input symbols: %v", err)
    return err
  }

  var body io.Reader
  if len(replacedParams) != 0 {
    body = bytes.NewBuffer([]byte(replacedParams[0]))
  }
  op := t.GetHTTPOp()
  strEP := `"` + op.GetEndPoint() + `"`
  ep := customtypes.RawMessage(strEP)
  replacedEP, err := s.symbolTable.ReplaceSymbols([]customtypes.RawMessage{ep})
  if err != nil || len(replacedEP) != 1 {
    return fmt.Errorf("error replacing symbols in the endpoint: %s :: %v %d",
      op.GetEndPoint(), err, len(replacedEP))
  }
  epPieces := matchQuotesRegexp.FindSubmatch(replacedEP[0])
  if len(epPieces) != 2 {
    return fmt.Errorf("invalid symbol table replacement in the endpoint: %s",
      replacedEP[0])
  }

  url := fmt.Sprintf("%s/%s", s.getAPIProxyURL(), epPieces[1])
  req, err := http.NewRequest(op.GetMethod(), url, body)
  if err != nil {
    glog.Errorf("error performing http op: %s :: %v", t.String(), err)
    return err
  }
  for _, hdr := range op.ReqHeader {
    if v, ok := s.symbolTable.GetSymbol(hdr.GetValue()); ok {
      pieces := matchQuotesRegexp.FindSubmatch(v)
      if len(pieces) != 2 {
        return fmt.Errorf("invalid symbol value for header %s: %s",
          hdr.GetKey(), v)
      }
      req.Header.Add(hdr.GetKey(), string(pieces[1]))
    } else {
      req.Header.Add(hdr.GetKey(), hdr.GetValue())
    }
  }
  for _, sym := range op.ReqSymbol {
    if req.Header.Get(sym.GetKey()) != "" {
      str := `"` + req.Header.Get(sym.GetKey()) + `"`
      s.symbolTable.PutSymbol(sym.GetId(), []byte(str))
    } else {
      err = fmt.Errorf("error extracting request header %s", sym.GetKey())
      glog.Errorf(err.Error())
      return err
    }
  }
  reqBytes, err := httputil.DumpRequest(req, true)
  if err != nil {
    glog.Infof("error dumping request :: %v", err)
    return err
  }
  glog.Infof("Request: %s", reqBytes)
  cl := s.httpClient
  if op.Timeout != nil {
    timeout, errT := time.ParseDuration(op.GetTimeout())
    if errT != nil {
      glog.Errorf("bad timeout string in the http op %s :: %v",
        op.GetTimeout(), err)
      return err
    }
    cl = &http.Client{
      Timeout:   timeout,
      Transport: s.httpClient.Transport,
    }
  }
  resp, err := cl.Do(req)
  if err != nil {
    glog.Errorf("error performing http op: %s :: %v", t.String(), err)
    return err
  }
  respBytes, err := httputil.DumpResponse(resp, true)
  if err != nil {
    glog.Infof("error dumping request :: %v", err)
    return err
  }
  glog.Infof("Response: %s", respBytes)

  if op.StatusCode != nil && resp.StatusCode != int(op.GetStatusCode()) {
    return fmt.Errorf("unexpected status code, expected:%d, got:%d",
      op.GetStatusCode(), resp.StatusCode)
  }

  // Verify the response headers.
  for _, hdr := range op.RspHeader {
    v := resp.Header.Get(hdr.GetKey())
    if v != "" && v != hdr.GetValue() {
      return fmt.Errorf(
        "mismatched response header. expected: %s=%s, actual: %s=%s",
        hdr.GetKey(), hdr.GetValue(), hdr.GetKey(), v)
    }
  }
  for _, sym := range op.RspSymbol {
    if resp.Header.Get(sym.GetKey()) != "" {
      str := `"` + resp.Header.Get(sym.GetKey()) + `"`
      s.symbolTable.PutSymbol(sym.GetId(), []byte(str))
    } else {
      err = fmt.Errorf("error extracting response header %s. headers: %v",
        sym.GetKey(), resp.Header)
      glog.Errorf(err.Error())
      return err
    }
  }
  respBody, err := ioutil.ReadAll(resp.Body)
  if err != nil {
    glog.Errorf("error extracting response :: %v", err)
    return err
  }

  err = s.symbolTable.ExtractSymbols(t.OutputSymbols,
    []customtypes.RawMessage{customtypes.RawMessage(respBody)})
  if err != nil {
    glog.Errorf("error extracting output symbols :: %v", err)
    return err
  }
  return nil
}

// OutString is a wrapper for string.
type OutString struct {
  StringValue string `json:"string_value"`
}

// IntToString converts int to string.
func (s *TestSystem) IntToString(i int) *OutString {
  return &OutString{StringValue: strconv.Itoa(i)}
}

// AssertEqualStrings checks if two strings are equal or not.
func (s *TestSystem) AssertEqualStrings(str1, str2 string) error {
  if str1 != str2 {
    return fmt.Errorf("strings %s and %s are not equal", str1, str2)
  }
  return nil
}
