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

// Package to run a test specified by a list of files as input operations.
// Ops are expected to create cluster and required accounts and openstack
// install before doing cloud operations.

package gotestframe

import (
  "flag"
  "fmt"
  "strings"
  "time"

  "github.com/golang/glog"

  "github.com/zerostackinc/iprotos"
)

var (
  jJobID = flag.String("jenkins_job_id", "", "jenkins jod id")
  // TODO(kiran) flags to be cleaned up
  cleanupOnSuccess = flag.Bool("cleanup_system_on_success", true,
    "if set to true, clean up on test success else keep it running")
  cleanupOnFail = flag.Bool("cleanup_system_on_fail", true,
    "If set to true, destroys on test failure else"+
      "keep it running.")
)

// ValidateJSONFiles just loads all JSON files for ops to sanity check them.
// This can be called upfront before starting the test so that all json parsing
// errors are found upfront instead of in the middle of execution.
func ValidateJSONFiles(dir string, fileNames []string,
  control *TestControl) error {

  for idx, fileName := range fileNames {
    done, err := HandleSetControlVar(fileName)
    if done {
      continue
    }
    if err != nil {
      glog.Errorf("error handling setcontrol %v", err)
      return err
    }
    if !strings.HasPrefix(fileName, "/") && dir != "" {
      fileName = dir + "/" + fileName
    }
    // Load test ops from the file
    testOps := &iprotos.TestOps{}
    err = LoadJSONTemplate(fileName, control, testOps)
    if err != nil || testOps == nil {
      return fmt.Errorf("error in loading test ops[%d] from %s :: %v",
        idx, fileName, err)
    }
  }
  return nil
}

// LoadControlFile is a generic function to load TestControl object from the
// file (while appending dir as necessary).
func LoadControlFile(fileName, dir string) (*TestControl, error) {
  control := &TestControl{}

  if fileName == "" {
    glog.Infof("no control file to load")
    return nil, nil
  }

  glog.Infof("loading control file")

  if !strings.HasPrefix(fileName, "/") && dir != "" {
    fileName = dir + "/" + fileName
  }
  err := LoadJSONTemplate(fileName, nil, control)
  if err != nil {
    glog.Errorf("error parsing the test control file: %s :: %v",
      fileName, err)
    return nil, err
  }

  return control, nil
}

// RunSingleTest runs one test using the test system, testFile and the
// list control object. The listControl object is used for loading the
// test file so any variables in the test files can be controlled by the
// listControl object.
func RunSingleTest(testSys *TestSystem, testFile string,
  controlFilesFlagDir string, controlFilesFlag string,
  listControl *TestControl) (bool, error) {

  // Some initialization upfront to print meaningful jenkins messages
  var result error
  unknown := "UnknownName"

  testConfig := iprotos.SingleTest{Name: &unknown}
  begin := time.Now()

  defer func() {
    fmt.Printf("=== RUN %s\n", testConfig.GetName())
    if result != nil {
      fmt.Printf("--- FAIL: %s (%v)\n", testConfig.GetName(),
        time.Since(begin))
    } else {
      fmt.Printf("--- PASS: %s (%v)\n", testConfig.GetName(),
        time.Since(begin))
    }
  }()

  controlTest := &GTestControl

  // split the provided string with delimiter ","
  controlFiles := strings.Split(controlFilesFlag, ",")

  // append control file flag to test controls for running test.
  if len(controlFiles) > 0 {
    for _, controlFlagFile := range controlFiles {
      if controlFlagFile != "" {
        controlFlag, resultC := LoadControlFile(controlFlagFile, controlFilesFlagDir)
        if resultC != nil {
          glog.Errorf("error loading control file %v:: %v", controlFlagFile, resultC)
          return true, resultC
        }
        controlTest.Append(controlFlag)
        glog.Infof("loaded control Flag file:%s", controlFlagFile)
        glog.Infof("loaded control: %+v", controlTest)
      }
    }
  }

  // append test controls to list controls for running test.
  controlTest.Append(listControl)

  result = LoadJSONTemplate(testFile, controlTest, &testConfig)
  if result != nil {
    glog.Errorf("error loading testfile: %s :: %v", testFile, result)
    return true, result
  }

  controlFiles = testConfig.GetControlFiles()
  if len(controlFiles) > 0 {
    for _, controlFile := range testConfig.GetControlFiles() {
      var control *TestControl
      control, result = LoadControlFile(controlFile, controlFilesFlagDir)
      if result != nil {
        glog.Errorf("error loading control file %v:: %v", controlFile, result)
        return true, result
      }
      controlTest.Append(control)
      glog.Infof("loaded control file:%s", controlFile)
      glog.Infof("loaded control: %+v", controlTest)
    }
  }

  glog.Infof("loaded control: %+v", controlTest)

  glog.Infof("validating json files upfront")

  result = ValidateJSONFiles(testConfig.GetDir(), testConfig.OpsFiles, controlTest)
  if result != nil {
    glog.Errorf("error validating json files :: %v", result)
    return true, result
  }

  glog.Infof("running test %s", testConfig.String())

  var done bool

  for idx, fileName := range testConfig.OpsFiles {
    glog.Infof("running test ops from file[%d]: %s", idx, fileName)
    done, result = HandleSetControlVar(fileName)
    if done {
      continue
    }
    if result != nil {
      glog.Errorf("error handling setcontrol %v", result)
      return true, result
    }
    // If the path is not absolute and testDir is not empty then prepend it.
    if !strings.HasPrefix(fileName, "/") && testConfig.GetDir() != "" {
      fileName = testConfig.GetDir() + "/" + fileName
    }

    result = testSys.RunTestOps(fileName, controlTest)
    if result != nil {
      glog.Errorf("error running ops from: %s :: %v", fileName, result)
      return testConfig.GetStopOnFail(), result
    }
  }

  return false, nil
}

// TearDownSingleTest cleans up a single test based on flags. This is run after
// each single test in a list.
func TearDownSingleTest(testSys *TestSystem, failed bool,
  cleanupOnSuccess, cleanupOnFail bool, jobID, testName string) error {

  shouldCleanupVC := false

  if failed {
    if jobID != "" {
      // Collect SOS logs.
      logPath := jobID + "/" + testName
      testSys.CollectLogFromStars("sos", logPath)
    }

    // check network health.
    // testSys.CheckNetOnStars()

    // if test failed, destroy VC if caller asked to do so else keep it running
    shouldCleanupVC = cleanupOnFail
    glog.Infof("test failed, so marking vc clean up to: %v", shouldCleanupVC)
  } else {
    // test passed, destroy
    shouldCleanupVC = cleanupOnSuccess
    glog.Infof("test succeeded, so marking vc clean up to: %v", shouldCleanupVC)
  }

  if shouldCleanupVC {
    err := testSys.Destroy()
    if err != nil {
      glog.Errorf("error cleaning up test system :: %v", err)
    }
  } else {
    glog.Infof("leaving virtual cluster running")
    // we should close the ssh session to pxe server after test only if we do
    // not Destroy the cluster so that no stale pxe session stays alive in pxe
    // vm
    if err := testSys.CloseSSHSessionToPXEServer(); err != nil {
      glog.Infof("could not close ssh session to pxe server")
    }
  }

  return nil
}

// RunTestList runs a list of tests specified in the list file.
func RunTestList(listFile string, controlFilesFlagDir string, controlFilesFlag string,
  cleanupOnSuccess, cleanupOnFail bool,
  jobID string, listControl *TestControl) error {

  var controlFlag *TestControl
  var result error

  testList := iprotos.TestList{}

  controlTest := &TestControl{}

  // split the provided string with delimiter ","
  controlFiles := strings.Split(controlFilesFlag, ",")

  // append control file flag to test controls for running test.
  if len(controlFiles) > 0 {
    for _, controlFlagFile := range controlFiles {
      if controlFlagFile != "" {
        controlFlag, result = LoadControlFile(controlFlagFile, controlFilesFlagDir)
        if result != nil {
          glog.Errorf("error loading control file %v:: %v", controlFlagFile, result)
          return result
        }
        controlTest.Append(controlFlag)
        glog.Infof("loaded control Flag file:%s", controlFlagFile)
        glog.Infof("loaded control: %+v", controlTest)
      }
    }
  }
  // append test controls to list controls for running test.
  controlTest.Append(listControl)

  result = LoadJSONTemplate(listFile, controlTest, &testList)
  if result != nil {
    glog.Errorf("error loading test list file:%s :: %v", listFile, result)
  }

  controlFiles = testList.GetControlFiles()
  if len(controlFiles) > 0 {
    for _, controlFile := range testList.GetControlFiles() {
      var control *TestControl
      control, result = LoadControlFile(controlFile, controlFilesFlagDir)
      if result != nil {
        glog.Errorf("error loading control file %s:: %v", controlFile, result)
        return result
      }
      controlTest.Append(control)
      glog.Infof("loaded control Flag file:%s", controlFile)
      glog.Infof("loaded control: %+v", controlTest)
    }
  }

  glog.Infof("parsing tests %s", testList.String())

  // Since go2xunit parsing is not good at parsing nested test results,
  // we close the in-progress log for TestRunList, and re-open it
  // after all the "RunSingleTest"s have run.
  fmt.Printf("--- PASS: TestRunList (%v)\n", 1*time.Second)
  defer func() {
    fmt.Printf("=== RUN: TestRunList\n")
  }()

  // TODO(kiran): more complex result struct if needed.
  var failedTests [][2]string
  var done, stop bool

  for idx, testName := range testList.TestList {
    done, result = HandleSetControlVar(testName)
    if done {
      continue
    }
    if result != nil {
      glog.Errorf("error handling setcontrol %v", result)
      return result
    }
    testPath := testName
    // If the path is not absolute and dir is not empty then prepend it.
    if !strings.HasPrefix(testPath, "/") && testList.GetDir() != "" {
      testPath = testList.GetDir() + "/" + testPath
    }
    glog.Infof("running test ops from file[%d]: %s", idx, testPath)

    testSys := NewTestSystem(cleanupOnFail, *serviceTimeout,
      *openstackSetupTimeout, *openstackOpTimeout)
    if testSys == nil {
      return fmt.Errorf("error creating NewTestSystem")
    }

    stop, result = RunSingleTest(testSys, testPath, controlFilesFlagDir,
      controlFilesFlag, controlTest)
    if result != nil {
      glog.Errorf("error running test from file:%s :: %v", testPath, result)
      failedTests = append(failedTests, [2]string{testName, result.Error()})
    }

    failed := stop || result != nil

    // If we are going to run one more test, we have to tear it down on success
    // but track its error separately
    errT := TearDownSingleTest(testSys, failed, cleanupOnSuccess, cleanupOnFail,
      jobID, testName)

    if stop || errT != nil {
      glog.Errorf("stopping test list stop: %v, teardown: %v", stop, errT)
      // we will return the run error rather than teardown error
      return result
    }
  }

  if len(failedTests) > 0 {
    for index, failedTest := range failedTests {
      ll := fmt.Errorf("\n---------------------------------------------------\n")
      glog.Errorf("%v%d. %v ## %v", ll.Error(), index, failedTest[0],
        failedTest[1])
    }
    glog.Infof("\n---------------------------------------------------")

    return fmt.Errorf("above tests failed")
  }

  return nil
}
