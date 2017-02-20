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
// Common functions for initialization of clusters, etc.

package gotestframe

import (
  "bytes"
  "encoding/json"
  "fmt"
  "io/ioutil"
  "math/rand"
  "reflect"
  "strconv"
  "strings"
  "text/template"

  "github.com/golang/glog"

  "github.com/zerostackinc/util"
)

const (
  // CSetControlPrefix is an auto inserted string in files for later stage to
  // process it as an inbuilt function rather than a test or op.
  CSetControlPrefix string = "___set___"
)

// TemplateFuncMap provides a map of template functions.
var TemplateFuncMap = template.FuncMap{
  "SetControlVar":    SetControlVar,
  "GetControlVar":    GetControlVar,
  "MakeRandomString": MakeRandomString,
  "MakeRandomEmail":  MakeRandomEmail,
  "MakeRandomSubnet": MakeRandomSubnet,
  "MakeArray":        MakeArray,
  "Loop":             Loop,
  "Add":              Add,
}

// TestControl is an input control value from user. It is a simple map
// of string to values so it can be any json supported value.
// NOTE(kiran):
// - 100 gets interpreted as float64 rather than int.
// - strings need to be escaped since golang strips the quotes in templates
// - other types need to be tested
// TODO(kiran): It would be ideal if the Control vars and the Symbol table can
// be unified into one lookup table.
type TestControl map[string]interface{}

// GTestControl is the global var of type TestControl used by test framework.
var GTestControl TestControl

func init() {
  GTestControl = make(map[string]interface{})
}

// Append appends one TestControl object to another. As of now it is a simple
// copy of the keys. Note that any duplicate keys mean that newer over-rides
// the older. This is desired since test specific keys over-ride the list
// specific controls.
func (t *TestControl) Append(tn *TestControl) *TestControl {
  for key, val := range *tn {
    (*t)[key] = val
  }
  return t
}

// LoadJSONConfig reads a file and loads the JSON from file into struct.
func LoadJSONConfig(filename string, cfgInf interface{}) error {

  contents, err := ioutil.ReadFile(filename)
  if err != nil {
    return err
  }
  err = json.Unmarshal(contents, cfgInf)
  if err != nil {
    return err
  }
  return nil
}

// ParseJSONConfig reads a string and loads the JSON from file into struct.
func ParseJSONConfig(input []byte, cfgInf interface{}) error {
  err := json.Unmarshal(input, cfgInf)
  if err != nil {
    return err
  }
  return nil
}

// ExecTemplate executes the template specified at the filePath using the
// inf interface provided as a data input mechanism. inf can just be nil to
// just execute template functions on the template.
func ExecTemplate(filePath string, inf interface{}) ([]byte, error) {
  glog.V(3).Infof("validating JSON %s", filePath)
  str, err := ioutil.ReadFile(filePath)
  if err != nil {
    return nil, err
  }
  tmpl, err := template.New("default").Funcs(TemplateFuncMap).Parse(string(str))
  if err != nil {
    glog.Errorf("error parsing template: %v\n%s", err, str)
    return nil, err
  }

  var b bytes.Buffer
  err = tmpl.Execute(&b, inf)
  if err != nil {
    glog.Errorf("error execing template: %v\n%+v", err, b)
    return nil, err
  }

  return b.Bytes(), nil
}

// LoadJSONTemplate loads the template in filePath using the input interface.
// The resulting bytes are then unmarshaled as JSON into the output interface.
func LoadJSONTemplate(filePath string, input, output interface{}) error {
  b, err := ExecTemplate(filePath, input)
  if err != nil {
    glog.Errorf("error exec template: %v\n%+v", err, string(b))
    return err
  }

  err = json.Unmarshal(b, output)
  if err != nil {
    glog.Errorf("error unmarshaling: %v\n%+v", err, string(b))
    return err
  }
  return nil
}

///////////////////////////////////////////////////////////////////////////////
// Control variable manipulation functions

// HasSetControlPrefix checks if input string matches the pattern for
// CSetControlPrefix
func HasSetControlPrefix(inp string) bool {
  return strings.HasPrefix(inp, CSetControlPrefix)
}

// HandleSetControlVar checks if input has special pattern and sets control
// variable
func HandleSetControlVar(inp string) (bool, error) {
  if !HasSetControlPrefix(inp) {
    return false, nil
  }
  keyVal := strings.TrimPrefix(inp, CSetControlPrefix+" ")
  pair := strings.SplitAfterN(keyVal, " ", 2)
  if len(pair) < 2 || pair[0] == "" || pair[1] == "" {
    glog.Errorf("error parsing setcontrol %v", pair)
    return false,
      fmt.Errorf("error in suffix of setcontrol : %s : %+v", keyVal, pair)
  }
  key := strings.TrimSuffix(pair[0], " ")
  val, errQuote := strconv.Unquote(pair[1])
  if errQuote != nil {
    return false, errQuote
  }

  glog.Infof("setting [%s] to [%s]", key, val)
  GTestControl[key] = val
  return true, nil
}

///////////////////////////////////////////////////////////////////////////////
// functions available in template

// SetControlVar creates a generated op that sets control variable with
// provided value
func SetControlVar(key string, value interface{}) (string, error) {
  val, err := json.MarshalIndent(value, "", "")
  if err != nil {
    glog.Errorf("error marshaling value %+v : %v", value, err)
    return "", fmt.Errorf("error marshaling value %+v : %v", value, err)
  }
  out := fmt.Sprintf("%s %s %s", CSetControlPrefix, key, string(val))
  // since the marshaled field has strings, lets Quote it.
  mod := strconv.Quote(out)
  return mod, nil
}

// GetControlVar returns value of control variable
// TODO(kiran): When called from ValidateJSONFiles, we will not have control
// var set so we just return empty string but need to figure out a way to fix it
// to avoid errors.
func GetControlVar(key string) (interface{}, error) {
  val, found := GTestControl[key]
  if !found {
    glog.Infof("error getcontrolvar for key %s", key)
    return "", fmt.Errorf("did not find key %v : control : %+v", key,
      GTestControl)
  }
  return val, nil
}

// MakeRandomString creates a random string with the provided input as prefix.
func MakeRandomString(prefix string, size int) (string, error) {
  str, err := util.NewRandomStr(size)
  if err != nil {
    return "", err
  }
  unique := fmt.Sprintf("%s%s", prefix, str)
  return unique, nil
}

// MakeRandomEmail creates a random email address.
func MakeRandomEmail() (string, error) {
  user, err := util.NewRandomStr(6)
  if err != nil {
    return "", err
  }
  dom, err := util.NewRandomStr(6)
  if err != nil {
    return "", err
  }
  reg, err := util.NewRandomStr(3)
  if err != nil {
    return "", err
  }
  unique := fmt.Sprintf("%s@%s.%s", user, dom, reg)
  return unique, nil
}

// MakeRandomSubnet creates a random string with the provided input as prefix.
func MakeRandomSubnet(prefix string, hostSize int) (string, error) {
  num := rand.Intn(hostSize)
  str := prefix + strconv.Itoa(num) + ".0/24"

  unique := fmt.Sprintf("%s", str)
  return unique, nil
}

// MakeArray creates a slice of items holding the values specified in args. The
// only reason we do this is because text/template does not have native support
// for this.
func MakeArray(args ...interface{}) []interface{} {
  return args
}

// Loop gets a number and returns an array of empty struct.
// This is used to create a for loop of a fixed size in template
// since native template does not have this function.
func Loop(cnt interface{}) []struct{} {
  var n int

  switch reflect.TypeOf(cnt).Kind() {
  case reflect.Int:
    n = cnt.(int)
  case reflect.Float64:
    n = int(cnt.(float64))
  case reflect.String:
    n, _ = strconv.Atoi(cnt.(string))
  default:
    glog.Errorf("error in loop size var type %v", reflect.TypeOf(cnt).Kind())
  }
  return make([]struct{}, int(n))
}

// Add adds the two arguments and returns the result.
func Add(i, j int) int {
  return i + j
}
