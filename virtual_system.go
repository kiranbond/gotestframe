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
  "crypto/tls"
  "encoding/json"
  "fmt"
  "io/ioutil"
  "net/http"
  "net/url"
  "os"
  "os/exec"
  "sync"
  "time"

  "github.com/gogo/protobuf/proto"
  "github.com/golang/glog"
  "github.com/pkg/errors"

  "github.com/zerostackinc/customtypes"
  "github.com/zerostackinc/gotestframe/iprotos"
)

const cZSClientTimeout time.Duration = 1800 * time.Second
const cOSClientTimeout time.Duration = 1 * time.Minute

// TestSystem encapsulates the state and structures needed to run an integration
// test.
type TestSystem struct {
  cleanupOnFail bool

  // clients
  httpClient *http.Client

  // symbol table for test symbols.
  symbolTable *SymbolTable
  opID        uint32
  opsWG       sync.WaitGroup
  opResult    map[uint32]error
}

// NewTestSystem creates and returns a new test system.
func NewTestSystem(cleanupOnFail bool) *TestSystem {

  cl := &http.Client{
    Transport: &http.Transport{
      TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
    },
  }

  return &TestSystem{
    symbolTable:   NewSymbolTable(),
    opResult:      make(map[uint32]error),
    cleanupOnFail: cleanupOnFail,
    httpClient:    cl,
  }
}

// SaveState saves the state of the TestSystem to a file so it can be
// reloaded to run more tests using the saved state.
func (s *TestSystem) SaveState(clusterFile, symbolFile string) error {
  err := s.SaveClusterStateAsOps(clusterFile)
  if err != nil {
    glog.Errorf("error saving TestSystem state :: %v", err)
    return fmt.Errorf("error saving TestSystem state :: %v", err)
  }

  err = s.SaveSymbols(symbolFile)
  if err != nil {
    glog.Errorf("error saving TestSystem symbols :: %v", err)
    return fmt.Errorf("error saving TestSystem symbols :: %v", err)
  }
  return nil
}

// SaveSymbols exports the symbols captured during execution into a file.
func (s *TestSystem) SaveSymbols(symbolFile string) error {
  symbols, err := json.MarshalIndent(*s.symbolTable, "", "  ")
  if err != nil {
    return fmt.Errorf("error marshaling symbols to json %v :: %v",
      s.symbolTable, err)
  }
  err = ioutil.WriteFile(symbolFile, symbols, os.FileMode(0644))
  if err != nil {
    return fmt.Errorf("error writing symbols %+v to file %s :: %v",
      symbols, symbolFile, err)
  }
  return nil
}

// LoadState loads the cluster state and symbols.
// For cluster state it just expect to run test ops to load cluster state.
// for symbols, just the symbol table is loaded.
func (s *TestSystem) LoadState(clusterFile, symbolFile string) error {
  err := s.LoadSymbols(symbolFile)
  if err != nil {
    return fmt.Errorf("error loading symbol state:: %v", err)
  }
  err = s.RunTestOps(clusterFile, nil)
  if err != nil {
    return fmt.Errorf("error running cluster ops:: %v", err)
  }
  return nil
}

// LoadSymbols loads the symbols from file into the symbol table.
func (s *TestSystem) LoadSymbols(symbolFile string) error {

  if len(symbolFile) == 0 {
    return fmt.Errorf("empty filename in input params")
  }

  var err error

  /// Load test system symbols from file
  err = LoadJSONTemplate(symbolFile, nil, s.symbolTable)
  if err != nil || s.symbolTable == nil {
    return fmt.Errorf("error in loading system symbols from %s :: %v",
      symbolFile, err)
  }

  glog.Infof("loaded %d symbols from:%s", s.symbolTable.Size(), symbolFile)

  return nil
}

// Destroy destroys the test system.
func (s *TestSystem) Destroy() error {
  glog.Infof("performing test system destroy")

  return nil
}

// RunTestOps loads, parses and runs the test ops in the file specified by
// testOpsFile.
func (s *TestSystem) RunTestOps(testOpsFile string, control *TestControl) error {

  // Load test ops from the file
  testOps := &iprotos.TestOps{}
  err := LoadJSONTemplate(testOpsFile, control, testOps)
  if err != nil || testOps == nil {
    return fmt.Errorf("error in loading test ops from %s :: %v",
      testOpsFile, err)
  }

  glog.Infof("testops:\n%+v", testOps)

  for _, op := range testOps.Ops {
    opID := s.opID
    s.opID++
    op.OpID = &opID
    // dispatch async if op is async
    if op.GetAsync() {
      s.opsWG.Add(1)
      glog.Infof("async executing test op[%d] %+v", op.GetOpID(), op.String())
      go func(op *iprotos.TestOp) {
        errOp := s.doTestOp(op)
        s.opResult[op.GetOpID()] = errOp
        if errOp != nil {
          glog.Errorf("error doing test op[%d] %+v :: %v", op.GetOpID(), op.String(),
            errOp)
        }
      }(op)

      continue
    }

    glog.Infof("executing test op[%d] %+v", opID, op.String())
    if errOp := s.doTestOp(op); errOp != nil {
      glog.Errorf("error doing test op[%d] %v :: %v", opID, op, errOp)
      return fmt.Errorf("error doing test op[%d] %v :: %v", opID, op, errOp)
    }
  }
  return nil
}

// NoOp does nothing and returns nil
func (s *TestSystem) NoOp(string) error {
  return nil
}

// WaitAsyncOps waits for all async ops to be completed. If input is not
// equal to 0 then it also verifies that the number of ops executed match
// input value.
func (s *TestSystem) WaitAsyncOps(exp int) error {
  s.opsWG.Wait()
  if exp != 0 && len(s.opResult) != exp {
    errM := fmt.Errorf("error in expected number of ops %d from %d", exp,
      len(s.opResult))
    glog.Errorf(errM.Error())
    return errM
  }
  // Check errors from all ops. We could optimize for storing and checking
  // only async ops but doesn't seem necessary.
  for id, res := range s.opResult {
    if res != nil {
      glog.Errorf("op[%d] failed with error : %v", id, res)
      return res
    }
  }
  return nil
}
