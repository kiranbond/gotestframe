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
  "testing"

  "github.com/golang/glog"
  "github.com/stretchr/testify/assert"
  "github.com/stretchr/testify/suite"
)

var (
  listFile = flag.String("list_file", "",
    "TestList definition file in JSON format")
)

type listRunnerTestSuite struct {
  suite.Suite
  testSys *TestSystem
}

func newListRunnerTestSuite(t *testing.T) *listRunnerTestSuite {
  ts := &listRunnerTestSuite{}
  return ts
}

func (s *listRunnerTestSuite) SetupSuite() {
  t := s.T()

  glog.Infof("setting up list runner suite, cleanup_on_success: %v, "+
    "cleanup_on_fail:%v", *cleanupOnSuccess, *cleanupOnFail)

  assert.NotEmpty(t, *listFile, "test list filename is empty")
}

func (s *listRunnerTestSuite) TearDownSuite() {
  glog.Infof("performing list runner suite cleanup ")
}

func (s *listRunnerTestSuite) TestRunList() {
  t := s.T()

  listControl := &TestControl{}
  err := RunTestList(*listFile, *controlFilesDir, *controlFiles,
    *cleanupOnSuccess, *cleanupOnFail,
    *jJobID, listControl)
  assert.NoError(t, err)
}

func TestListRunner(t *testing.T) {
  ts := newListRunnerTestSuite(t)
  suite.Run(t, ts)
}
