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
// Package to run a test specified by a list of files as input operations.
// Ops are expected to create cluster and required accounts and openstack
// install before doing cloud operations.

package gotestframe

import (
  "flag"
  "fmt"
  "testing"
  "time"

  "github.com/golang/glog"
  "github.com/stretchr/testify/assert"
  "github.com/stretchr/testify/suite"
)

var (
  testFile = flag.String("test_file", "",
    "SingleTest definition file in JSON format")
  controlFilesDir = flag.String("control_files_dir", "",
    "ControlFile Dir definition file in JSON format")
  controlFiles = flag.String("control_files", "",
    "ControlFile definition file in JSON format")
)

type TestController struct {
  LoopCount int
}

type singleRunnerTestSuite struct {
  suite.Suite
  testSys    *TestSystem
  testFailed bool
}

func newSingleRunnerTestSuite(t *testing.T) *singleRunnerTestSuite {
  ts := &singleRunnerTestSuite{}
  return ts
}

func (s *singleRunnerTestSuite) SetupSuite() {
  t := s.T()

  glog.Infof("setting up test runner suite, cleanup_vc: %v, "+
    "cleanup_on_fail:%v", *cleanupOnSuccess, *cleanupOnFail)

  assert.NotEmpty(t, *testFile, "test definition filename is empty")
}

func (s *singleRunnerTestSuite) TearDownSuite() {

  glog.Infof("performing test runner suite cleanup %v", s.testFailed)

  err := TearDownSingleTest(s.testSys, s.testFailed, *cleanupOnSuccess,
    *cleanupOnFail, *jJobID, "")
  assert.NoError(s.T(), err)

}

func (s *singleRunnerTestSuite) TestSingle() {
  t := s.T()

  s.testSys = NewTestSystem(*cleanupOnFail, *serviceTimeout,
    *openstackSetupTimeout, *openstackOpTimeout)
  assert.NotNil(t, s.testSys)

  // Since go2xunit parsing is not good at parsing nested test results,
  // we close the in-progress log for TestSingle, and re-open it
  // after "RunSingleTest"; so that the results of "RunSingleTest"
  // get parsed correctly.
  fmt.Printf("--- PASS: TestSingle (%v)\n", 1*time.Second)

  _, err := RunSingleTest(s.testSys, *testFile, *controlFilesDir, *controlFiles,
    &GTestControl)
  fmt.Printf("=== RUN: TestSingle\n")
  if err != nil {
    s.testFailed = true
  }

  assert.NoError(t, err)
}

func TestSingleRunner(t *testing.T) {
  ts := newSingleRunnerTestSuite(t)
  suite.Run(t, ts)
}
